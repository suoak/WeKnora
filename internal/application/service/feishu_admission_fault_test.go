package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestFeishuAdmissionFaultRetainsCheckpoint(t *testing.T) {
	for name, failure := range map[string]error{
		"database":                     errors.New("database write failed"),
		"file_storage":                 errors.New("file store unavailable"),
		"quota":                        types.NewStorageQuotaExceededError(),
		"bad_request_is_not_permanent": errors.New("bad request"),
	} {
		t.Run(name, func(t *testing.T) {
			ks := &sweepFakeKS{repo: &sweepFakeRepo{}, createErr: failure}
			ds := &types.DataSource{
				ID:              "source",
				TenantID:        1,
				KnowledgeBaseID: "kb",
				Type:            "feishu",
				LastSyncCursor:  types.JSON(`{"accepted":"v1"}`),
			}
			h := newStreamHandler(
				&DataSourceService{knowledgeService: ks},
				ds,
				&types.SyncResult{},
				&types.SyncLog{},
			)
			item := types.FetchedItem{ExternalID: "doc", Content: []byte("body"), FileName: "doc.md"}
			err := h.Emit(context.Background(), item)
			require.ErrorIs(t, err, failure)
			require.Error(t, h.Checkpoint(context.Background(), &types.SyncCursor{}))
			require.JSONEq(t, `{"accepted":"v1"}`, string(ds.LastSyncCursor))
			ks.createErr = nil
			require.Error(t, h.Emit(context.Background(), item), "ignored Emit error must remain latched")
			require.Len(t, ks.events, 1, "latched worker must not write more knowledge")
			require.Empty(t, ks.deleted)
		})
	}
}

func TestFeishuAdmissionLookupFaultNeverCreates(t *testing.T) {
	ks := &sweepFakeKS{repo: &deletionLookupKnowledgeRepo{lookupErr: errors.New("read unavailable")}}
	s := &DataSourceService{knowledgeService: ks}
	_, err := s.ingestItem(
		context.Background(),
		&types.DataSource{ID: "source", Type: "feishu", TenantID: 1, KnowledgeBaseID: "kb"},
		&types.FetchedItem{ExternalID: "doc", Content: []byte("body"), FileName: "doc.md"},
		nil,
	)
	require.Error(t, err)
	require.Empty(t, ks.events)
	require.Empty(t, ks.deleted)
}

func TestFeishuAdmissionUnsupportedDoesNotPoisonNextItem(t *testing.T) {
	ks := &sweepFakeKS{repo: &sweepFakeRepo{}, createErr: ErrInvalidFileType}
	h := newStreamHandler(
		&DataSourceService{knowledgeService: ks},
		&types.DataSource{ID: "source", Type: "feishu"},
		&types.SyncResult{},
		&types.SyncLog{},
	)
	item := types.FetchedItem{ExternalID: "doc", Content: []byte("body"), FileName: "doc.md"}
	require.NoError(t, h.Emit(context.Background(), item))
	require.Equal(t, "unsupported_file_type", h.result.Errors[0].Code)
	ks.createErr = nil
	require.NoError(t, h.Emit(context.Background(), item))
	require.Equal(t, 1, h.result.Created)
}

type failingFeishuCheckpoint struct{ *processSyncSyncLogRepo }

func (r *failingFeishuCheckpoint) WriteExecution(
	context.Context,
	types.SyncExecution,
	*types.DataSource,
	*types.SyncLog,
	bool,
) (bool, error) {
	return false, errors.New("checkpoint commit unknown")
}

func TestFeishuCheckpointFailureRestoresInMemoryCursor(t *testing.T) {
	log := &types.SyncLog{
		ID:                  "run",
		Status:              types.SyncLogStatusRunning,
		ExecutionClaimed:    true,
		ExecutionGeneration: 1,
	}
	repo := &failingFeishuCheckpoint{&processSyncSyncLogRepo{logs: map[string]*types.SyncLog{"run": log}}}
	ds := &types.DataSource{ID: "source", Type: "feishu", LastSyncCursor: types.JSON(`{"accepted":"v1"}`)}
	h := newStreamHandler(&DataSourceService{syncLogRepo: repo}, ds, &types.SyncResult{}, log)
	ctx := types.WithSyncExecution(
		context.Background(),
		types.SyncExecution{DataSourceID: "source", SyncLogID: "run", Generation: 1},
	)
	require.Error(t, h.Checkpoint(ctx, &types.SyncCursor{}))
	require.JSONEq(t, `{"accepted":"v1"}`, string(ds.LastSyncCursor))
	require.Error(
		t,
		h.Checkpoint(ctx, &types.SyncCursor{}),
		"a later checkpoint cannot bypass the failed write",
	)
}
