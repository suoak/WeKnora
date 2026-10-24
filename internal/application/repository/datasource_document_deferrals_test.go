package repository

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDocumentDeferralRepositorySQLite(t *testing.T) { documentDeferralRepositorySuite(t, "sqlite") }
func TestDocumentDeferralRepositoryPostgres(t *testing.T) {
	documentDeferralRepositorySuite(t, "postgres")
}
func documentDeferralRepositorySuite(t *testing.T, dialect string) {
	db := syncdb.Open(t, dialect)
	repo := &DataSourceRepository{db: db}
	ctx := context.Background()
	makeSource := func() *types.DataSource {
		s := &types.DataSource{ID: uuid.NewString(),
			TenantID:          1,
			KnowledgeBaseID:   "kb",
			Type:              types.ConnectorTypeFeishu,
			Status:            types.DataSourceStatusActive,
			LegacyAccessState: "unmanaged",
			Config:            types.JSON(`{"resource_ids":["root"],"credentials":{"app_secret":"synthetic-kept"}}`),
			LastSyncCursor:    types.JSON(`{"accepted":"kept"}`),
			SyncSchedule:      "0 0 * * * *"}
		require.NoError(t, db.Create(s).Error)
		return s
	}
	entry := func(s *types.DataSource) types.SyncDocumentDeferral {
		return types.SyncDocumentDeferral{TenantID: s.TenantID,
			DataSourceID:     s.ID,
			KnowledgeBaseID:  s.KnowledgeBaseID,
			SourceResourceID: "root",
			ExternalID:       "one",
			ReasonCode:       types.DocumentDeferralAwaitingAuthorization,
			Reason:           "owner authorization pending",
			RequestedBy:      "synthetic-author",
			RequestedAt:      time.Now().UTC(),
			Reference:        "synthetic-request"}
	}
	t.Run("policy_only_preserves_scope_and_checkpoint", func(t *testing.T) {
		s := makeSource()
		before, err := repo.FindByID(ctx, s.ID)
		require.NoError(t, err)
		config, cursor := string(before.Config), string(before.LastSyncCursor)
		require.NoError(t,
			repo.ReplaceDocumentDeferrals(ctx,
				s,
				types.DocumentDeferralHash(nil),
				[]types.SyncDocumentDeferral{entry(s)}))
		stored, err := repo.FindByID(ctx, s.ID)
		require.NoError(t, err)
		require.Equal(t, config, string(stored.Config))
		require.Equal(t, cursor, string(stored.LastSyncCursor))
		require.Equal(t, "unmanaged", stored.LegacyAccessState)
		policy, err := stored.ParseDocumentDeferrals()
		require.NoError(t, err)
		require.Len(t, policy, 1)
		require.Error(t, repo.ReplaceDocumentDeferrals(ctx, s, types.DocumentDeferralHash(nil), nil))
		require.NoError(t, repo.ReplaceDocumentDeferrals(ctx, s, types.DocumentDeferralHash(policy), nil))
	})
	t.Run("one_policy_writer_wins", func(t *testing.T) {
		s := makeSource()
		var winners atomic.Int64
		var wg sync.WaitGroup
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				copy := *s
				if repo.ReplaceDocumentDeferrals(ctx,
					&copy,
					types.DocumentDeferralHash(nil),
					[]types.SyncDocumentDeferral{entry(s)}) == nil {
					winners.Add(1)
				}
			}()
		}
		wg.Wait()
		require.Equal(t, int64(1), winners.Load())
	})
	t.Run("pending_cursor_survives_jsonb", func(t *testing.T) {
		s := makeSource()
		e := entry(s)
		cursor := map[string]any{"accepted": "kept",
			"connector_cursor": map[string]any{"feishu_pending_documents_v1": map[string]types.SyncDocumentDeferral{e.Key(): e}}}
		blob, err := json.Marshal(cursor)
		require.NoError(t, err)
		s.LastSyncCursor = blob
		require.NoError(t, repo.UpdateSyncState(ctx, s))
		stored, err := repo.FindByID(ctx, s.ID)
		require.NoError(t, err)
		require.JSONEq(t, string(blob), string(stored.LastSyncCursor))
	})
	for _, state := range []string{"paused", "hold", "revoked", "running", "wrong_scope"} {
		t.Run(state, func(t *testing.T) {
			s := makeSource()
			e := entry(s)
			switch state {
			case "paused":
				require.NoError(t, db.Model(s).Update("status", types.DataSourceStatusPaused).Error)
			case "hold", "revoked":
				require.NoError(t, db.Model(s).Update("legacy_access_state", state).Error)
			case "running":
				require.NoError(t,
					db.Create(&types.SyncLog{DataSourceID: s.ID,
						TenantID: 1,
						Status:   types.SyncLogStatusRunning}).Error)
			case "wrong_scope":
				e.TenantID = 2
			}
			require.Error(t,
				repo.ReplaceDocumentDeferrals(ctx,
					s,
					types.DocumentDeferralHash(nil),
					[]types.SyncDocumentDeferral{e}))
			stored, err := repo.FindByID(ctx, s.ID)
			require.NoError(t, err)
			p, err := stored.ParseDocumentDeferrals()
			require.NoError(t, err)
			require.Empty(t, p)
		})
	}
	t.Run("generic_update_cannot_clear_policy", func(t *testing.T) {
		s := makeSource()
		require.NoError(t,
			repo.ReplaceDocumentDeferrals(ctx,
				s,
				types.DocumentDeferralHash(nil),
				[]types.SyncDocumentDeferral{entry(s)}))
		s.DocumentDeferrals = types.JSON(`[]`)
		require.NoError(t, repo.Update(ctx, s))
		stored, err := repo.FindByID(ctx, s.ID)
		require.NoError(t, err)
		p, err := stored.ParseDocumentDeferrals()
		require.NoError(t, err)
		require.Len(t, p, 1)
		b, _ := json.Marshal(p)
		require.JSONEq(t, string(b), string(stored.DocumentDeferrals))
	})
}
