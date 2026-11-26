package feishusafety

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

// RequireAutomaticService selects the explicit system-service protocol. It
// cannot grant permission: missing state/epoch/source validation remains closed,
// even if a legacy signed normal-service grant is still present in Redis.
func (g *Guard) RequireAutomaticService() {
	g.automaticServiceRequired.Store(true)
	g.RequireRecovery()
}

// Called by both check and budget allocation, so a successful pre-check cannot
// race a source/epoch/phase change and spend a stale authorization afterward.
func automaticServiceGateLua(stateKey, sourceKey, workersKey, leasesKey, revisionKey,
	sourceArg, epochArg, configArg, runArg, generationArg, validatedArg, requiredArg int,
) string {
	return fmt.Sprintf(`
if ARGV[%d]=='true' or ARGV[%d]=='1' then
 local raw=redis.call('GET',KEYS[%d]);if not raw then return redis.error_reply('SYSTEM_SERVICE_MISSING') end
 local ok,s=pcall(cjson.decode,raw)
 if not ok or s.version~=1 or type(s.namespace)~='string' or s.namespace..'system_service'~=KEYS[%d] or
  s.guard_fingerprint~=ARGV[1] or
  (s.phase~='canary' and s.phase~='ramp' and s.phase~='normal') or
  (ARGV[%d]~='true' and ARGV[%d]~='1') then return redis.error_reply('SYSTEM_SERVICE_CLOSED') end
 if s.epoch~=ARGV[%d] then return redis.error_reply('SYSTEM_SOURCE_EPOCH_CHANGED') end
 local registered=redis.call('HGET',KEYS[%d],ARGV[%d])
 if not registered then return redis.error_reply('SYSTEM_SOURCE_MISSING') end
 local entry_ok,entry=pcall(cjson.decode,registered)
 if not entry_ok or entry.epoch~=s.epoch or entry.source_id~=ARGV[%d] or entry.config_sha256~=ARGV[%d] then
  return redis.error_reply('SYSTEM_SOURCE_CHANGED') end
 if s.phase=='canary' and (s.canary_source_id~=ARGV[%d] or
  (ARGV[%d]~='' and s.canary_run_id~=ARGV[%d])) then return redis.error_reply('SYSTEM_CANARY_SCOPE_DENIED') end
 local t=redis.call('TIME');local now=t[1]*1000+math.floor(t[2]/1000)
 if s.phase=='canary' and (type(s.canary_started_ms)~='number' or
  s.canary_started_ms<=0 or now-s.canary_started_ms>=300000) then
  return redis.error_reply('SYSTEM_CANARY_SCOPE_DENIED') end
 local lease=redis.call('ZSCORE',KEYS[%d],s.worker_id)
 if redis.call('HLEN',KEYS[%d])~=1 or redis.call('HGET',KEYS[%d],s.worker_id)~=s.worker_value then
  return redis.error_reply('SYSTEM_WORKER_IDENTITY_CHANGED') end
 if redis.call('GET',KEYS[%d])~=s.membership_revision then return redis.error_reply('SYSTEM_MEMBERSHIP_CHANGED') end
 if not lease or tonumber(lease)<=now then return redis.error_reply('SYSTEM_LEASE_NOT_READY') end
 if type(s.consumer_server_id)~='string' or #s.consumer_server_id~=36 or
  type(s.consumer_metadata_sha1)~='string' or #s.consumer_metadata_sha1~=40 then
  return redis.error_reply('SYSTEM_CONSUMER_UNCONFIRMED') end
 local serverkey='asynq:servers:{'..s.worker_id..':'..s.consumer_server_id..'}'
 local meta=redis.call('GET',serverkey);local ttl=redis.call('PTTL',serverkey)
 local score=redis.call('ZSCORE','asynq:servers',serverkey)
 if not meta or ttl<=0 or not score or tonumber(score)*1000<=now or
  redis.sha1hex(meta)~=s.consumer_metadata_sha1 then return redis.error_reply('SYSTEM_CONSUMER_UNCONFIRMED') end
 if ARGV[%d]~='' and (not tonumber(ARGV[%d]) or tonumber(ARGV[%d])<1) then
  return redis.error_reply('SYSTEM_GENERATION_INVALID') end
 normal=true
end
`, requiredArg, requiredArg, stateKey, stateKey, validatedArg, validatedArg, epochArg,
		sourceKey, sourceArg, sourceArg, configArg, sourceArg, runArg, runArg,
		leasesKey, workersKey, workersKey, revisionKey, runArg, generationArg, generationArg)
}

// Denial remains fail-closed in every branch. A known observation gap is not
// permission and must not be turned into a new persistent trip by the HTTP
// watchdog before the controller can classify it. Unknown state/identity errors
// keep their hard-stop classification.
func (g *Guard) automaticAdmissionError(ctx context.Context, cause error) error {
	if !g.automaticServiceRequired.Load() {
		return fmt.Errorf("%w: %v", ErrClosed, cause)
	}
	text := cause.Error()
	for _, reason := range []string{"SYSTEM_CONSUMER_UNCONFIRMED", "SYSTEM_LEASE_NOT_READY", "SYSTEM_MEMBERSHIP_CHANGED", "SAFETY_NO_PERMIT"} {
		if strings.Contains(text, reason) {
			return errors.Join(ErrClosed, ErrControlTemporarilyUnavailable)
		}
	}
	for _, reason := range []string{"SYSTEM_SOURCE_EPOCH_CHANGED", "SYSTEM_CANARY_SCOPE_DENIED", "SYSTEM_SOURCE_MISSING", "SYSTEM_SOURCE_CHANGED"} {
		if strings.Contains(text, reason) {
			return ErrSourceAdmissionChanged
		}
	}
	var network net.Error
	if ctx.Err() != nil || errors.Is(cause, context.DeadlineExceeded) || errors.Is(cause, context.Canceled) || errors.As(cause, &network) {
		return errors.Join(ErrClosed, ErrControlTemporarilyUnavailable)
	}
	return fmt.Errorf("%w: %v", ErrClosed, cause)
}
