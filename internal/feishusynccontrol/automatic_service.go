package feishusynccontrol

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// automaticService owns normal business availability. It has no private/public
// approval key, signer, approval plan, or human permit dependency.
type automaticService struct {
	c              *Controller
	db             *gorm.DB
	keys           AutomaticSourceKeyScopes
	stable         stableServiceHealth
	missingSince   time.Time
	lastCanaryRead time.Time
	receipt        *systemServiceReceipt
	needsCanary    bool
}

type systemServiceTransactionKey struct{}

// NewAutomaticServiceManager installs no operational signer and performs no
// recovery write. The serialized controller will qualify and journal its own
// epoch before any ordinary source can execute.
func NewAutomaticServiceManager(c *Controller, db *gorm.DB, keys AutomaticSourceKeyScopes) (*RecoveryManager, error) {
	if c == nil || !c.Config.AutomaticService || db == nil || db.Name() != "postgres" ||
		!c.Config.Automatic || !c.Config.DedicatedSyncLane || c.Config.Scope != "all" || keys == nil {
		return nil, ErrNotReady
	}
	// Receipt/dependency failures are handled by the bounded closed controller,
	// not an APP-wide startup panic that would also interrupt unrelated traffic.
	s := &automaticService{c: c, db: db, keys: keys}
	m := &RecoveryManager{controller: c, system: s}
	c.system, c.steadyManager = s, m
	c.Guard.RequireAutomaticService()
	c.Guard.SetApprovedSourceValidator(m.checkApprovedSource)
	return m, nil
}

func (m *RecoveryManager) AutomaticServiceManaged() bool { return m != nil && m.system != nil }

func (s *automaticService) health(ctx context.Context) (systemHealthProof, error) {
	c := s.c
	var proof systemHealthProof
	if c.ApprovedConsumers == nil {
		return proof, ErrNotReady
	}
	registryCount, err := c.Client.ZCard(ctx, "asynq:servers").Result()
	if err != nil || registryCount < 1 || registryCount > consumerEvidenceLimit {
		return proof, errors.Join(ErrNotReady, err)
	}
	revision, err := c.Client.Get(ctx, c.prefix+"membership_revision").Result()
	if err != nil {
		return proof, err
	}
	var evidence *consumerEvidence
	probe := context.WithValue(ctx, consumerEvidenceKey{}, &evidence)
	ids, err := c.ApprovedConsumers(probe)
	c.consumerDiagnostic = evidence
	if err != nil || len(ids) != 1 || ids[0] != c.Config.WorkerID || evidence == nil ||
		evidence.Truncated || len(evidence.ConsumerServerIDs) != 1 {
		return proof, errors.Join(ErrNotReady, err)
	}
	proof = systemHealthProof{
		WorkerID: c.Config.WorkerID, WorkerValue: c.fingerprint + ":" + c.incarnation,
		MembershipRevision: revision, ConsumerServerIDs: append([]string(nil), evidence.ConsumerServerIDs...),
		Metadata: map[string]string{},
	}
	key := "asynq:servers:{" + proof.WorkerID + ":" + proof.ConsumerServerIDs[0] + "}"
	read := evidence.reads[key]
	if read == nil || read.raw == "" {
		return systemHealthProof{}, ErrNotReady
	}
	proof.Metadata[key] = redisCASDigest(read.raw)
	args, err := systemProofArgs(proof)
	if err != nil {
		return systemHealthProof{}, err
	}
	if err := redis.NewScript(systemHealthLua+"return 1").Run(ctx, c.Client, s.healthKeys(), args...).Err(); err != nil {
		return systemHealthProof{}, err
	}
	return proof, nil
}

func (s *automaticService) healthKeys() []string {
	p := s.c.prefix
	return []string{p + "workers", p + "leases", p + "membership_revision", p + "admitted", "asynq:servers"}
}

// withdrawal is immediate and recoverable, without a permanent trip. A failed
// withdrawal/audit never reports recovery: the old permit still expires in750ms.
func (s *automaticService) withdraw(ctx context.Context, reason string) error {
	c := s.c
	c.Guard.SuspendDependency(true)
	stop, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	err := c.Client.Del(stop, c.Guard.PermitKey()).Err()
	if c.lastEvent != "system_wait:"+reason {
		audit := c.audit(stop, Event{Kind: "feishu_automatic_wait", Reason: reason, WorkerID: c.Config.WorkerID})
		if err == nil && audit == nil {
			c.lastEvent = "system_wait:" + reason
		}
		err = errors.Join(err, audit)
	}
	return errors.Join(ErrNotReady, err)
}

