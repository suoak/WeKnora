package feishusafety

import (
	"context"
	"crypto/sha1" // Current-byte CAS fingerprint, never an authentication primitive.
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"strings"

	"github.com/google/uuid"
	_ "github.com/hibiken/asynq" // Register the pinned SDK's protobuf descriptor.
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

// One read-only Lua snapshot, not an Inspector call. Bounds are checked before
// returning bytes; neither the root nor the consumer registry is modified.
var readAutomaticConsumer = redis.NewScript(`
if redis.call('STRLEN',KEYS[1])>4096 then return redis.error_reply('SYSTEM_SERVICE_CLOSED') end
local raw=redis.call('GET',KEYS[1])
if not raw then return redis.error_reply('SYSTEM_SERVICE_MISSING') end
if #raw>4096 then return redis.error_reply('SYSTEM_SERVICE_CLOSED') end
local ok,s=pcall(cjson.decode,raw)
if not ok or type(s)~='table' or s.version~=1 or s.namespace~=ARGV[1] or
 s.guard_fingerprint~=ARGV[2] or
 (s.phase~='canary' and s.phase~='ramp' and s.phase~='normal') or
 type(s.worker_id)~='string' or #s.worker_id<1 or #s.worker_id>128 or
 string.find(s.worker_id,'[{}%s]') or
 type(s.consumer_server_id)~='string' or #s.consumer_server_id~=36 then
 return redis.error_reply('SYSTEM_SERVICE_CLOSED') end
local key='asynq:servers:{'..s.worker_id..':'..s.consumer_server_id..'}'
if redis.call('STRLEN',key)>2048 then return redis.error_reply('SYSTEM_CONSUMER_INVALID') end
local rawmeta=redis.call('GET',key)
if not rawmeta then return {raw,false} end
if #rawmeta==0 or #rawmeta>2048 then return redis.error_reply('SYSTEM_CONSUMER_INVALID') end
return {raw,rawmeta}`)

// automaticConsumerDigest semantically validates the current pinned-SDK
// heartbeat. Active counts and protobuf map ordering are not worker identity.
// The returned current-byte digest MUST be checked by the immediately following
// atomic admission/budget Lua against Redis again; this read grants nothing.
func (g *Guard) automaticConsumerDigest(ctx context.Context) (string, error) {
	if g == nil {
		return "", ErrClosed
	}
	if !g.automaticServiceRequired.Load() {
		return "", nil
	}
	fail := func(code string) (string, error) { return "", g.automaticAdmissionError(ctx, errors.New(code)) }
	if ctx == nil {
		return "", ErrClosed
	}
	values, err := readAutomaticConsumer.Run(ctx, g.client, []string{g.key("system_service")}, g.prefix, g.fingerprint).
		Slice()
	if err != nil {
		if ctx.Err() != nil {
			return fail("SYSTEM_CONSUMER_UNCONFIRMED")
		}
		for _, code := range []string{
			"SYSTEM_SERVICE_MISSING",
			"SYSTEM_SERVICE_CLOSED",
			"SYSTEM_CONSUMER_UNCONFIRMED",
			"SYSTEM_CONSUMER_INVALID",
		} {
			if redis.HasErrorPrefix(err, code) {
				return fail(code)
			}
		}
		var network net.Error
		if errors.As(err, &network) {
			return fail("SYSTEM_CONSUMER_UNCONFIRMED")
		}
		return fail("SYSTEM_CONSUMER_READ_FAILED")
	}
	if len(values) != 2 {
		return fail("SYSTEM_CONSUMER_READ_FAILED")
	}
	root, ok := values[0].(string)
	if !ok || len(root) > 4096 {
		return fail("SYSTEM_SERVICE_CLOSED")
	}
	var state struct {
		Version     int    `json:"version"`
		Namespace   string `json:"namespace"`
		WorkerID    string `json:"worker_id"`
		ServerID    string `json:"consumer_server_id"`
		Fingerprint string `json:"guard_fingerprint"`
		Phase       string `json:"phase"`
	}
	if json.Unmarshal([]byte(root), &state) != nil || state.Version != 1 || state.Namespace != g.prefix ||
		state.Fingerprint != g.fingerprint {
		return fail("SYSTEM_SERVICE_CLOSED")
	}
	id, iderr := uuid.Parse(state.ServerID)
	if iderr != nil || id.String() != state.ServerID {
		return fail("SYSTEM_CONSUMER_IDENTITY_INVALID")
	}
	if values[1] == nil {
		return fail("SYSTEM_CONSUMER_UNCONFIRMED")
	}
	raw, ok := values[1].(string)
	if !ok || len(raw) == 0 || len(raw) > 2048 {
		return fail("SYSTEM_CONSUMER_INVALID")
	}
	descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName("asynq.ServerInfo")
	if err != nil {
		return fail("SYSTEM_CONSUMER_SCHEMA_UNAVAILABLE")
	}
	messageDescriptor, ok := descriptor.(protoreflect.MessageDescriptor)
	if !ok {
		return fail("SYSTEM_CONSUMER_SCHEMA_UNAVAILABLE")
	}
	fields := messageDescriptor.Fields()
	for _, name := range []protoreflect.Name{"host", "pid", "server_id", "status", "concurrency", "queues"} {
		if fields.ByName(name) == nil {
			return fail("SYSTEM_CONSUMER_SCHEMA_UNAVAILABLE")
		}
	}
	message := dynamicpb.NewMessage(messageDescriptor)
	if proto.Unmarshal([]byte(raw), message) != nil {
		return fail("SYSTEM_CONSUMER_INVALID")
	}
	host := message.Get(fields.ByName("host")).String()
	pid := message.Get(fields.ByName("pid")).Int()
	if host == "" || strings.ContainsAny(host, "{} \t\r\n") || pid <= 0 ||
		host+":"+strconv.FormatInt(pid, 10) != state.WorkerID ||
		message.Get(fields.ByName("server_id")).String() != state.ServerID {
		return fail("SYSTEM_CONSUMER_IDENTITY_INVALID")
	}
	if message.Get(fields.ByName("status")).String() != "active" ||
		message.Get(fields.ByName("concurrency")).Int() <= 0 {
		return fail("SYSTEM_CONSUMER_INACTIVE")
	}
	queues := message.Get(fields.ByName("queues")).Map()
	for _, name := range []string{"sync", "approved_sync"} {
		key := protoreflect.ValueOfString(name).MapKey()
		if !queues.Has(key) || queues.Get(key).Int() <= 0 {
			return fail("SYSTEM_CONSUMER_QUEUE_INVALID")
		}
	}
	digest := sha1.Sum([]byte(raw))
	return hex.EncodeToString(digest[:]), nil
}
