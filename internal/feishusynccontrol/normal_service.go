package feishusynccontrol

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const NormalServiceSetting = "integration.feishu.normal_service"

// NormalServiceAuthorization approves the service's live authorization policy,
// not a source list or a particular deployment. Expiry bounds activation only;
// the service remains authorized until explicitly revoked. Every source and SQL
// execution is checked independently, including after worker replacement.
type NormalServiceAuthorization struct {
	Version             int    `json:"version"`
	ID                  string `json:"id"`
	Namespace           string `json:"namespace"`
	ApprovalKeyID       string `json:"approval_key_id"`
	ActorID             string `json:"actor_id"`
	GuardFingerprint    string `json:"guard_fingerprint"`
	Purpose             string `json:"purpose"`
	SourcePolicy        string `json:"source_policy"`
	Validity            string `json:"validity"`
	CreatedMS           int64  `json:"created_ms"`
	ActivationExpiresMS int64  `json:"activation_expires_ms"`
	Signature           string `json:"signature"`
}

type NormalServiceRecord struct {
	Authorization NormalServiceAuthorization `json:"authorization"`
	Active        bool                       `json:"active"`
	ActivatedMS   int64                      `json:"activated_ms"`
}

// NormalSourceAuthority identifies the actual current source binding. It is
// produced by trusted database policy, never a task payload or HTTP header.
type NormalSourceAuthority struct {
	SourceID        string `json:"source_id"`
	KBID            string `json:"knowledge_base_id"`
	ConfigSHA256    string `json:"config_sha256"`
	AuthoritySHA256 string `json:"authority_sha256"`
	TenantID        uint64 `json:"tenant_id"`
}

type normalSourceAuthorityKey struct{}

type normalServiceConfiguration struct {
	source  func(context.Context, *gorm.DB, string, string) (NormalSourceAuthority, error)
	store   func(context.Context, NormalServiceRecord, *NormalServiceRecord) error
	saved   *NormalServiceRecord
	pending bool
}

// ConfigureNormalService loads the durable activation marker at startup. Redis
// state loss cannot silently become a fresh activation: an existing SQL marker
// requires its exact independently signed Redis record to remain present.
func (m *RecoveryManager) ConfigureNormalService(
	source func(context.Context, *gorm.DB, string, string) (NormalSourceAuthority, error),
	store func(context.Context, NormalServiceRecord, *NormalServiceRecord) error,
	saved *NormalServiceRecord,
) error {
	if source == nil || store == nil || (saved != nil && m.verifyNormalAuthorization(saved.Authorization, false) != nil) {
		return ErrRecoveryRejected
	}
	m.normalService.Store(&normalServiceConfiguration{source: source, store: store, saved: saved})
	m.controller.steadyManager = m
	return nil
}

func (m *RecoveryManager) verifyNormalAuthorization(a NormalServiceAuthorization, activating bool) error {
	namespace := strings.TrimSuffix(strings.TrimPrefix(m.controller.prefix, "feishu:safety:{"), "}:")
	_, idErr := uuid.Parse(a.ID)
	_, sourceErr := hex.DecodeString(m.controller.Config.SourceSHA)
	if a.Version != 1 || idErr != nil || a.Namespace != namespace || a.ApprovalKeyID != m.keyID ||
		sourceErr != nil || len(m.controller.Config.SourceSHA) != 40 ||
		m.controller.Config.Scope != "all" ||
		a.ActorID == "" || types.IsSyntheticUserID(a.ActorID) || a.GuardFingerprint != m.controller.Guard.Fingerprint() ||
		a.Purpose != "normal_feishu_sync" || a.SourcePolicy != "current_authorized_sources_v1" ||
		a.Validity != "until_revoked" || a.CreatedMS <= 0 || a.ActivationExpiresMS <= a.CreatedMS ||
		a.ActivationExpiresMS-a.CreatedMS > int64(15*time.Minute/time.Millisecond) {
		return ErrRecoveryRejected
	}
	if activating {
		now := time.Now().UnixMilli()
		if a.CreatedMS > now+5000 || a.ActivationExpiresMS <= now {
			return ErrRecoveryRejected
		}
	}
	signature, err := hex.DecodeString(a.Signature)
	a.Signature = ""
	canonical, canonicalErr := recoveryCanonical(a)
	if err != nil || canonicalErr != nil || !ed25519.Verify(m.key, canonical, signature) {
		return ErrRecoveryRejected
	}
	return nil
}

func (m *RecoveryManager) PlanNormalService(ctx context.Context, actor string) (NormalServiceAuthorization, error) {
	if m.normalService.Load() == nil || actor == "" || m.steadyReady(ctx) != nil {
		return NormalServiceAuthorization{}, ErrRecoveryRejected
	}
	now, err := m.controller.Client.Time(ctx).Result()
	if err != nil {
		return NormalServiceAuthorization{}, err
	}
	return NormalServiceAuthorization{
		Version: 1, ID: uuid.NewString(), ApprovalKeyID: m.keyID, ActorID: actor,
		Namespace:        strings.TrimSuffix(strings.TrimPrefix(m.controller.prefix, "feishu:safety:{"), "}:"),
		GuardFingerprint: m.controller.Guard.Fingerprint(), Purpose: "normal_feishu_sync",
		SourcePolicy: "current_authorized_sources_v1", Validity: "until_revoked",
		CreatedMS: now.UnixMilli(), ActivationExpiresMS: now.Add(15 * time.Minute).UnixMilli(),
	}, nil
}