// Incident evidence is bounded, content-free and pinned by latest stream IDs.
// No Inspector cleanup is used to obtain additional historical diagnostics.
func (s *automaticService) incidentEvidence(ctx context.Context, trip string) (string, string, string, string, error) {
	guard, err := s.readIncidentStream(ctx, "audit")
	if err != nil {
		return "", "", "", "", errors.Join(ErrNotReady, err)
	}
	control, err := s.readIncidentStream(ctx, "control_audit")
	if err != nil {
		return "", "", "", "", errors.Join(ErrNotReady, err)
	}
	matched := trip == ""
	incidentMS := ""
	incidentID := ""
	for _, e := range guard {
		if e.Values["event"] == "trip" && e.Values["reason"] == trip {
			matched = true
			incidentMS, _, _ = strings.Cut(e.ID, "-")
			incidentID = e.ID
			break
		}
	}
	if !matched {
		return "", "", "", "", ErrNotReady
	}
	if trip != "" {
		// Legacy streams are trimmed. A control entry preceding the incident
		// proves that the retained window covers every later operator stop;
		// an empty/truncated window is not evidence of no operator intent.
		if len(control) == 0 {
			return "", "", "", "", ErrNotReady
		}
		firstMS, _, _ := strings.Cut(control[0].ID, "-")
		if len(firstMS) > len(incidentMS) || len(firstMS) == len(incidentMS) && firstMS >= incidentMS {
			return "", "", "", "", ErrNotReady
		}
	}
	for _, e := range append(append([]redis.XMessage(nil), guard...), control...) {
		ms, _, _ := strings.Cut(e.ID, "-")
		// Redis stream IDs use canonical millisecond decimal values. Compare
		// lengths first, not lexicographic numbers of different lengths.
		after := incidentMS == "" || len(ms) > len(incidentMS) || len(ms) == len(incidentMS) && ms >= incidentMS
		reason, _ := e.Values["reason"].(string)
		// Trip is first-writer-wins. Later hard failures may only exist in the
		// audit stream and must not be hidden by the first recoverable reason.
		hard := e.Values["event"] == "trip" && !automaticServiceRecoverableTrip(reason)
		hard = hard || e.Values["event"] == "feishu_safety_stop" &&
			!automaticServiceRecoverableTrip(reason) && reason != "automatic_permit_tripped"
		if after && (hard || e.Values["event"] == "emergency_stop" || reason == "operator_emergency_stop") {
			return "", "", "", "", ErrNotReady
		}
	}
	raw, err := json.Marshal(map[string]any{"guard": guard, "control": control})
	if err != nil || len(raw) > 256*1024 {
		return "", "", "", "", ErrNotReady
	}
	guardID, controlID := "", ""
	if len(guard) > 0 {
		guardID = guard[len(guard)-1].ID
	}
	if len(control) > 0 {
		controlID = control[len(control)-1].ID
	}
	return string(raw), guardID, controlID, incidentID, nil
}

func (s *automaticService) readIncidentStream(ctx context.Context, name string) ([]redis.XMessage, error) {
	var entries []redis.XMessage
	start, size := "-", 0
	for {
		page, err := s.c.Client.XRangeN(ctx, s.c.prefix+name, start, "+", 16).Result()
		if err != nil {
			return nil, err
		}
		for _, entry := range page {
			raw, err := json.Marshal(entry)
			if err != nil || len(entries) >= 1000 || size+len(raw) > 128*1024 {
				return nil, ErrNotReady
			}
			size += len(raw)
			entries = append(entries, entry)
		}
		if len(page) < 16 {
			return entries, nil
		}
		start = "(" + page[len(page)-1].ID
	}
}

