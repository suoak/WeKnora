package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type sourceConsentFixture struct {
	f            *personalRecoveryFixture
	ds           types.DataSource
	old          types.FeishuCredentialRef
	config       *FeishuAppConfig
	deny         atomic.Bool
	onPermission func()
}

func newSourceConsentFixture(t *testing.T) *sourceConsentFixture {
	t.Helper()
	f := v2PostgresFixture(t)
	require.NoError(t, f.db.AutoMigrate(&types.DataSource{}, &types.SyncLog{}))
	require.NoError(t, f.db.Exec("CREATE TABLE kb_shares(id text PRIMARY KEY,knowledge_base_id "+
		"text,organization_id text,source_tenant_id bigint,permission "+
		"text,deleted_at timestamptz)").Error)
	_, _, old, err := f.s.ResolveCredentialReference(f.ctx, types.FeishuUserOAuth, "")
	require.NoError(t, err)
	cfg, err := f.s.app(f.ctx)
	require.NoError(t, err)
	v := &sourceConsentFixture{f: f, old: old, config: cfg}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/metas/batch_query") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"metas": []any{map[string]any{
					"doc_type": "file", "doc_token": "one",
					"title": "synthetic.pdf", "latest_modify_time": "2",
				}}},
			})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/members/auth") {
			if v.onPermission != nil {
				v.onPermission()
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"auth_result": !v.deny.Load()},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	f.s.newClient = func(c *core.Config) *core.Client { c.BaseURL = server.URL; return core.NewClient(c) }
	id, kid := uuid.NewString(), uuid.NewString()
	require.NoError(t, f.db.Create(&types.Knowledge{
		ID: kid, TenantID: 1, KnowledgeBaseID: "kb",
		ParseStatus: types.ParseStatusCompleted,
	}).Error)
	require.NoError(t, f.db.Create(&types.FeishuPersonalImport{
		ID: id, UserID: "A", AppID: old.AppID,
		OpenID: old.OpenID, KBID: "kb", AuthMode: types.FeishuUserOAuth,
		ResourceType: "file", DocumentToken: "one", KnowledgeID: kid, Status: "source_managed",
	}).Error)
	config, err := (&types.DataSourceConfig{Settings: map[string]interface{}{
		"credential_reference": types.FeishuSourceReference{
			Version: 1, Kind: types.FeishuUserOAuth, OwnerUserID: "A", AppID: old.AppID,
			IdentityVersion: old.ConfigVersion, ConnectionID: old.ConnectionID,
		},
		"links": []string{"https://test.feishu.cn/file/one"},
	}}).ToJSON()
	require.NoError(t, err)
	v.ds = types.DataSource{
		ID: id, TenantID: 1, KnowledgeBaseID: "kb", Type: "feishu",
		Status: types.DataSourceStatusActive, LegacyAccessState: "active", Config: config,
		DocumentDeferrals: types.JSON(`[]`), LastSyncCursor: types.JSON(`{"checkpoint":"preserved"}`),
	}
	require.NoError(t, f.db.Create(&v.ds).Error)
	require.NoError(t, f.db.First(&v.ds, "id = ?", id).Error)
	return v
}

func (v *sourceConsentFixture) consent(t *testing.T, epoch, openID, scope string) {
	t.Helper()
	f := v.f
	var next types.FeishuConnection
	require.NoError(t, f.db.Where("user_id = ?", "A").First(&next).Error)
	next.OpenID = openID
	cipher, err := sealFeishu(feishuTokens{UserID: "A", Access: "access-A", Refresh: "refresh-A", Scope: scope})
	require.NoError(t, err)
	next.Tokens = cipher
	require.NoError(t, f.db.Transaction(func(tx *gorm.DB) error {
		if err := f.s.recordSourceConsent(tx, &next, v.config, epoch); err != nil {
			return err
		}
		if err := tx.Model(&types.FeishuConnection{}).Where("user_id = ?",
			"A").Updates(map[string]any{"open_id": openID, "tokens": cipher}).Error; err != nil {
			return err
		}
		return tx.Model(&types.FeishuPersonalGrantEpoch{}).Where("user_id = ?", "A").Update("epoch", epoch).Error
	}))
}

