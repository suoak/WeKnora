package feishusynccontrol

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type systemSourceRegistration struct {
	NormalSourceAuthority
	Epoch string `json:"epoch"`
}

type (
	systemPendingExecution struct {
		SourceID, RunID string
		TenantID        uint64
	}
	systemPendingExecutionKey struct{}
)

func systemPendingExecutionFromContext(ctx context.Context) (systemPendingExecution, bool) {
	p, ok := ctx.Value(systemPendingExecutionKey{}).(systemPendingExecution)
	return p, ok
}

// PrepareAutomaticSourceExecution is a worker-only continuation of an already
// persisted intent. The payload supplies identifiers only; current SQL source,
// log, tenant and status are loaded before a private context proof is installed.
func PrepareAutomaticSourceExecution(ctx context.Context, source, run string, tenant uint64) (context.Context, bool, error) {
	m := Recovery()
	if m == nil || m.system == nil {
		return ctx, false, nil
	}
	s := m.system
	if _, keyed := types.TenantAPIKeyScopeFromContext(ctx); keyed {
		return ctx, true, ErrRecoveryRejected
	}
	if _, principal := ctx.Value(types.PrincipalContextKey).(types.Principal); principal {
		return ctx, true, ErrRecoveryRejected
	}
	var ds types.DataSource
	if err := s.db.WithContext(ctx).First(&ds, "id=? AND tenant_id=?", source, tenant).Error; err != nil {
		return ctx, true, err
	}
	if !types.FeishuConnectorType(ds.Type) {
		return ctx, false, nil
	}
	var log types.SyncLog
	if err := s.db.WithContext(ctx).First(&log, "id=? AND data_source_id=? AND tenant_id=?", run, source, tenant).Error; err != nil {
		return ctx, true, err
	}
	if log.Status != types.SyncLogStatusRunning {
		return ctx, true, types.ErrStaleSyncExecution
	}
	config, err := feishusafety.SourceConfigFingerprint(ds.ID, ds.KnowledgeBaseID, ds.Type, ds.LegacyAccessState, ds.TenantID, ds.Config)
	if err != nil {
		return ctx, true, err
	}
	ctx = context.WithValue(ctx, systemPendingExecutionKey{}, systemPendingExecution{SourceID: source, RunID: run, TenantID: tenant})
	ctx = feishusafety.WithRecoveryExecution(ctx, config, run, 0)
	work, err := s.bindSource(ctx, source, config)
	if err != nil {
		return ctx, true, err
	}
	work = types.WithSyncAdmissionGuard(work, source, run, tenant, func(tx *gorm.DB, owner types.SyncExecution) error {
		marked := types.WithSyncExecution(tx.Statement.Context, owner)
		marked = feishusafety.WithRecoveryExecution(marked, config, owner.SyncLogID, owner.Generation)
		_, err := s.checkSource(marked, tx, source, config)
		return err
	})
	return work, true, nil
}

func (s *automaticService) queueSource(ctx context.Context, source, config string) (bool, error) {
	if _, err := automaticSourceAuthority(ctx, s.db, nil, source, config, s.keys); err != nil {
		if errors.Is(err, ErrRecoveryRejected) {
			return true, err
		}
		return true, errors.Join(ErrNormalQueueSafetyUnavailable, err)
	}
	trip, err := systemRead(ctx, s.c.Client, s.c.prefix+"trip")
	if err != nil {
		return true, errors.Join(ErrNormalQueueSafetyUnavailable, err)
	}
	if trip != "" && !automaticServiceRecoverableTrip(trip) {
		return true, ErrNormalQueueStopped
	}
	// Accept valid intent without asserting a healthy consumer or creating an
	// outbound grant. Actual workers must bind the current system epoch below.
	return true, nil
}