func (m *RecoveryManager) normalRecord(ctx context.Context) (*NormalServiceRecord, error) {
	cfg := m.normalService.Load()
	if cfg == nil {
		return nil, nil
	}
	raw, err := m.controller.Client.Get(ctx, m.controller.prefix+"normal_service").Result()
	if errors.Is(err, redis.Nil) && (cfg.saved == nil || cfg.pending) {
		return nil, nil
	}
	var record NormalServiceRecord
	if err != nil || cfg.saved == nil || json.Unmarshal([]byte(raw), &record) != nil ||
		record != *cfg.saved || m.verifyNormalAuthorization(record.Authorization, false) != nil {
		return nil, ErrRecoveryRejected
	}
	if !record.Active {
		return nil, nil
	}
	return &record, nil
}

// NormalServiceActive is a service-level boundary, never source read authority.
func (m *RecoveryManager) NormalServiceActive(ctx context.Context) (bool, error) {
	record, err := m.normalRecord(ctx)
	return record != nil, err
}

func (m *RecoveryManager) ActivateNormalService(
	ctx context.Context, authorization NormalServiceAuthorization, actor string,
) error {
	if !m.operation.TryLock() {
		return ErrRecoveryRejected
	}
	defer m.operation.Unlock()
	cfg := m.normalService.Load()
	if cfg == nil || authorization.ActorID != actor || m.verifyNormalAuthorization(authorization, true) != nil {
		return ErrRecoveryRejected
	}
	previousRaw := ""
	if cfg.saved != nil && !cfg.saved.Active {
		if cfg.saved.Authorization.ID == authorization.ID {
			return ErrRecoveryRejected
		}
		raw, readErr := m.controller.Client.Get(ctx, m.controller.prefix+"normal_service").Result()
		var previous NormalServiceRecord
		if readErr != nil || json.Unmarshal([]byte(raw), &previous) != nil || previous != *cfg.saved {
			return ErrRecoveryRejected
		}
		previousRaw = raw
	}
	if cfg.saved != nil && cfg.saved.Active {
		if cfg.saved.Authorization == authorization {
			if _, err := m.normalRecord(ctx); err == nil {
				return nil
			}
			if _, err := m.controller.Client.Get(ctx, m.controller.prefix+"normal_service").Result(); !errors.Is(err, redis.Nil) {
				return ErrRecoveryRejected
			}
		} else {
			return ErrRecoveryRejected
		}
	}
	if err := m.steadyReady(ctx); err != nil {
		return err
	}
	now, err := m.controller.Client.Time(ctx).Result()
	if err != nil {
		return err
	}
	record := NormalServiceRecord{Authorization: authorization, Active: true, ActivatedMS: now.UnixMilli()}
	if cfg.saved != nil && cfg.saved.Authorization == authorization && cfg.saved.Active {
		record = *cfg.saved
	} else {
		if err := cfg.store(ctx, record, cfg.saved); err != nil {
			return err
		}
	}
	next := *cfg
	next.saved = &record
	next.pending = true
	m.normalService.Store(&next)
	raw, _ := json.Marshal(record)
	err = m.controller.Client.Eval(ctx, `
if redis.call('EXISTS',KEYS[1])~=0 or redis.call('GET',KEYS[2])~=ARGV[1] or
 redis.call('PTTL',KEYS[2])<=0 then return redis.error_reply('NORMAL_SERVICE_NOT_READY') end
if (redis.call('GET',KEYS[3]) or '')~=ARGV[5] then return redis.error_reply('NORMAL_SERVICE_ALREADY_ACTIVATED') end
redis.call('SET',KEYS[3],ARGV[2])
redis.call('DEL',KEYS[4])
redis.call('XADD',KEYS[5],'*','event','normal_service_activated','grant_id',ARGV[3],'actor_id',ARGV[4])
return 1`, []string{
		m.controller.prefix + "trip", m.controller.Guard.PermitKey(), m.controller.prefix + "normal_service",
		"asynq:{approved_sync}:paused", m.controller.prefix + "control_audit",
	}, m.controller.Guard.Fingerprint(), string(raw), authorization.ID, actor, previousRaw).Err()
	finished := next
	finished.pending = false
	m.normalService.Store(&finished)
	return err
}

