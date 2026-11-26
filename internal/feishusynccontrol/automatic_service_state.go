package feishusynccontrol

import (
	"context"
	"crypto/sha1" // Redis CAS fingerprint, not an approval or authentication primitive.
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const automaticServiceVersion = 1

// systemServiceState is explicitly system-owned policy, NOT a RecoveryApproval
// or an unsigned NormalServiceAuthorization masquerading as a signed grant.
// Epoch changes invalidate every old task context and source registration.
type systemServiceState struct {
	Version              int    `json:"version"`
	Namespace            string `json:"namespace"`
	Epoch                string `json:"epoch"`
	RuntimeSHA           string `json:"runtime_sha"`
	WorkerID             string `json:"worker_id"`
	WorkerValue          string `json:"worker_value"`
	MembershipRevision   string `json:"membership_revision"`
	ConsumerServerID     string `json:"consumer_server_id"`
	ConsumerMetadataSHA1 string `json:"consumer_metadata_sha1"`
	GuardFingerprint     string `json:"guard_fingerprint"`
	Phase                string `json:"phase"`
	CanarySourceID       string `json:"canary_source_id"`
	CanaryRunID          string `json:"canary_run_id"`
	CanaryStartedMS      int64  `json:"canary_started_ms"`
	CanaryProofSHA256    string `json:"canary_proof_sha256"`
	RampUntilMS          int64  `json:"ramp_until_ms"`
	NextAdmissionMS      int64  `json:"next_admission_ms"`
}

// systemHealthProof is produced only by actual bounded Redis/Asynq reads.
// Metadata bytes never enter a journal, log, API response or task payload.
type systemHealthProof struct {
	WorkerID           string
	WorkerValue        string
	MembershipRevision string
	Metadata           map[string]string // Redis key -> SHA1 of the observed protobuf.
	ConsumerServerIDs  []string
}

func (p systemHealthProof) stabilityIdentity() string {
	ids := append([]string(nil), p.ConsumerServerIDs...)
	sort.Strings(ids)
	// Metadata changes on ordinary heartbeats and must not reset a stable
	// identity. The final atomic transition still compares the current bytes.
	raw, _ := json.Marshal([]any{p.WorkerID, p.WorkerValue, p.MembershipRevision, ids})
	return string(raw)
}

// stableServiceHealth never treats a failed read, a new identity, a long gap
// or a single successful sample as proof that a previous failure recovered.
type stableServiceHealth struct {
	identity string
	started  time.Time
	last     time.Time
	samples  int
}

func (s *stableServiceHealth) observe(now time.Time, proof systemHealthProof, err error) bool {
	if err != nil || proof.WorkerID == "" || len(proof.Metadata) == 0 || len(proof.ConsumerServerIDs) == 0 {
		*s = stableServiceHealth{}
		return false
	}
	identity := proof.stabilityIdentity()
	if identity != s.identity || now.Before(s.last) || now.Sub(s.last) > time.Second {
		*s = stableServiceHealth{identity: identity, started: now, last: now, samples: 1}
		return false
	}
	// Multiple probes in the same instant cannot manufacture a stable window.
	if now.Sub(s.last) < 100*time.Millisecond {
		return s.samples >= 10 && now.Sub(s.started) >= 3*time.Second
	}
	s.last = now
	s.samples++
	return s.samples >= 10 && now.Sub(s.started) >= 3*time.Second
}

func redisCASDigest(raw string) string {
	sum := sha1.Sum([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// Only observable infrastructure/read gaps qualify for automatic historical
// migration. An actual lost worker, conflicting incarnation, operator stop,
// unknown failure, quota/integrity failure or revoked service does not.
func automaticServiceRecoverableTrip(reason string) bool {
	switch reason {
	case "automatic_asynq_consumer_consumer_absent",
		"automatic_redis_ping_redis_timeout", "automatic_redis_ping_context_deadline",
		"automatic_asynq_inspection_redis_timeout", "automatic_asynq_inspection_context_deadline":
		return true
	default:
		return false
	}
}

// This read-only proof is shared by transition and permit renewal. No consumer
// metadata cleanup, inventory removal or permission write occurs here.
const systemHealthLua = `
local t=redis.call('TIME');local now=t[1]*1000+math.floor(t[2]/1000)
if redis.call('HLEN',KEYS[1])~=1 or redis.call('HGET',KEYS[1],ARGV[1])~=ARGV[2] or
 (redis.call('GET',KEYS[3]) or '')~=ARGV[3] then return redis.error_reply('SYSTEM_IDENTITY_CHANGED') end
local lease=redis.call('ZSCORE',KEYS[2],ARGV[1])
if not lease or tonumber(lease)<=now then return redis.error_reply('SYSTEM_LEASE_EXPIRED') end
local admitted=redis.call('HGET',KEYS[4],ARGV[1])
if admitted and admitted~=ARGV[2] then return redis.error_reply('SYSTEM_INCARNATION_CONFLICT') end
local meta=cjson.decode(ARGV[4]);local count=0
for key,digest in pairs(meta) do
 count=count+1
 local raw=redis.call('GET',key);local ttl=redis.call('PTTL',key)
 local score=redis.call('ZSCORE',KEYS[5],key)
 if not raw or ttl<=0 or ttl>15000 or redis.sha1hex(raw)~=digest or
  not score or tonumber(score)*1000<=now then return redis.error_reply('SYSTEM_CONSUMER_CHANGED') end
end
if count<1 or count>32 then return redis.error_reply('SYSTEM_CONSUMER_PROOF_INVALID') end
`

// Migrate is one audited CAS, never an operational DEL/SET shortcut. The
// immutable SQL receipt must already be durable before calling it. Its digest,
// original trip evidence and previous policy fingerprints are retained in an
// UNTRIMMED journal; existing incident records are not rewritten or deleted.
var migrateSystemService = redis.NewScript(systemHealthLua + `
local recoverable={automatic_asynq_consumer_consumer_absent=true,
 automatic_redis_ping_redis_timeout=true,automatic_redis_ping_context_deadline=true,
 automatic_asynq_inspection_redis_timeout=true,automatic_asynq_inspection_context_deadline=true}
if ARGV[5]~='' and not recoverable[ARGV[5]] then return redis.error_reply('SYSTEM_STOP_NOT_RECOVERABLE') end
if (redis.call('GET',KEYS[6]) or '')~=ARGV[5] or
 (redis.call('GET',KEYS[7]) or '')~=ARGV[6] or
 (redis.call('GET',KEYS[8]) or '')~=ARGV[7] or
 (redis.call('GET',KEYS[9]) or '')~=ARGV[8] or
 (redis.call('GET',KEYS[10]) or '')~=ARGV[9] or
 (redis.call('GET',KEYS[17]) or '')~=ARGV[16] then
 return redis.error_reply('SYSTEM_MIGRATION_SNAPSHOT_CHANGED') end
if redis.call('HLEN',KEYS[11])~=0 or redis.call('LLEN',KEYS[12])~=0 or
 redis.call('LLEN',KEYS[13])~=0 or redis.call('EXISTS',KEYS[18])~=0 then
 return redis.error_reply('SYSTEM_OLD_WORK_NOT_QUIESCENT') end
if redis.call('EXISTS',KEYS[19])~=0 then return redis.error_reply('SYSTEM_COMMIT_REFERENCE_EXISTS') end
local guard=redis.call('XREVRANGE',KEYS[15],'+','-','COUNT',1)
local control=redis.call('XREVRANGE',KEYS[16],'+','-','COUNT',1)
if (#guard==0 and ARGV[13]~='') or (#guard>0 and guard[1][1]~=ARGV[13]) or
 (#control==0 and ARGV[14]~='') or (#control>0 and control[1][1]~=ARGV[14]) then
 return redis.error_reply('SYSTEM_INCIDENT_EVIDENCE_CHANGED') end
local evidence=cjson.decode(ARGV[12])
local coverage={evidence.prior_guard_id or '',evidence.prior_control_id or ''}
for i,key in ipairs({KEYS[15],KEYS[16]}) do
 if coverage[i]~='' and #redis.call('XRANGE',key,coverage[i],coverage[i],'COUNT',1)~=1 then
  return redis.error_reply('SYSTEM_INCIDENT_COVERAGE_CHANGED') end
end
if ARGV[5]~='' and coverage[2]=='' then return redis.error_reply('SYSTEM_CONTROL_COVERAGE_MISSING') end
if ARGV[5]~='' then
 if ARGV[17]=='' then return redis.error_reply('SYSTEM_ORIGINAL_TRIP_AUDIT_MISSING') end
 local incident=redis.call('XRANGE',KEYS[15],ARGV[17],ARGV[17],'COUNT',1)
 if #incident~=1 then return redis.error_reply('SYSTEM_ORIGINAL_TRIP_AUDIT_MISSING') end
 local fields={};for i=1,#incident[1][2],2 do fields[incident[1][2][i]]=incident[1][2][i+1] end
 if fields.event~='trip' or fields.reason~=ARGV[5] then
  return redis.error_reply('SYSTEM_ORIGINAL_TRIP_AUDIT_CHANGED') end
elseif ARGV[17]~='' then return redis.error_reply('SYSTEM_UNEXPECTED_TRIP_AUDIT') end
local state=cjson.decode(ARGV[10])
if state.version~=1 or state.phase~='canary' or state.worker_id~=ARGV[1] or
 state.worker_value~=ARGV[2] or state.membership_revision~=ARGV[3] or
 KEYS[19]~=state.namespace..'system_commit:'..state.epoch then
 return redis.error_reply('SYSTEM_MIGRATION_STATE_INVALID') end
local journal_id=redis.call('XADD',KEYS[14],'*','event','automatic_service_migrated','epoch',state.epoch,
 'sql_receipt_sha256',ARGV[11],'original_trip',ARGV[5],'original_evidence',ARGV[12],
 'original_trip_audit_id',ARGV[17],
 'legacy_normal_sha1',redis.sha1hex(ARGV[7]),'legacy_recovery_sha1',redis.sha1hex(ARGV[8]),
 'legacy_observation_sha1',redis.sha1hex(ARGV[9]),'previous_system_state',ARGV[6])
redis.call('SET',KEYS[7],ARGV[10])
if ARGV[15]~='' then redis.call('SET',KEYS[8],ARGV[15]) end
redis.call('SET',KEYS[17],'system-service-v1:'..state.epoch)
redis.call('SET',KEYS[19],ARGV[11]..':'..journal_id)
redis.call('XADD',KEYS[16],'*','event','automatic_service_migrated','epoch',state.epoch,
 'sql_receipt_sha256',ARGV[11],'worker_id',state.worker_id)
if ARGV[5]~='' then redis.call('DEL',KEYS[6]) end
return 1`)

func systemProofArgs(proof systemHealthProof) ([]any, error) {
	fingerprint, incarnation, separated := strings.Cut(proof.WorkerValue, ":")
	id, incarnationErr := uuid.Parse(incarnation)
	_, revisionErr := strconv.ParseUint(proof.MembershipRevision, 10, 64)
	if proof.WorkerID == "" || !separated || !recoverySHA.MatchString(fingerprint) ||
		incarnationErr != nil || id.String() != incarnation || revisionErr != nil ||
		len(proof.Metadata) != 1 || len(proof.ConsumerServerIDs) != 1 {
		return nil, ErrNotReady
	}
	serverID, err := uuid.Parse(proof.ConsumerServerIDs[0])
	if err != nil || serverID.String() != proof.ConsumerServerIDs[0] {
		return nil, ErrNotReady
	}
	key := "asynq:servers:{" + proof.WorkerID + ":" + serverID.String() + "}"
	digest, ok := proof.Metadata[key]
	decoded, digestErr := hex.DecodeString(digest)
	if !ok || digestErr != nil || len(decoded) != sha1.Size {
		return nil, ErrNotReady
	}
	metadata, err := json.Marshal(proof.Metadata)
	if err != nil {
		return nil, err
	}
	return []any{proof.WorkerID, proof.WorkerValue, proof.MembershipRevision, string(metadata)}, nil
}

func systemState(raw string) (systemServiceState, error) {
	var state systemServiceState
	if len(raw) > 4096 || json.Unmarshal([]byte(raw), &state) != nil ||
		state.Version != automaticServiceVersion || !recoverySHA.MatchString(state.GuardFingerprint) ||
		len(state.RuntimeSHA) != 40 || state.WorkerID == "" || state.WorkerValue == "" ||
		state.MembershipRevision == "" {
		return state, ErrNotReady
	}
	if id, err := uuid.Parse(state.Epoch); err != nil || id.String() != state.Epoch {
		return state, ErrNotReady
	}
	if _, err := hex.DecodeString(state.RuntimeSHA); err != nil ||
		!strings.HasPrefix(state.Namespace, "feishu:safety:{") || !strings.HasSuffix(state.Namespace, "}:") {
		return state, ErrNotReady
	}
	switch state.Phase {
	case "canary", "ramp", "normal", "blocked":
		return state, nil
	default:
		return state, ErrNotReady
	}
}

func systemRead(ctx context.Context, client redis.UniversalClient, key string) (string, error) {
	raw, err := client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	return raw, err
}
