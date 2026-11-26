package feishusynccontrol

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

var errCanaryEvidenceLimit = errors.New("automatic Canary evidence exceeds candidate limit")

// Immutable SQL evidence of an ordinary increment, not a human approval.
type systemCanaryWitness struct {
	Version           int    `json:"version"`
	Epoch             string `json:"epoch"`
	SourceID          string `json:"source_id"`
	RunID             string `json:"run_id"`
	RuntimeSHA        string `json:"runtime_sha"`
	WorkerValue       string `json:"worker_value"`
	ConfigSHA256      string `json:"config_sha256"`
	AuthoritySHA256   string `json:"authority_sha256"`
	ExecutionSHA256   string `json:"execution_sha256"`
	PublicationSHA256 string `json:"publication_sha256"`
}

func canaryWitnessKey(state systemServiceState) string {
	return systemServiceSetting + ".canary." + state.Epoch + "." + state.CanaryRunID
}

func readCanaryWitness(
	ctx context.Context,
	db *gorm.DB,
	state systemServiceState,
) (systemCanaryWitness, error) {
	var witness systemCanaryWitness
	var row types.SystemSetting
	if err := db.WithContext(ctx).First(&row, "key=?", canaryWitnessKey(state)).Error; err != nil {
		return witness, err
	}
	if json.Unmarshal(row.Value, &witness) != nil || witness.Version != 1 || witness.Epoch != state.Epoch ||
		witness.SourceID != state.CanarySourceID || witness.RunID != state.CanaryRunID ||
		witness.RuntimeSHA != state.RuntimeSHA ||
		witness.WorkerValue != state.WorkerValue ||
		!recoverySHA.MatchString(witness.PublicationSHA256) {
		return witness, ErrNotReady
	}
	hash, err := RecoveryDigest(witness)
	if err != nil || hash != state.CanaryProofSHA256 {
		return witness, ErrNotReady
	}
	return witness, nil
}

// Resource identities/counts are bounded and hashed before recording. No title,
// document body, raw original path or SQL parameters are retained in the witness.
func canaryPublicationDigest(ctx context.Context, db *gorm.DB, source string) (string, error) {
	var rows []struct {
		Resource, Knowledge, Attempt, Revision, OriginalPathDigest string
		Generation, Chunks, Embeddings                             int64
	}
	err := db.WithContext(ctx).Raw(`SELECT r.id AS resource,r.knowledge_id AS knowledge,
 r.active_attempt AS attempt,r.source_revision AS revision,r.generation,md5(k.file_path) AS original_path_digest,
 (SELECT count(*) FROM chunks c WHERE c.knowledge_id=k.id AND c.deleted_at IS NULL) AS chunks,
 (SELECT count(*) FROM embeddings e WHERE e.knowledge_id=k.id) AS embeddings
 FROM feishu_legacy_resources r JOIN knowledges k ON k.id=r.knowledge_id AND k.tenant_id=r.tenant_id
 AND k.knowledge_base_id=r.knowledge_base_id AND k.deleted_at IS NULL
 WHERE r.data_source_id=? AND r.state='active' ORDER BY r.id LIMIT 257`, source).Scan(&rows).Error
	if err != nil {
		return "", err
	}
	if len(rows) > 256 {
		return "", errCanaryEvidenceLimit
	}
	if len(rows) == 0 {
		return "", ErrNotReady
	}
	return RecoveryDigest(rows)
}