func (v *sourceConsentFixture) execution(t *testing.T) context.Context {
	t.Helper()
	log := types.SyncLog{
		ID: uuid.NewString(), DataSourceID: v.ds.ID, TenantID: 1,
		Status: types.SyncLogStatusRunning, StartedAt: time.Now(), ExecutionClaimed: true, ExecutionGeneration: 1,
	}
	require.NoError(t, v.f.db.Create(&log).Error)
	return types.WithSyncExecution(v.f.ctx, types.SyncExecution{
		DataSourceID: v.ds.ID, SyncLogID: log.ID,
		TenantID: 1, Generation: 1,
	})
}

func TestFeishuV2PostgresAutomaticSourceConsent(t *testing.T) {
	for _, scenario := range []string{
		"success", "non_creator_writer", "coalesced", "worker_restart", "deferred_retry", "held", "revoked", "paused",
		"protected", "changed_account", "missing_scope", "permission_denied", "lost_write",
		"unsupported_connector",
		"pause_during_probe", "protection_during_probe", "lease_lost_during_probe", "consent_during_probe",
		"concurrent_workers",
	} {
		t.Run(scenario, func(t *testing.T) {
			v := newSourceConsentFixture(t)
			f := v.f
			before := v.ds
			openID, scope := v.old.OpenID, feishuScopes+" docs:permission.member:auth"
			switch scenario {
			case "held", "revoked":
				require.NoError(t, f.db.Model(&types.DataSource{}).Where("id = ?",
					v.ds.ID).UpdateColumn("legacy_access_state", map[string]string{
					"held":    "hold",
					"revoked": "revoked",
				}[scenario]).Error)
			case "paused":
				require.NoError(t, f.db.Model(&types.DataSource{}).Where("id = ?",
					v.ds.ID).UpdateColumn("status", types.DataSourceStatusPaused).Error)
			case "protected":
				require.NoError(t, f.db.Model(&types.DataSource{}).Where("id = ?",
					v.ds.ID).UpdateColumn("document_deferrals", types.JSON(`[{"state":"hold"}]`)).Error)
			case "changed_account":
				openID = "different-account"
			case "missing_scope":
				scope = "drive:drive.metadata:readonly docs:permission.member:auth"
			case "unsupported_connector":
				require.NoError(t, f.db.Model(&types.DataSource{}).Where("id = ?", v.ds.ID).
					UpdateColumn("type", types.ConnectorTypeNotion).Error)
			}
			v.consent(t, "supplemented", openID, scope)
			if scenario == "coalesced" {
				v.consent(t, "supplemented-again", openID, scope)
			}
			if scenario == "non_creator_writer" {
				require.NoError(t, f.db.Model(&types.TenantMember{}).Where("user_id = ?", "A").
					UpdateColumn("role", types.TenantRoleContributor).Error)
				require.NoError(t, f.db.Model(&types.KnowledgeBase{}).Where("id = ?", "kb").
					UpdateColumn("creator_id", "B").Error)
			}
			if scenario == "deferred_retry" {
				require.NoError(t, f.db.Model(&types.DataSource{}).Where("id = ?", v.ds.ID).
					UpdateColumn("status", types.DataSourceStatusError).Error)
				require.NoError(t, f.db.First(&v.ds, "id = ?", v.ds.ID).Error)
			}
			if scenario == "permission_denied" {
				v.deny.Store(true)
			}
			if scenario == "lost_write" {
				require.NoError(t, f.db.Model(&types.TenantMember{}).Where("user_id = ?",
					"A").UpdateColumn("role", types.TenantRoleViewer).Error)
			}
			var once sync.Once
			if scenario == "pause_during_probe" {
				v.onPermission = func() {
					once.Do(func() {
						require.NoError(t, f.db.Model(&types.DataSource{}).Where("id = ?",
							v.ds.ID).UpdateColumn("status", types.DataSourceStatusPaused).Error)
					})
				}
			}
			if scenario == "consent_during_probe" {
				v.onPermission = func() { once.Do(func() { v.consent(t, "concurrent-consent", openID, scope) }) }
			}
			ctx := v.execution(t)
			if scenario == "protection_during_probe" {
				v.onPermission = func() {
					once.Do(func() {
						require.NoError(t, f.db.Model(&types.DataSource{}).Where("id = ?", v.ds.ID).
							UpdateColumn("document_deferrals", types.JSON(`[{"state":"hold"}]`)).Error)
					})
				}
			}
			if scenario == "lease_lost_during_probe" {
				execution, _ := types.SyncExecutionFromContext(ctx)
				v.onPermission = func() {
					once.Do(func() {
						require.NoError(t, f.db.Model(&types.SyncLog{}).Where("id = ?", execution.SyncLogID).
							UpdateColumn("execution_generation", 2).Error)
					})
				}
			}
			if scenario == "worker_restart" {
				cold := NewFeishuPersonalService(f.db, repository.NewSystemSettingRepository(f.db), nil, nil, nil)
				cold.newClient = f.s.newClient
				f.s = cold
			}
			var err error
			if scenario == "concurrent_workers" {
				var winners atomic.Int32
				var wg sync.WaitGroup
				for range 2 {
					wg.Add(1)
					go func() {
						defer wg.Done()
						ds := before
						if f.s.renewSourceConsent(ctx, &ds) == nil {
							winners.Add(1)
						}
					}()
				}
				wg.Wait()
				require.EqualValues(t, 1, winners.Load())
			} else {
				err = f.s.renewSourceConsent(ctx, &v.ds)
			}
			var after types.DataSource
			require.NoError(t, f.db.First(&after, "id = ?", v.ds.ID).Error)
			require.Equal(t, string(before.LastSyncCursor), string(after.LastSyncCursor))
			var imported types.FeishuPersonalImport
			require.NoError(t, f.db.First(&imported, "id = ?", v.ds.ID).Error)
			if scenario == "success" || scenario == "non_creator_writer" || scenario == "coalesced" ||
				scenario == "worker_restart" ||
				scenario == "deferred_retry" || scenario == "concurrent_workers" {
				require.NoError(t, err)
				require.NotEqual(t, string(before.Config), string(after.Config))
				require.Equal(t, "active", after.LegacyAccessState)
				config, e := after.ParseConfig()
				require.NoError(t, e)
				ref, e := types.ParseFeishuSourceReference(config)
				require.NoError(t, e)
				require.NoError(t, validatePersonalSourceBinding(f.db, ref))
				require.NoError(t, checkSourceRebindAuthority(f.db, after.ID, ref.ConnectionID, "",
					time.Now().Add(7*24*time.Hour)), "a completed same-account renewal has no daily approval lifetime")
				var auditCount int64
				require.NoError(t, f.db.Model(&types.SystemSetting{}).Where("key LIKE ?",
					sourceRebindPrefix+"auto.%").Count(&auditCount).Error)
				require.EqualValues(t, 1, auditCount)
				var current types.FeishuPersonalGrantEpoch
				require.NoError(t, f.db.Where("user_id = ?", "A").First(&current).Error)
				var consent sourceConsentTransition
				require.NoError(t, readSourceSetting(f.db, sourceConsentPrefix+current.Epoch, &consent))
				require.JSONEq(t, string(before.Config), string(consent.Items[before.ID].OriginalConfig))
			} else {
				require.Error(t, err)
				require.JSONEq(t, string(before.Config), string(after.Config))
				if scenario == "pause_during_probe" {
					require.Equal(t, types.DataSourceStatusPaused, after.Status)
				}
				if scenario == "held" {
					require.Equal(t, "hold", after.LegacyAccessState)
				}
				if scenario == "revoked" {
					require.Equal(t, "revoked", after.LegacyAccessState)
				}
			}
			var count int64
			require.NoError(t, f.db.Model(&types.Knowledge{}).Where("id = ?", imported.KnowledgeID).Count(&count).Error)
			require.EqualValues(t, 1, count)
		})
	}
}
