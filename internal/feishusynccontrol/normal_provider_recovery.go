package feishusynccontrol

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/redis/go-redis/v9"
)

// Same membership/consumer checks as ordinary admission; provider migration
// adds an exact CAS over the signed service record, first stop evidence and
// latest audit entry. It never reopens an old locally closed Guard.
var normalProviderPermit = redis.NewScript(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(
	automaticPermitLua,
	"if redis.call('EXISTS',KEYS[4])==1 then return redis.error_reply('TRIPPED') end",
	`local expected=ARGV[#ARGV-2]
if redis.call('GET',KEYS[4])~=expected or redis.call('GET',KEYS[9])~=ARGV[#ARGV-1] then
 return redis.error_reply('PROVIDER_RECOVERY_CHANGED') end
local last=redis.call('XREVRANGE',KEYS[10],'+','-','COUNT',1)
local seen=cjson.decode(ARGV[#ARGV])
if #last~=1 or last[1][1]~=seen.guard then return redis.error_reply('PROVIDER_RECOVERY_CHANGED') end
local control=redis.call('XREVRANGE',KEYS[11],'+','-','COUNT',1)
if #control~=1 or control[1][1]~=seen.control then return redis.error_reply('PROVIDER_RECOVERY_CHANGED') end
for _,key in ipairs({KEYS[12],KEYS[13]}) do
 local pause=redis.call('GET',key)
 if pause and pause~='feishu_safety_stop' and pause~='invalid_feishu_safety_configuration' then
  return redis.error_reply('PROVIDER_RECOVERY_PAUSED') end
end`),
	"for i=4,#ARGV do", "for i=4,#ARGV-3 do"),
	"redis.call('SET',KEYS[3],ARGV[2],'PX',750);return 1",
	`redis.call('XADD',KEYS[11],'*','event','normal_provider_stop_recovered',
 'reason',ARGV[#ARGV-2],'original_audit_id',seen.guard,'original_control_audit_id',seen.control,
 'observed_consumer_id',ARGV[4],'normal_service_record_sha1',redis.sha1hex(ARGV[#ARGV-1]))
redis.call('DEL',KEYS[4])
redis.call('SET',KEYS[3],ARGV[2],'PX',750);return 1`))

func (c *Controller) automaticPermitWithProviderRecovery(ctx context.Context, keys []string, args []any) error {
	ordinary := func() error { return automaticPermit.Run(ctx, c.Client, keys, args...).Err() }
	if !c.Guard.ProviderFailuresIsolated() || c.steadyManager == nil {
		return ordinary()
	}
	reason, err := c.Client.Get(ctx, c.prefix+"trip").Result()
	if errors.Is(err, redis.Nil) {
		return ordinary()
	}
	if err != nil {
		return err
	}
	entries, err := c.Client.XRevRangeN(ctx, c.prefix+"audit", "+", "-", 1).Result()
	if err != nil {
		return err
	}
	if len(entries) != 1 {
		return ordinary()
	}
	event := entries[0]
	evidence, _ := event.Values["http_evidence"].(string)
	if event.Values["event"] != "trip" || event.Values["reason"] != reason ||
		!c.Guard.RecoverableProviderStop(reason, evidence) {
		return ordinary()
	}
	// Legacy EmergencyStop kept the provider's first cause and recorded later
	// operator intent in control_audit only. Require a retained complete window
	// and pin its latest entry before reading that window, avoiding a TOCTOU gap.
	control, err := c.Client.XRevRangeN(ctx, c.prefix+"control_audit", "+", "-", 1).Result()
	if err != nil {
		return err
	}
	if len(control) != 1 {
		return ordinary()
	}
	// Sequences belong to individual streams, so compare their common
	// millisecond boundary, never sequences from different streams.
	stopMS, _, ok := strings.Cut(event.ID, "-")
	if !ok {
		return ordinary()
	}
	windowStart := stopMS + "-0"
	covered, err := c.Client.XRangeN(ctx, c.prefix+"control_audit", "-", "("+windowStart, 1).Result()
	if err != nil {
		return err
	}
	if len(covered) != 1 {
		return ordinary()
	}
	window, err := c.Client.XRangeN(ctx, c.prefix+"control_audit", windowStart, control[0].ID, 1001).Result()
	if err != nil {
		return err
	}
	if len(window) > 1000 {
		return ordinary()
	}
	for _, entry := range window {
		if entry.Values["event"] == "emergency_stop" ||
			(entry.Values["event"] == "feishu_safety_stop" && entry.Values["reason"] != "automatic_permit_tripped") {
			return ordinary()
		}
	}
	record, err := c.steadyManager.normalRecord(ctx)
	if err != nil {
		return err
	}
	if record == nil {
		return ordinary()
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	keys = append(keys, c.prefix+"normal_service", c.prefix+"audit", c.prefix+"control_audit",
		"asynq:{approved_sync}:paused", "asynq:{sync}:paused")
	snapshot, err := json.Marshal(map[string]string{"guard": event.ID, "control": control[0].ID})
	if err != nil {
		return err
	}
	args = append(args, reason, string(raw), string(snapshot))
	return normalProviderPermit.Run(ctx, c.Client, keys, args...).Err()
}
