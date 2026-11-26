package feishusafety

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

// Component fixtures use the pinned SDK's descriptor, never a second wire
// schema. They do not attest a real Consumer or full APP qualification.
func automaticConsumerMessage(t *testing.T, server string) *dynamicpb.Message {
	t.Helper()
	descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName("asynq.ServerInfo")
	require.NoError(t, err)
	message := dynamicpb.NewMessage(descriptor.(protoreflect.MessageDescriptor))
	fields := message.Descriptor().Fields()
	for name, value := range map[protoreflect.Name]string{"host": "worker", "server_id": server, "status": "active"} {
		message.Set(fields.ByName(name), protoreflect.ValueOfString(value))
	}
	message.Set(fields.ByName("pid"), protoreflect.ValueOfInt32(123))
	message.Set(fields.ByName("concurrency"), protoreflect.ValueOfInt32(8))
	queues := message.Mutable(fields.ByName("queues")).Map()
	for _, name := range []string{"sync", "approved_sync"} {
		queues.Set(protoreflect.ValueOfString(name).MapKey(), protoreflect.ValueOfInt32(1))
	}
	return message
}

func automaticConsumerWire(t *testing.T, message proto.Message) []byte {
	t.Helper()
	raw, err := (proto.MarshalOptions{Deterministic: true}).Marshal(message)
	require.NoError(t, err)
	return raw
}

func reversedAutomaticConsumerWire(t *testing.T, raw []byte) []byte {
	t.Helper()
	var fields [][]byte
	for len(raw) > 0 {
		number, kind, tagLength := protowire.ConsumeTag(raw)
		require.Positive(t, tagLength)
		valueLength := protowire.ConsumeFieldValue(number, kind, raw[tagLength:])
		require.GreaterOrEqual(t, valueLength, 0)
		n := tagLength + valueLength
		fields = append(fields, append([]byte(nil), raw[:n]...))
		raw = raw[n:]
	}
	var reversed []byte
	for i := len(fields) - 1; i >= 0; i-- {
		reversed = append(reversed, fields[i]...)
	}
	return reversed
}

func automaticConsumerRoot(t *testing.T, g *Guard, client *redis.Client, server string, raw []byte) map[string]any {
	t.Helper()
	root := map[string]any{"version": 1, "namespace": g.prefix, "guard_fingerprint": g.Fingerprint(), "phase": "normal", "worker_id": "worker:123", "consumer_server_id": server, "consumer_metadata_sha1": strings.Repeat("0", 40)}
	automaticConsumerStoreRoot(t, g, client, root)
	require.NoError(t, client.Set(context.Background(), "asynq:servers:{worker:123:"+server+"}", raw, 10*time.Second).Err())
	return root
}

func automaticConsumerStoreRoot(t *testing.T, g *Guard, client *redis.Client, root map[string]any) {
	t.Helper()
	raw, err := json.Marshal(root)
	require.NoError(t, err)
	require.NoError(t, client.Set(context.Background(), g.key("system_service"), raw, 0).Err())
}

func TestAutomaticConsumerCurrentSemanticHeartbeat(t *testing.T) {
	g, client := fixture(t, Config{})
	g.RequireAutomaticService()
	server := uuid.NewString()
	message := automaticConsumerMessage(t, server)
	original := automaticConsumerWire(t, message)
	automaticConsumerRoot(t, g, client, server, original)
	ctx := context.Background()
	rootBefore, err := client.Get(ctx, g.key("system_service")).Result()
	require.NoError(t, err)
	countBefore, err := client.DBSize(ctx).Result()
	require.NoError(t, err)
	changed := proto.Clone(message).(*dynamicpb.Message)
	changed.Set(changed.Descriptor().Fields().ByName("active_worker_count"), protoreflect.ValueOfInt32(4))
	otherCount := automaticConsumerWire(t, changed)
	otherEncoding := reversedAutomaticConsumerWire(t, original)
	require.NotEqual(t, original, otherCount)
	require.NotEqual(t, original, otherEncoding)
	decoded := dynamicpb.NewMessage(message.Descriptor())
	require.NoError(t, proto.Unmarshal(otherEncoding, decoded))
	require.True(t, proto.Equal(message, decoded))
	var digests []string
	for _, raw := range [][]byte{original, otherCount, otherEncoding} {
		require.NoError(t, client.Set(ctx, "asynq:servers:{worker:123:"+server+"}", raw, 10*time.Second).Err())
		digest, err := g.automaticConsumerDigest(ctx)
		require.NoError(t, err)
		expected := sha1.Sum(raw)
		require.Equal(t, hex.EncodeToString(expected[:]), digest)
		digests = append(digests, digest)
		require.Equal(t, rootBefore, client.Get(ctx, g.key("system_service")).Val(), "semantic read must never refresh root metadata fingerprint")
		require.Equal(t, g.Fingerprint(), client.Get(ctx, g.PermitKey()).Val(), "read cannot issue, renew or change the fixture permit")
		require.Equal(t, countBefore, client.DBSize(ctx).Val(), "read cannot create registry, leases, grants or audit state")
	}
	require.NotEqual(t, digests[0], digests[1])
	require.NotEqual(t, digests[0], digests[2])
}

