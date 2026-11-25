package feishusafety

import "fmt"

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
 if not ok or s.version~=1 or s.guard_fingerprint~=ARGV[1] or s.epoch~=ARGV[%d] or
  (s.phase~='canary' and s.phase~='ramp' and s.phase~='normal') or
  (ARGV[%d]~='true' and ARGV[%d]~='1') then return redis.error_reply('SYSTEM_SERVICE_CLOSED') end
 local registered=redis.call('HGET',KEYS[%d],ARGV[%d])
 if not registered then return redis.error_reply('SYSTEM_SOURCE_MISSING') end
 local entry_ok,entry=pcall(cjson.decode,registered)
 if not entry_ok or entry.epoch~=s.epoch or entry.source_id~=ARGV[%d] or entry.config_sha256~=ARGV[%d] then
  return redis.error_reply('SYSTEM_SOURCE_CHANGED') end
 if s.phase=='canary' and (s.canary_source_id~=ARGV[%d] or
  (ARGV[%d]~='' and s.canary_run_id~=ARGV[%d])) then return redis.error_reply('SYSTEM_CANARY_SCOPE_DENIED') end
 local t=redis.call('TIME');local now=t[1]*1000+math.floor(t[2]/1000)
 local lease=redis.call('ZSCORE',KEYS[%d],s.worker_id)
 if redis.call('HLEN',KEYS[%d])~=1 or redis.call('HGET',KEYS[%d],s.worker_id)~=s.worker_value or
  redis.call('GET',KEYS[%d])~=s.membership_revision or not lease or tonumber(lease)<=now then
  return redis.error_reply('SYSTEM_RUNTIME_CHANGED') end
 if ARGV[%d]~='' and (not tonumber(ARGV[%d]) or tonumber(ARGV[%d])<1) then
  return redis.error_reply('SYSTEM_GENERATION_INVALID') end
 normal=true
end
`, requiredArg, requiredArg, stateKey, epochArg, validatedArg, validatedArg,
		sourceKey, sourceArg, sourceArg, configArg, sourceArg, runArg, runArg,
		leasesKey, workersKey, workersKey, revisionKey, runArg, generationArg, generationArg)
}