func (m *RecoveryManager) DeactivateNormalService(ctx context.Context) error {
	if !m.operation.TryLock() {
		return ErrRecoveryRejected
	}
	defer m.operation.Unlock()
	cfg := m.normalService.Load()
	if cfg == nil || cfg.saved == nil || !cfg.saved.Active {
		return ErrRecoveryRejected
	}
	if err := m.controller.Guard.Trip(ctx, "normal_service_revoked"); err != nil {
		return err
	}
	record := *cfg.saved
	record.Active = false
	if err := cfg.store(ctx, record, cfg.saved); err != nil {
		return err
	}
	raw, _ := json.Marshal(record)
	if err := m.controller.Client.Set(ctx, m.controller.prefix+"normal_service", string(raw), 0).Err(); err != nil {
		return err
	}
	next := *cfg
	next.saved = &record
	m.normalService.Store(&next)
	return nil
}

func (m *RecoveryManager) checkNormalSource(
	ctx context.Context, tx *gorm.DB, source, config string,
) (context.Context, bool, error) {
	record, err := m.normalRecord(ctx)
	if err != nil || record == nil {
		return ctx, false, err
	}
	identity, _ := feishusafety.RecoveryOutboundExecution(ctx)
	if identity.Plan != "" && identity.Plan != record.Authorization.ID {
		return ctx, false, nil // Existing signed incident recovery remains separate.
	}
	authority, err := m.normalService.Load().source(ctx, tx, source, config)
	if err != nil {
		return ctx, true, steadySourceChanged()
	}
	if authority.SourceID != source || authority.ConfigSHA256 != config || authority.TenantID == 0 || authority.KBID == "" {
		return ctx, true, steadySourceChanged()
	}
	if err := m.steadyReady(ctx); err != nil {
		return ctx, true, err
	}
	ctx = feishusafety.WithRecoveryContext(ctx, feishusafety.RecoveryContext{
		Plan: record.Authorization.ID, Config: config, Run: identity.Run, Generation: identity.Generation,
	})
	ctx = context.WithValue(ctx, normalSourceAuthorityKey{}, authority)
	if _, claimed := types.SyncExecutionFromContext(ctx); claimed {
		ctx = types.WithReconciliationCommitGuard(ctx, func(tx *gorm.DB) error {
			_, _, err := m.checkNormalSource(tx.Statement.Context, tx, source, config)
			return err
		})
	}
	return ctx, true, nil
}

func (m *RecoveryManager) bindNormalSource(ctx context.Context, source, config string) (context.Context, bool, error) {
	work, handled, err := m.checkNormalSource(ctx, nil, source, config)
	if !handled || err != nil {
		return work, handled, err
	}
	authority, ok := work.Value(normalSourceAuthorityKey{}).(NormalSourceAuthority)
	if !ok {
		return ctx, true, ErrRecoveryRejected
	}
	identity, _ := feishusafety.RecoveryOutboundExecution(work)
	entry := struct {
		NormalSourceAuthority
		Plan string `json:"plan"`
	}{authority, identity.Plan}
	raw, _ := json.Marshal(entry)
	err = m.controller.Client.Eval(ctx, `
local raw=redis.call('GET',KEYS[1]);if not raw then return redis.error_reply('NORMAL_SERVICE_MISSING') end
local s=cjson.decode(raw)
if s.active~=true or s.authorization.id~=ARGV[1] or redis.call('EXISTS',KEYS[4])~=0 then
 return redis.error_reply('NORMAL_SERVICE_CHANGED') end
if redis.call('HGET',KEYS[2],ARGV[2])~=ARGV[3] then
 redis.call('HSET',KEYS[2],ARGV[2],ARGV[3])
 redis.call('XADD',KEYS[3],'MAXLEN','~',10000,'*','event','normal_source_registered',
  'grant_id',ARGV[1],'source_id',ARGV[2],'tenant_id',ARGV[4],'knowledge_base_id',ARGV[5])
end
return 1`, []string{
		m.controller.prefix + "normal_service", m.controller.prefix + "normal_sources",
		m.controller.prefix + "control_audit", m.controller.prefix + "trip",
	}, identity.Plan, source, string(raw), authority.TenantID, authority.KBID).Err()
	return work, true, err
}

// A normal service grant outlives incident certificates. Current incident work
// may renew its existing lease; an expired/stale incident cannot stop ordinary
// sources or silently regain authority on a replacement worker.
func (c *Controller) renewNormalIncidentHealth(ctx context.Context) error {
	raw, err := c.Client.Get(ctx, c.prefix+"recovery").Result()
	if errors.Is(err, redis.Nil) {
		return nil
	}
	if err != nil {
		return err
	}
	var state recoveryMode
	if json.Unmarshal([]byte(raw), &state) != nil || state.Steady == nil ||
		state.RuntimeSHA != c.Config.SourceSHA || state.WorkerID != c.Config.WorkerID ||
		state.Incarnation != c.incarnation || state.ExpiresMS <= time.Now().UnixMilli() {
		return nil
	}
	err = c.renewSteadyHealth(ctx)
	if err != nil && strings.Contains(err.Error(), "STEADY_HEALTH_LOST") {
		return nil // The incident lane stays closed; its grant is never recreated.
	}
	return err
}
