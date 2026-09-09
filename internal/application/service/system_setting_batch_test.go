package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type batchSettingRepo struct {
	mu       sync.Mutex
	rows     map[string]*types.SystemSetting
	batchErr error
}

func cloneSetting(row *types.SystemSetting) *types.SystemSetting {
	if row == nil {
		return nil
	}
	copy := *row
	copy.Value = append(types.JSON(nil), row.Value...)
	copy.Enum = append([]string(nil), row.Enum...)
	return &copy
}

func (r *batchSettingRepo) Get(_ context.Context, key string) (*types.SystemSetting, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneSetting(r.rows[key]), nil
}

func (r *batchSettingRepo) List(context.Context) ([]*types.SystemSetting, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	keys := make([]string, 0, len(r.rows))
	for key := range r.rows {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rows := make([]*types.SystemSetting, 0, len(keys))
	for _, key := range keys {
		rows = append(rows, cloneSetting(r.rows[key]))
	}
	return rows, nil
}

func (r *batchSettingRepo) Upsert(_ context.Context, row *types.SystemSetting) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows[row.Key] = cloneSetting(row)
	return nil
}

func (r *batchSettingRepo) UpsertBatch(_ context.Context, rows []*types.SystemSetting) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.batchErr != nil {
		return r.batchErr
	}
	// Copy all values before exposing the replacement map, mirroring the
	// repository transaction's all-or-nothing visibility.
	next := make(map[string]*types.SystemSetting, len(r.rows)+len(rows))
	for key, row := range r.rows {
		next[key] = cloneSetting(row)
	}
	for _, row := range rows {
		next[row.Key] = cloneSetting(row)
	}
	r.rows = next
	return nil
}

func (r *batchSettingRepo) Delete(_ context.Context, key string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, found := r.rows[key]
	delete(r.rows, key)
	return found, nil
}

func stringSetting(key, value string) *types.SystemSetting {
	encoded, _ := json.Marshal(value)
	return &types.SystemSetting{Key: key, Value: encoded, ValueType: "string", Category: "model"}
}

func newBatchTestService(repo *batchSettingRepo, client *redis.Client, instanceID string) *systemSettingService {
	service := &systemSettingService{
		repo: repo, rdb: client, instanceID: instanceID,
		cache: make(map[string]*types.SystemSetting),
	}
	return service
}

func TestSystemSettingUpdateBatchRefreshesLocalAndPeerCaches(t *testing.T) {
	t.Setenv("WEKNORA_REDIS_NAMESPACE", "batch-test")
	server := miniredis.RunT(t)
	clientA := redis.NewClient(&redis.Options{Addr: server.Addr()})
	clientB := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = clientA.Close(); _ = clientB.Close() })

	repo := &batchSettingRepo{rows: map[string]*types.SystemSetting{}}
	values := map[string]any{}
	for _, spec := range modelPolicySettings {
		repo.rows[spec.key] = stringSetting(spec.key, "old-"+string(spec.role))
		values[spec.key] = "new-" + string(spec.role)
	}

	serviceA := newBatchTestService(repo, clientA, "instance-a")
	serviceB := newBatchTestService(repo, clientB, "instance-b")
	serviceA.preload(context.Background())
	serviceB.preload(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	require.NoError(t, serviceB.SubscribeRedis(ctx))
	require.Eventually(t, func() bool {
		counts, err := clientA.PubSubNumSub(ctx, pubsubChannel()).Result()
		return err == nil && counts[pubsubChannel()] == 1
	}, 2*time.Second, 10*time.Millisecond)

	_, err := serviceA.UpdateBatch(ctx, values)
	require.NoError(t, err)
	for _, spec := range modelPolicySettings {
		assert.Equal(t, "new-"+string(spec.role), serviceA.GetString(ctx, spec.key, "", ""),
			"the publishing instance must not retain an old cache value")
	}
	require.Eventually(t, func() bool {
		for _, spec := range modelPolicySettings {
			if serviceB.GetString(ctx, spec.key, "", "") != "new-"+string(spec.role) {
				return false
			}
		}
		return true
	}, 2*time.Second, 10*time.Millisecond, "peer cache must reload every key published after the commit")
}

func TestSystemSettingUpdateBatchFailureLeavesCacheUnchanged(t *testing.T) {
	repo := &batchSettingRepo{rows: map[string]*types.SystemSetting{
		settingDefaultChat: stringSetting(settingDefaultChat, "old-chat"),
	}, batchErr: errors.New("transaction rolled back")}
	service := newBatchTestService(repo, nil, "instance-a")
	service.preload(context.Background())

	_, err := service.UpdateBatch(context.Background(), map[string]any{settingDefaultChat: "new-chat"})
	require.Error(t, err)
	assert.Equal(t, "old-chat", service.GetString(context.Background(), settingDefaultChat, "", ""))
	row, getErr := repo.Get(context.Background(), settingDefaultChat)
	require.NoError(t, getErr)
	value, valueErr := row.AsString()
	require.NoError(t, valueErr)
	assert.Equal(t, "old-chat", value)
}