func TestAutomaticConsumerInvalidStateRemainsClosed(t *testing.T) {
	for _, fault := range []string{"missing-root", "root-corrupt", "root-long", "root-version", "root-phase", "root-namespace", "root-fingerprint", "root-worker", "root-server", "metadata-missing", "metadata-long", "metadata-empty", "metadata-corrupt", "server-mismatch", "host-mismatch", "pid-mismatch", "stopped", "zero-concurrency", "missing-sync", "missing-approved", "zero-weight", "negative-weight", "context-canceled", "root-wrong-type", "metadata-wrong-type"} {
		t.Run(fault, func(t *testing.T) {
			g, client := fixture(t, Config{})
			g.RequireAutomaticService()
			ctx := context.Background()
			server := uuid.NewString()
			message := automaticConsumerMessage(t, server)
			root := automaticConsumerRoot(t, g, client, server, automaticConsumerWire(t, message))
			key := "asynq:servers:{worker:123:" + server + "}"
			fields := message.Descriptor().Fields()
			changedMessage := false
			switch fault {
			case "missing-root":
				require.NoError(t, client.Del(ctx, g.key("system_service")).Err())
			case "root-corrupt":
				require.NoError(t, client.Set(ctx, g.key("system_service"), "{secret-test-raw", 0).Err())
			case "root-long":
				require.NoError(t, client.Set(ctx, g.key("system_service"), strings.Repeat("x", 4097), 0).Err())
			case "root-version":
				root["version"] = 2
				automaticConsumerStoreRoot(t, g, client, root)
			case "root-phase":
				root["phase"] = "unknown"
				automaticConsumerStoreRoot(t, g, client, root)
			case "root-namespace":
				root["namespace"] = "feishu:safety:{different}:"
				automaticConsumerStoreRoot(t, g, client, root)
			case "root-fingerprint":
				root["guard_fingerprint"] = strings.Repeat("0", 64)
				automaticConsumerStoreRoot(t, g, client, root)
			case "root-worker":
				root["worker_id"] = "untrusted{worker}:123"
				automaticConsumerStoreRoot(t, g, client, root)
			case "root-server":
				root["consumer_server_id"] = "zzzzzzzz-zzzz-zzzz-zzzz-zzzzzzzzzzzz"
				automaticConsumerStoreRoot(t, g, client, root)
			case "root-wrong-type":
				require.NoError(t, client.Del(ctx, g.key("system_service")).Err())
				require.NoError(t, client.HSet(ctx, g.key("system_service"), "secret-test-raw", "not serialized").Err())
			case "metadata-wrong-type":
				require.NoError(t, client.Del(ctx, key).Err())
				require.NoError(t, client.HSet(ctx, key, "secret-test-raw", "not serialized").Err())
			case "metadata-missing":
				require.NoError(t, client.Del(ctx, key).Err())
			case "metadata-long":
				require.NoError(t, client.Set(ctx, key, strings.Repeat("x", 2049), 10*time.Second).Err())
			case "metadata-empty":
				require.NoError(t, client.Set(ctx, key, "", 10*time.Second).Err())
			case "metadata-corrupt":
				require.NoError(t, client.Set(ctx, key, []byte{0xff, 0xff, 0xff}, 10*time.Second).Err())
			case "server-mismatch":
				message.Set(fields.ByName("server_id"), protoreflect.ValueOfString(uuid.NewString()))
				changedMessage = true
			case "host-mismatch":
				message.Set(fields.ByName("host"), protoreflect.ValueOfString("other"))
				changedMessage = true
			case "pid-mismatch":
				message.Set(fields.ByName("pid"), protoreflect.ValueOfInt32(124))
				changedMessage = true
			case "stopped":
				message.Set(fields.ByName("status"), protoreflect.ValueOfString("stopped"))
				changedMessage = true
			case "zero-concurrency":
				message.Set(fields.ByName("concurrency"), protoreflect.ValueOfInt32(0))
				changedMessage = true
			case "missing-sync":
				message.Mutable(fields.ByName("queues")).Map().Clear(protoreflect.ValueOfString("sync").MapKey())
				changedMessage = true
			case "missing-approved":
				message.Mutable(fields.ByName("queues")).Map().Clear(protoreflect.ValueOfString("approved_sync").MapKey())
				changedMessage = true
			case "zero-weight":
				message.Mutable(fields.ByName("queues")).Map().Set(protoreflect.ValueOfString("approved_sync").MapKey(), protoreflect.ValueOfInt32(0))
				changedMessage = true
			case "negative-weight":
				message.Mutable(fields.ByName("queues")).Map().Set(protoreflect.ValueOfString("sync").MapKey(), protoreflect.ValueOfInt32(-1))
				changedMessage = true
			case "context-canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if changedMessage {
				require.NoError(t, client.Set(context.Background(), key, automaticConsumerWire(t, message), 10*time.Second).Err())
			}
			digest, err := g.automaticConsumerDigest(ctx)
			require.Error(t, err)
			require.Empty(t, digest)
			require.NotContains(t, err.Error(), "secret-test-raw")
			require.Equal(t, g.Fingerprint(), client.Get(context.Background(), g.PermitKey()).Val(), "failure cannot mutate the pre-existing fixture permit")
			if fault == "metadata-missing" {
				require.ErrorIs(t, err, ErrControlTemporarilyUnavailable)
			}
		})
	}
}