func (s *automaticService) migrate(ctx context.Context, proof systemHealthProof, rawState, trip string) error {
	c := s.c
	if trip != "" && !automaticServiceRecoverableTrip(trip) {
		return s.withdraw(ctx, "hard_stop_preserved")
	}
	// No running source is silently trusted or retired by its task payload.
	// This callback must fence claimed generations only; unclaimed accepted
	// requests retain their durable SyncLogs and ordinary task retries.
	if err := c.Stop(ctx); err != nil {
		return err
	}
	receipt, err := readSystemReceipt(ctx, s.db)
	if err != nil {
		return err
	}
	if receipt != nil && receipt.Phase == "active" {
		prior, err := systemState(rawState)
		if err != nil || prior.Namespace != c.prefix || prior.Phase == "blocked" {
			return ErrNotReady
		}
		if err := checkSystemReceipt(ctx, s.db, prior); err != nil {
			return err
		}
		if prior.Phase == "ramp" || prior.Phase == "normal" {
			if _, err := readCanaryWitness(ctx, s.db, prior); err != nil {
				return err
			}
		}
	}
	resumePrepared := false
	if receipt != nil && receipt.Phase == "prepared" {
		// A lost Redis acknowledgment or a failed SQL acknowledgment is not a
		// reason to replay migration. Verify the committed state and its exact
		// untrimmed journal receipt before acknowledging SQL, still without a permit.
		if rawState == receipt.StateJSON && trip == "" {
			if err := s.verifyPreparedCommit(ctx, proof, *receipt); err != nil {
				return err
			}
			return acknowledgeSystemReceipt(ctx, s.db, *receipt)
		}
		// An uncommitted preparation can only be superseded while the exact
		// original Redis snapshot remains. A new process creates its own epoch;
		// it never inherits execution authority from its predecessor's receipt.
		if receipt.OriginalTrip != trip || receipt.PriorStateJSON != rawState {
			return ErrNotReady
		}
		resumePrepared = receipt.WorkerValue == proof.WorkerValue && receipt.RuntimeSHA == c.Config.SourceSHA
	}
	if !resumePrepared {
		if receipt != nil && receipt.Phase != "prepared" && rawState == "" {
			return ErrNotReady // state loss is not a clean installation.
		}
		evidence, guardID, controlID, incidentID, err := s.incidentEvidence(ctx, trip)
		if err != nil {
			return err
		}
		originals := make([]string, 4)
		for i, key := range []string{"normal_service", "recovery", "observation_required", "recovery_required"} {
			originals[i], err = systemRead(ctx, c.Client, c.prefix+key)
			if err != nil {
				return err
			}
		}
		state := systemServiceState{
			Version: 1, Namespace: c.prefix, Epoch: uuid.NewString(), RuntimeSHA: c.Config.SourceSHA,
			WorkerID: proof.WorkerID, WorkerValue: proof.WorkerValue, MembershipRevision: proof.MembershipRevision,
			ConsumerServerID:     proof.ConsumerServerIDs[0],
			ConsumerMetadataSHA1: proof.Metadata["asynq:servers:{"+proof.WorkerID+":"+proof.ConsumerServerIDs[0]+"}"],
			GuardFingerprint:     c.Guard.Fingerprint(), Phase: "canary",
		}
		raw, err := json.Marshal(state)
		if err != nil {
			return err
		}
		next := &systemServiceReceipt{
			Version: 1, Namespace: c.prefix, Epoch: state.Epoch,
			RuntimeSHA: state.RuntimeSHA, WorkerValue: state.WorkerValue, Phase: "prepared",
			StateJSON: string(raw), PriorStateJSON: rawState, OriginalTrip: trip,
			OriginalNormal: originals[0], OriginalRecovery: originals[1], OriginalObserve: originals[2], OriginalRequired: originals[3],
			GuardAuditID: guardID, ControlAuditID: controlID, OriginalTripAuditID: incidentID,
			EvidenceJSON: evidence, CreatedMS: time.Now().UnixMilli(),
		}
		if err := prepareSystemReceipt(ctx, s.db, next, receipt); err != nil {
			return err
		}
		receipt = next
	}
	args, err := systemProofArgs(proof)
	if err != nil {
		return err
	}
	digest, err := RecoveryDigest(receipt)
	if err != nil {
		return err
	}
	args = append(args, receipt.OriginalTrip, receipt.PriorStateJSON, receipt.OriginalNormal,
		receipt.OriginalRecovery, receipt.OriginalObserve, receipt.StateJSON, digest, receipt.EvidenceJSON,
		receipt.GuardAuditID, receipt.ControlAuditID, receipt.RetiredNormal, receipt.OriginalRequired, receipt.OriginalTripAuditID)
	keys := append(s.healthKeys(), c.prefix+"trip", c.prefix+"system_service", c.prefix+"normal_service",
		c.prefix+"recovery", c.prefix+"observation_required", c.prefix+"inflight", "asynq:{approved_sync}:active",
		"asynq:{sync}:active", c.prefix+"system_journal", c.prefix+"audit", c.prefix+"control_audit",
		c.prefix+"recovery_required", c.Guard.PermitKey())
	if err := migrateSystemService.Run(ctx, c.Client, keys, args...).Err(); err != nil {
		return err
	}
	if err := acknowledgeSystemReceipt(ctx, s.db, *receipt); err != nil {
		return err // Redis-only transition cannot authorize any source.
	}
	s.receipt = receipt
	return nil
}