func (s *automaticService) bindSource(ctx context.Context, source, config string) (context.Context, error) {
	authority, err := automaticSourceAuthority(ctx, s.db, nil, source, config, s.keys)
	if err != nil {
		return ctx, err
	}
	state, rawState, err := s.current(ctx)
	if err != nil {
		return ctx, errors.Join(ErrNotReady, err)
	}
	if state.Phase == "canary" && state.CanarySourceID == "" {
		// A bounded Canary must not select an existing large corpus and then
		// hold every unrelated source behind its evidence limit. Large sources
		// remain eligible for normal service after another candidate qualifies.
		var resources int64
		if err := s.db.WithContext(ctx).Raw(`SELECT count(*) FROM
 (SELECT id FROM feishu_legacy_resources WHERE data_source_id=? AND state='active' LIMIT 257) r`, source).
			Scan(&resources).Error; err != nil {
			return ctx, err
		}
		if resources > 256 {
			return ctx, ErrNotReady
		}
	}
	identity, _ := feishusafety.RecoveryOutboundExecution(ctx)
	run := identity.Run
	if execution, ok := types.SyncExecutionFromContext(ctx); ok {
		if execution.DataSourceID != source || execution.TenantID != authority.TenantID {
			return ctx, ErrRecoveryRejected
		}
		run = execution.SyncLogID
		identity.Generation = execution.Generation
	}
	if run == "" {
		return ctx, ErrNotReady
	}
	entry := systemSourceRegistration{NormalSourceAuthority: authority, Epoch: state.Epoch}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return ctx, err
	}
	err = registerSystemSource.Run(ctx, s.c.Client, []string{
		s.c.prefix + "system_service", s.c.prefix + "system_sources",
		s.c.prefix + "trip", s.c.Guard.PermitKey(), s.c.prefix + "system_journal", s.c.prefix + "system_source_backoff",
	},
		rawState, source, run, string(encoded), s.c.Guard.Fingerprint()).Err()
	if err != nil {
		return ctx, errors.Join(ErrNotReady, err)
	}
	ctx = feishusafety.WithRecoveryContext(ctx, feishusafety.RecoveryContext{Plan: state.Epoch, Config: config, Run: run, Generation: identity.Generation})
	ctx = context.WithValue(ctx, normalSourceAuthorityKey{}, authority)
	return s.checkSource(ctx, nil, source, config)
}

var registerSystemSource = redis.NewScript(`
if redis.call('GET',KEYS[1])~=ARGV[1] or redis.call('EXISTS',KEYS[3])~=0 or
 redis.call('GET',KEYS[4])~=ARGV[5] or redis.call('PTTL',KEYS[4])<=0 then return redis.error_reply('SYSTEM_SOURCE_NOT_READY') end
local s=cjson.decode(ARGV[1]);local entry=cjson.decode(ARGV[4])
if entry.epoch~=s.epoch or entry.source_id~=ARGV[2] then return redis.error_reply('SYSTEM_SOURCE_BINDING_CHANGED') end
local t=redis.call('TIME');local now=t[1]*1000+math.floor(t[2]/1000)
local backoff=redis.call('HGET',KEYS[6],ARGV[2])
if backoff and tonumber(backoff)>now then return redis.error_reply('SYSTEM_SOURCE_BACKOFF') end
local changed=false
if s.phase=='canary' then
 if s.canary_source_id=='' then s.canary_source_id=ARGV[2];s.canary_run_id=ARGV[3];s.canary_started_ms=now;changed=true
 elseif s.canary_source_id~=ARGV[2] or s.canary_run_id~=ARGV[3] then return redis.error_reply('SYSTEM_CANARY_OTHER_SOURCE') end
elseif s.phase=='ramp' then
 local registered=redis.call('HGET',KEYS[2],ARGV[2]);local same=false
 if registered then local previous=cjson.decode(registered);same=previous.epoch==s.epoch end
 if not same then
  if s.next_admission_ms>now then return redis.error_reply('SYSTEM_SOURCE_RAMP_BACKOFF') end
  s.next_admission_ms=now+5000;changed=true
 end
elseif s.phase~='normal' then return redis.error_reply('SYSTEM_SERVICE_CLOSED') end
if redis.call('HGET',KEYS[2],ARGV[2])~=ARGV[4] then
 redis.call('XADD',KEYS[5],'*','event','automatic_source_registered','epoch',s.epoch,
  'source_id',ARGV[2],'tenant_id',entry.tenant_id,'knowledge_base_id',entry.knowledge_base_id)
 redis.call('HSET',KEYS[2],ARGV[2],ARGV[4])
end
if changed then redis.call('SET',KEYS[1],cjson.encode(s)) end
return 1`)
