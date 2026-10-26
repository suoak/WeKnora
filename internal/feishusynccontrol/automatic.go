package feishusynccontrol

import (
	"context"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// All discovered controllers are retained until an explicit rollout reset. A
// disappeared worker cannot silently shrink the expected inventory and reopen.
// Redis TIME supplies lease time; no gateway/online-health metrics are invented.
var automaticPermit = redis.NewScript(`
local t=redis.call('TIME');local now=t[1]*1000+math.floor(t[2]/1000)
if redis.call('EXISTS',KEYS[4])==1 then return redis.error_reply('TRIPPED') end
local workers=redis.call('HGETALL',KEYS[1])
if #workers==0 or #ARGV<4 then return redis.error_reply('CONSUMERS_NOT_READY') end
if (redis.call('GET',KEYS[8]) or '0')~=ARGV[3] then
 redis.call('DEL',KEYS[3]);return redis.error_reply('REGISTRY_CHANGED')
end
local consumers={}
for i=4,#ARGV do consumers[ARGV[i]]=true end
local draining=redis.call('ZRANGEBYSCORE',KEYS[5],now,'+inf')
for _,id in ipairs(draining) do if not redis.call('HGET',KEYS[1],id) then consumers[id]=nil end end
for i=1,#workers,2 do
 local id=workers[i];local value=workers[i+1]
 local untilms=redis.call('ZSCORE',KEYS[2],id)
 if string.sub(value,1,64)~=ARGV[1] then return redis.error_reply('POLICY_MISMATCH') end
 if not untilms or tonumber(untilms)<=now then return redis.error_reply('WORKER_LOST') end
 local admitted=redis.call('HGET',KEYS[6],id)
 if admitted and admitted~=value then return redis.error_reply('ADMITTED_INCARNATION_CONFLICT') end
 if not consumers[id] then
  if admitted then return redis.error_reply('CONSUMER_LOST') end
  local joining=redis.call('ZSCORE',KEYS[7],id)
  if joining and tonumber(joining)>now then
   redis.call('DEL',KEYS[3]);return redis.error_reply('JOINING_CONSUMER')
  end
  return redis.error_reply('CONSUMER_START_TIMEOUT')
 end
 consumers[id]=nil
end
for _,_ in pairs(consumers) do return redis.error_reply('UNREGISTERED_CONSUMER') end
local changed=false
for i=1,#workers,2 do
 if not redis.call('HGET',KEYS[6],workers[i]) then
  redis.call('HSET',KEYS[6],workers[i],workers[i+1]);changed=true
 end
 redis.call('ZREM',KEYS[7],workers[i])
end
if changed then redis.call('INCR',KEYS[8]) end
redis.call('SET',KEYS[3],ARGV[2],'PX',750);return 1`)

func (c *Controller) automaticStep(ctx context.Context) error {
	if c.Consumers == nil {
		return c.freeze(ctx, "consumer_probe_missing", true)
	}
	if time.Since(c.lastAlertAck) > time.Second {
		c.probeStage = "audit"
		event := Event{
			Kind:     "feishu_safety_heartbeat",
			Reason:   "controller_alive",
			WorkerID: c.Config.WorkerID,
		}
		if err := c.audit(ctx, event); err != nil {
			return c.freeze(ctx, "audit_unavailable", true)
		}
		c.lastAlertAck = time.Now()
	}
	// Pair the external Asynq snapshot with membership revision. A peer may
	// register or first admit a consumer while this process is inspecting it.
	// Such an obsolete snapshot closes permission but cannot prove consumer loss.
	c.probeStage = "membership_probe"
	revision, err := c.Client.Get(ctx, c.prefix+"membership_revision").Result()
	if err != nil {
		return c.freeze(ctx, automaticFailureReason("membership_probe", err), true)
	}
	c.probeStage = "consumer_probe"
	ids, err := c.Consumers(ctx)
	stage := "consumer_probe"
	if err == nil {
		stage = "permit"
		c.probeStage = stage
		args := []any{c.fingerprint, c.Guard.Fingerprint(), revision}
		for _, id := range ids {
			args = append(args, id)
		}
		keys := []string{
			c.prefix + "workers",
			c.prefix + "leases",
			c.Guard.PermitKey(),
			c.prefix + "trip",
			c.prefix + "draining",
			c.prefix + "admitted",
			c.prefix + "joining",
			c.prefix + "membership_revision",
		}
		err = automaticPermit.Run(ctx, c.Client, keys, args...).Err()
	}
	if err != nil {
		if strings.Contains(err.Error(), "JOINING_CONSUMER") ||
			strings.Contains(err.Error(), "REGISTRY_CHANGED") {
			// Lua has already withdrawn the permit. This bounded join is not a
			// lost previously admitted consumer and never resumes the queue.
			return ErrNotReady
		}
		// Startup remains outbound-closed, without pausing a healthy fresh queue.
		// Once admitted, every loss latches and requires explicit recovery.
		if !c.admitted && time.Since(c.started) < 30*time.Second &&
			!strings.Contains(err.Error(), "MISMATCH") &&
			!strings.Contains(err.Error(), "TRIPPED") &&
			!strings.Contains(err.Error(), "CONSUMER_START_TIMEOUT") &&
			!strings.Contains(err.Error(), "CONSUMER_LOST") &&
			!strings.Contains(err.Error(), "WORKER_LOST") &&
			!strings.Contains(err.Error(), "INCARNATION_CONFLICT") {
			_ = c.Client.Del(ctx, c.Guard.PermitKey()).Err()
			return ErrNotReady
		}
		return c.freeze(ctx, automaticFailureReason(stage, err), true)
	}
	c.admitted = true
	return nil
}

// Drain is a graceful, fenced departure, not recovery from an unexpected loss.
// A failed fencing/Redis action retains membership so peers still fail closed.
// The bounded tombstone tolerates Asynq's old heartbeat until worker shutdown.
func (c *Controller) Drain(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.departed = true
	c.Guard.CloseLocal()
	CancelLocal()
	if err := c.Stop(ctx); err != nil {
		return err
	}
	if err := c.audit(ctx, Event{
		Kind: "feishu_safety_drain", Reason: "graceful_fenced_departure",
		WorkerID: c.Config.WorkerID, Fenced: true,
	}); err != nil {
		return err
	}
	return redis.NewScript(`
local old=redis.call('HGET',KEYS[1],ARGV[1])
if old and old~=ARGV[2] then return redis.error_reply('INCARNATION_CONFLICT') end
local admitted=redis.call('HGET',KEYS[5],ARGV[1])
if admitted and admitted~=ARGV[2] then return redis.error_reply('INCARNATION_CONFLICT') end
local t=redis.call('TIME');local now=t[1]*1000+math.floor(t[2]/1000)
redis.call('ZADD',KEYS[4],now+30000,ARGV[1])
redis.call('HDEL',KEYS[1],ARGV[1]);redis.call('ZREM',KEYS[2],ARGV[1])
redis.call('HDEL',KEYS[5],ARGV[1]);redis.call('ZREM',KEYS[6],ARGV[1]);redis.call('INCR',KEYS[7])
if redis.call('HLEN',KEYS[1])==0 then redis.call('DEL',KEYS[3]) end
return 1`).Run(ctx, c.Client,
		[]string{
			c.prefix + "workers",
			c.prefix + "leases",
			c.Guard.PermitKey(),
			c.prefix + "draining",
			c.prefix + "admitted",
			c.prefix + "joining",
			c.prefix + "membership_revision",
		}, c.Config.WorkerID, c.fingerprint+":"+c.incarnation).
		Err()
}
