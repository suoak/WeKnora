package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestFeishuV2PostgresSourceRebindOperationWindow(t *testing.T) {
	for _, scenario := range []string{"completed", "unchanged_sync", "no_sync", "failed_sync", "held", "paused", "bad_receipt", "revoked_approval", "changed_epoch", "changed_audience", "rolled_back"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("FEISHU_SOURCE_REBIND_OPERATIONS_ENABLED", "1")
			v := newSourceConsentFixture(t)
			f := v.f
			v.consent(t, "renewed", v.old.OpenID, feishuScopes+" docs:permission.member:auth")
			require.NoError(t, f.db.Model(&types.User{}).Where("id = ?", "A").UpdateColumn("is_system_admin", true).Error)
			admin := context.WithValue(f.ctx, types.SystemAdminContextKey, true)
			proposal, err := f.s.PrepareSourceRebind(admin, []string{v.ds.ID}, "A")
			require.NoError(t, err)
			grantID, err := f.s.AuthorizeSourceRebindPublication(admin, proposal, "A", "explicit-recovery-publication", time.Now().Add(time.Hour))
			require.NoError(t, err)
			receiptID, err := f.s.ApplySourceRebind(admin, proposal, grantID, "A", "explicit-binding-migration", strings.Repeat("b", 64))
			require.NoError(t, err)
			var grant sourceRebindGrant
			require.NoError(t, readSourceSetting(f.db, sourceRebindPrefix+"grant."+grantID, &grant))
			var imported types.FeishuPersonalImport
			require.NoError(t, f.db.First(&imported, "id = ?", v.ds.ID).Error)
			resourceID, attemptID := uuid.NewString(), uuid.NewString()
			published := time.Now().UTC()
			if scenario == "unchanged_sync" {
				published = published.Add(-2 * time.Hour)
			}
			proof := types.FeishuLegacyReceipt{ResourceID: resourceID, AttemptID: attemptID, KnowledgeID: imported.KnowledgeID, Generation: 1,
				ManifestSHA256: strings.Repeat("a", 64), Chunks: 1, IndexedChunks: 1, Vectors: 1, BM25Rows: 1, Dimension: 1024, PublishedAt: published}
			if scenario == "bad_receipt" {
				proof.Vectors = 0
			}
			payload, err := json.Marshal(proof)
			require.NoError(t, err)
			require.NoError(t, f.db.Create(&types.FeishuLegacyResource{ID: resourceID, TenantID: 1, KnowledgeBaseID: "kb", DataSourceID: v.ds.ID,
				KnowledgeID: imported.KnowledgeID, ExternalID: "file:one", State: "active", ActiveAttempt: attemptID}).Error)
			require.NoError(t, f.db.Create(&types.FeishuLegacyAttempt{ID: attemptID, ResourceID: resourceID, Generation: 1, Status: "published", Receipt: payload}).Error)
			if scenario != "no_sync" {
				finished := time.Now().UTC()
				status := types.SyncLogStatusSuccess
				if scenario == "failed_sync" {
					status = types.SyncLogStatusFailed
				}
				require.NoError(t, f.db.Create(&types.SyncLog{ID: uuid.NewString(), DataSourceID: v.ds.ID, TenantID: 1, Status: status, StartedAt: finished,
					FinishedAt: &finished, ExecutionGeneration: 1, ExecutionClaimed: false}).Error)
			}
			switch scenario {
			case "held":
				require.NoError(t, f.db.Model(&types.DataSource{}).Where("id = ?", v.ds.ID).UpdateColumn("legacy_access_state", "hold").Error)
			case "paused":
				require.NoError(t, f.db.Model(&types.DataSource{}).Where("id = ?", v.ds.ID).UpdateColumn("status", types.DataSourceStatusPaused).Error)
			case "revoked_approval":
				require.NoError(t, f.db.Model(&types.FeishuSharingApproval{}).Where("id = ?", grant.Approvals[v.ds.ID]).UpdateColumn("state", "revoked").Error)
			case "changed_epoch":
				require.NoError(t, f.db.Model(&types.FeishuPersonalGrantEpoch{}).Where("user_id = ?", "A").UpdateColumn("epoch", "other-grant").Error)
			case "changed_audience":
				require.NoError(t, f.db.Exec("INSERT INTO kb_shares(id,knowledge_base_id,organization_id,source_tenant_id,permission) VALUES(?,?,?,?,?)", uuid.NewString(), "kb", "new-org", 1, "viewer").Error)
			case "rolled_back":
				var receipt sourceRebindReceipt
				require.NoError(t, readSourceSetting(f.db, sourceRebindPrefix+"receipt."+receiptID, &receipt))
				receipt.RolledBack = true
				cipher, e := sealFeishu(receipt)
				require.NoError(t, e)
				b, e := json.Marshal(cipher)
				require.NoError(t, e)
				require.NoError(t, f.db.Model(&types.SystemSetting{}).Where("key = ?", sourceRebindPrefix+"receipt."+receiptID).UpdateColumn("value", types.JSON(b)).Error)
			}
			var ds types.DataSource
			require.NoError(t, f.db.First(&ds, "id = ?", v.ds.ID).Error)
			config, err := ds.ParseConfig()
			require.NoError(t, err)
			ref, err := types.ParseFeishuSourceReference(config)
			require.NoError(t, err)
			err = checkSourceRebindAuthority(f.db, ds.ID, ref.ConnectionID, "", grant.ExpiresAt.Add(24*time.Hour))
			if scenario == "completed" || scenario == "unchanged_sync" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			var after types.DataSource
			require.NoError(t, f.db.First(&after, "id = ?", ds.ID).Error)
			require.Equal(t, string(ds.Config), string(after.Config))
			require.Equal(t, string(ds.LastSyncCursor), string(after.LastSyncCursor))
			require.Equal(t, ds.LegacyAccessState, after.LegacyAccessState)
		})
	}
}

func TestFeishuV2PostgresConsentFencesOnlyKnownOldExecutions(t *testing.T) {
	v := newSourceConsentFixture(t)
	f := v.f
	config, err := v.ds.ParseConfig()
	require.NoError(t, err)
	ref, err := types.ParseFeishuSourceReference(config)
	require.NoError(t, err)
	v.consent(t, "renewed", v.old.OpenID, feishuScopes+" docs:permission.member:auth")
	err = sourceConsentFenceError(f.db, &v.ds, ref, ErrFeishuReconnect)
	require.ErrorIs(t, err, errSourceConsentPending)
	require.False(t, legacyFetchNeedsHold(err), "a known same-account transition must not quarantine the source")
	require.NoError(t, f.db.Model(&types.FeishuPersonalGrantEpoch{}).Where("user_id = ?", "A").UpdateColumn("epoch", "unknown").Error)
	err = sourceConsentFenceError(f.db, &v.ds, ref, ErrFeishuReconnect)
	require.ErrorIs(t, err, ErrFeishuReconnect)
	require.True(t, legacyFetchNeedsHold(err), "unknown grant changes retain fail-closed behavior")
}