func (s *automaticService) advanceCanary(
	ctx context.Context,
	state systemServiceState,
	rawState string,
) error {
	var witness systemCanaryWitness
	outcome := "pending"
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked string
		if err := tx.Raw(
			"SELECT id FROM data_sources WHERE id=? AND deleted_at IS NULL FOR UPDATE",
			state.CanarySourceID,
		).
			Scan(&locked).Error; err != nil {
			return err
		}
		if locked != state.CanarySourceID {
			outcome = "source_failed"
			return nil
		}
		if err := checkSystemReceipt(ctx, tx, state); err != nil {
			return err
		}
		var err error
		outcome, err = systemCanaryOutcome(ctx, tx, state)
		if err != nil || outcome != "success" {
			return err
		}
		raw, err := s.c.Client.HGet(ctx, s.c.prefix+"system_sources", state.CanarySourceID).Result()
		if err != nil {
			return err
		}
		var entry systemSourceRegistration
		if json.Unmarshal([]byte(raw), &entry) != nil || entry.Epoch != state.Epoch {
			return ErrNotReady
		}
		authority, err := automaticBackgroundSourceAuthority(
			ctx,
			s.db,
			tx,
			state.CanarySourceID,
			entry.ConfigSHA256,
		)
		if err != nil {
			if errors.Is(err, ErrRecoveryRejected) {
				outcome = "source_failed"
				return nil
			}
			return err
		}
		if authority != entry.NormalSourceAuthority {
			outcome = "source_failed"
			return nil
		}
		publication, err := canaryPublicationDigest(ctx, tx, state.CanarySourceID)
		if errors.Is(err, errCanaryEvidenceLimit) {
			outcome = "candidate_limit"
			return nil
		}
		if err != nil {
			return err
		}
		witness = systemCanaryWitness{
			Version: 1, Epoch: state.Epoch, SourceID: state.CanarySourceID, RunID: state.CanaryRunID,
			RuntimeSHA: state.RuntimeSHA, WorkerValue: state.WorkerValue, ConfigSHA256: authority.ConfigSHA256,
			AuthoritySHA256:   authority.AuthoritySHA256,
			ExecutionSHA256:   authority.ExecutionSHA256,
			PublicationSHA256: publication,
		}
		encoded, err := json.Marshal(witness)
		if err != nil {
			return err
		}
		var current types.SystemSetting
		err = tx.First(&current, "key=?", canaryWitnessKey(state)).Error
		if err == nil {
			var previous systemCanaryWitness
			if json.Unmarshal(current.Value, &previous) != nil || previous != witness {
				return ErrNotReady
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return tx.Create(
			&types.SystemSetting{
				Key:       canaryWitnessKey(state),
				Value:     types.JSON(encoded),
				ValueType: "string",
				Category:  "integration",
			},
		).Error
	})
	if err != nil || outcome == "pending" {
		return err
	}
	next := state
	if outcome == "success" {
		next.Phase = "ramp"
		next.CanaryProofSHA256, err = RecoveryDigest(witness)
		if err != nil {
			return err
		}
	} else {
		next.CanarySourceID, next.CanaryRunID = "", ""
		next.CanaryStartedMS = 0
	}
	encoded, err := json.Marshal(next)
	if err != nil {
		return err
	}
	return redis.NewScript(`
if redis.call('GET',KEYS[1])~=ARGV[1] or redis.call('EXISTS',KEYS[2])~=0 then return redis.error_reply('SYSTEM_CANARY_CHANGED') end
local s=cjson.decode(ARGV[1]);local next=cjson.decode(ARGV[2])
local t=redis.call('TIME');local now=t[1]*1000+math.floor(t[2]/1000)
if next.phase=='ramp' then next.next_admission_ms=now+5000;next.ramp_until_ms=now+60000
else redis.call('HSET',KEYS[4],s.canary_source_id,tostring(now+60000)) end
redis.call('XADD',KEYS[3],'*','event','automatic_canary_'..ARGV[3],
 'epoch',s.epoch,'source_id',s.canary_source_id,'run_id',s.canary_run_id,'publication_proof',next.canary_proof_sha256 or '')
redis.call('SET',KEYS[1],cjson.encode(next));return 1`).Run(ctx, s.c.Client,
		[]string{
			s.c.prefix + "system_service",
			s.c.prefix + "trip",
			s.c.prefix + "system_journal",
			s.c.prefix + "system_source_backoff",
		}, rawState, string(encoded), outcome).
		Err()
}