func (s *automaticService) verifyPreparedCommit(ctx context.Context, proof systemHealthProof, receipt systemServiceReceipt) error {
	// A new, healthy process may acknowledge an already committed predecessor
	// receipt. This only completes historical durability: current() still denies
	// the predecessor identity until a separate quiescent new-epoch migration.
	args, err := systemProofArgs(proof)
	if err != nil {
		return err
	}
	digest, err := RecoveryDigest(receipt)
	if err != nil {
		return err
	}
	args = append(args, receipt.StateJSON, receipt.RetiredNormal, receipt.Epoch, digest)
	keys := append(s.healthKeys(), s.c.prefix+"system_service", s.c.prefix+"normal_service",
		s.c.prefix+"trip", s.c.prefix+"recovery_required", s.c.prefix+"system_journal", s.c.Guard.PermitKey())
	return redis.NewScript(systemHealthLua+`
if redis.call('GET',KEYS[6])~=ARGV[5] or (redis.call('GET',KEYS[7]) or '')~=ARGV[6] or
 redis.call('EXISTS',KEYS[8])~=0 or redis.call('GET',KEYS[9])~='system-service-v1:'..ARGV[7] or
 redis.call('EXISTS',KEYS[11])~=0 then return redis.error_reply('SYSTEM_PREPARED_COMMIT_CHANGED') end
local last=redis.call('XREVRANGE',KEYS[10],'+','-','COUNT',1)
if #last~=1 then return redis.error_reply('SYSTEM_COMMIT_AUDIT_MISSING') end
local fields={};for i=1,#last[1][2],2 do fields[last[1][2][i]]=last[1][2][i+1] end
if fields.event~='automatic_service_migrated' or fields.epoch~=ARGV[7] or
 fields.sql_receipt_sha256~=ARGV[8] then return redis.error_reply('SYSTEM_COMMIT_AUDIT_CHANGED') end
return 1`).Run(ctx, s.c.Client, keys, args...).Err()
}

// Runtime normal/canary authority is never inferred from container health,
// prior signatures, a queue payload or an inherited execution tenant.
func (s *automaticService) current(ctx context.Context) (systemServiceState, string, error) {
	raw, err := s.c.Client.Get(ctx, s.c.prefix+"system_service").Result()
	if err != nil {
		return systemServiceState{}, "", err
	}
	state, err := systemState(raw)
	if err != nil || state.Namespace != s.c.prefix || state.RuntimeSHA != s.c.Config.SourceSHA || state.WorkerID != s.c.Config.WorkerID ||
		state.WorkerValue != s.c.fingerprint+":"+s.c.incarnation || state.GuardFingerprint != s.c.Guard.Fingerprint() {
		return state, raw, ErrNotReady
	}
	work := s.db
	if tx, ok := ctx.Value(systemServiceTransactionKey{}).(*gorm.DB); ok && tx != nil {
		work = tx
	}
	if err := checkSystemReceipt(ctx, work, state); err != nil {
		return state, raw, err
	}
	if state.Phase == "ramp" || state.Phase == "normal" {
		if _, err := readCanaryWitness(ctx, work, state); err != nil {
			return state, raw, err
		}
	}
	return state, raw, nil
}

func (s *automaticService) checkSource(ctx context.Context, tx *gorm.DB, source, config string) (context.Context, error) {
	if tx != nil {
		ctx = context.WithValue(ctx, systemServiceTransactionKey{}, tx.Clauses(clause.Locking{Strength: "SHARE"}))
	}
	state, _, err := s.current(ctx)
	if err != nil {
		return ctx, err
	}
	if state.Phase == "blocked" || state.Phase == "canary" && state.CanarySourceID != source {
		return ctx, feishusafety.ErrSourceAdmissionChanged
	}
	if state.Phase == "canary" && (state.CanaryStartedMS <= 0 || time.Now().UnixMilli()-state.CanaryStartedMS >= 300000) {
		return ctx, feishusafety.ErrSourceAdmissionChanged
	}
	authority, err := automaticSourceAuthority(ctx, s.db, tx, source, config, s.keys)
	if err != nil {
		return ctx, err
	}
	identity, _ := feishusafety.RecoveryOutboundExecution(ctx)
	if identity.Plan != "" && identity.Plan != state.Epoch ||
		state.Phase == "canary" && state.CanaryRunID != "" && identity.Run != "" && identity.Run != state.CanaryRunID {
		return ctx, feishusafety.ErrSourceAdmissionChanged
	}
	if prior, ok := ctx.Value(normalSourceAuthorityKey{}).(NormalSourceAuthority); ok &&
		prior.ExecutionSHA256 != authority.ExecutionSHA256 {
		return ctx, feishusafety.ErrSourceAdmissionChanged
	}
	ctx = feishusafety.WithRecoveryContext(ctx, feishusafety.RecoveryContext{
		Plan: state.Epoch, Config: config, Run: identity.Run, Generation: identity.Generation,
	})
	ctx = context.WithValue(ctx, normalSourceAuthorityKey{}, authority)
	if _, claimed := types.SyncExecutionFromContext(ctx); claimed {
		ctx = types.WithReconciliationCommitGuard(ctx, func(publication *gorm.DB) error {
			_, err := s.checkSource(publication.Statement.Context, publication, source, config)
			return err
		})
	}
	return ctx, nil
}
