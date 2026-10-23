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
if #workers==0 or #ARGV<3 then return redis.error_reply('CONSUMERS_NOT_READY') end
local consumers={}
for i=3,#ARGV do consumers[ARGV[i]]=true end
local draining=redis.call('ZRANGEBYSCORE',KEYS[5],now,'+inf')
for _,id in ipairs(draining) do if not redis.call('HGET',KEYS[1],id) then consumers[id]=nil end end
for i=1,#workers,2 do
 local id=workers[i];local value=workers[i+1]
 local untilms=redis.call('ZSCORE',KEYS[2],id)
 if string.sub(value,1,64)~=ARGV[1] then return redis.error_reply('POLICY_MISMATCH') end
 if not untilms or tonumber(untilms)<=now then return redis.error_reply('WORKER_LOST') end
 if not consumers[id] then return redis.error_reply('CONSUMER_LOST') end
 consumers[id]=nil
end
for _,_ in pairs(consumers) do return redis.error_reply('UNREGISTERED_CONSUMER') end
redis.call('SET',KEYS[3],ARGV[2],'PX',750);return 1`)

func (c *Controller) automaticStep(ctx context.Context) error {
	if c.Consumers == nil {
		return c.freeze(ctx, "consumer_probe_missing", true)
	}
	if time.Since(c.lastAlertAck) > time.Second {
		event := Event{
			Kind:     "feishu_safety_heartbeat",
			Reason:   "controller_alive",
			WorkerID: c.Config.WorkerID,
		}
		if err := c.Audit(event); err != nil {
			return c.freeze(ctx, "audit_unavailable", true)
		}
		c.lastAlertAck = time.Now()
	}
	ids, err := c.Consumers(ctx)
	if err == nil {
		args := []any{c.fingerprint, c.Guard.Fingerprint()}
		for _, id := range ids {
			args = append(args, id)
		}
		keys := []string{
			c.prefix + "workers",
			c.prefix + "leases",
			c.Guard.PermitKey(),
			c.prefix + "trip",
			c.prefix + "draining",
		}
		err = automaticPermit.Run(ctx, c.Client, keys, args...).Err()
	}
	if err != nil {
		// Startup remains outbound-closed, without pausing a healthy fresh queue.
		// Once admitted, every loss latches and requires explicit recovery.
		if !c.admitted && time.Since(c.started) < 30*time.Second &&
			!strings.Contains(err.Error(), "MISMATCH") &&
			!strings.Contains(err.Error(), "TRIPPED") {
			_ = c.Client.Del(ctx, c.Guard.PermitKey()).Err()
			return ErrNotReady
		}
		return c.freeze(ctx, "automatic_consumer_or_registry_unhealthy", true)
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
	if err := c.Audit(Event{
		Kind: "feishu_safety_drain", Reason: "graceful_fenced_departure",
		WorkerID: c.Config.WorkerID, Fenced: true,
	}); err != nil {
		return err
	}
	return redis.NewScript(`
local old=redis.call('HGET',KEYS[1],ARGV[1])
if old and old~=ARGV[2] then return redis.error_reply('INCARNATION_CONFLICT') end
local t=redis.call('TIME');local now=t[1]*1000+math.floor(t[2]/1000)
redis.call('ZADD',KEYS[4],now+30000,ARGV[1])
redis.call('HDEL',KEYS[1],ARGV[1]);redis.call('ZREM',KEYS[2],ARGV[1])
if redis.call('HLEN',KEYS[1])==0 then redis.call('DEL',KEYS[3]) end
return 1`).Run(ctx, c.Client,
		[]string{
			c.prefix + "workers",
			c.prefix + "leases",
			c.Guard.PermitKey(),
			c.prefix + "draining",
		}, c.Config.WorkerID, c.fingerprint+":"+c.incarnation).
		Err()
}
