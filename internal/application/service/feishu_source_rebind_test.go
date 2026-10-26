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

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestFeishuV2PostgresControlledSourceRebind(t *testing.T) {
	for _, scenario := range []string{
		"atomic_success_and_rollback", "second_source_conflict", "revoked_new_approval",
		"changed_epoch", "changed_index", "changed_account", "permission_denied",
		"concurrent_one_winner", "rollback_after_index_change", "revoked_operator",
	} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("FEISHU_SOURCE_REBIND_OPERATIONS_ENABLED", "1")
			f := v2PostgresFixture(t)
			require.NoError(
				t,
				f.db.Model(&types.User{}).
					Where("id = ?", "A").
					UpdateColumn("is_system_admin", true).
					Error,
			)
			require.NoError(t, f.db.AutoMigrate(&types.DataSource{}, &types.SyncLog{}))
			require.NoError(
				t,
				f.db.Exec(
					"CREATE TABLE kb_shares(id text PRIMARY KEY,knowledge_base_id text,organization_id text,"+
						"source_tenant_id bigint,permission text,deleted_at timestamptz)",
				).Error,
			)
			_, _, old, err := f.s.ResolveCredentialReference(f.ctx, types.FeishuUserOAuth, "")
			require.NoError(t, err)
			var deny atomic.Bool
			server := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					require.Equal(t, "Bearer access-A", r.Header.Get("Authorization"))
					if strings.HasSuffix(r.URL.Path, "/metas/batch_query") {
						var request struct {
							Docs []map[string]string `json:"request_docs"`
						}
						require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
						_ = json.NewEncoder(w).
							Encode(map[string]any{"code": 0, "data": map[string]any{"metas": []any{map[string]any{
								"doc_type": "file", "doc_token": request.Docs[0]["doc_token"],
								"title": "synthetic.pdf", "latest_modify_time": "2",
							}}}})
						return
					}
					if strings.HasSuffix(r.URL.Path, "/members/auth") {
						_ = json.NewEncoder(w).
							Encode(map[string]any{"code": 0, "data": map[string]any{"auth_result": !deny.Load()}})
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}),
			)
			defer server.Close()
			f.s.newClient = func(cfg *core.Config) *core.Client { cfg.BaseURL = server.URL; return core.NewClient(cfg) }
			ids := []string{uuid.NewString(), uuid.NewString()}
			original := map[string]types.DataSource{}
			for i, id := range ids {
				token := []string{"one", "two"}[i]
				kid := uuid.NewString()
				require.NoError(
					t,
					f.db.Create(
						&types.Knowledge{
							ID:              kid,
							TenantID:        1,
							KnowledgeBaseID: "kb",
							ParseStatus:     types.ParseStatusCompleted,
							Metadata:        types.JSON(`{"external_id":"file:` + token + `"}`),
						},
					).Error,
				)
				require.NoError(
					t,
					f.db.Create(
						&types.FeishuPersonalImport{
							ID:             id,
							UserID:         "A",
							AppID:          old.AppID,
							OpenID:         old.OpenID,
							KBID:           "kb",
							AuthMode:       types.FeishuUserOAuth,
							DocumentToken:  token,
							ResourceType:   "file",
							KnowledgeID:    kid,
							SourceModified: "1",
							Status:         "source_managed",
						},
					).Error,
				)
				config, e := (&types.DataSourceConfig{Settings: map[string]interface{}{
					"credential_reference": types.FeishuSourceReference{
						Version: 1, Kind: old.Kind, OwnerUserID: "A", AppID: old.AppID,
						IdentityVersion: old.ConfigVersion, ConnectionID: old.ConnectionID,
					},
					"links": []string{"https://test.feishu.cn/file/" + token},
				}}).ToJSON()
				require.NoError(t, e)
				ds := types.DataSource{
					ID:                id,
					TenantID:          1,
					KnowledgeBaseID:   "kb",
					Type:              "feishu",
					Status:            types.DataSourceStatusError,
					LegacyAccessState: "hold",
					DocumentDeferrals: types.JSON(`[{"resource":"protected", "state":"hold"}]`),
					Config:            config,
					LastSyncCursor:    types.JSON(`{"accepted":"old"}`),
				}
				require.NoError(t, f.db.Create(&ds).Error)
				require.NoError(t, f.db.First(&ds, "id = ?", id).Error)
				original[id] = ds
			}
			require.NoError(
				t,
				f.db.Model(&types.FeishuPersonalGrantEpoch{}).
					Where("user_id = ?", "A").
					Update("epoch", "supplemented").
					Error,
			)
			cipher, err := sealFeishu(
				feishuTokens{
					UserID:  "A",
					Access:  "access-A",
					Refresh: "refresh-A",
					Scope:   feishuScopes + " docs:permission.member:auth",
				},
			)
			require.NoError(t, err)
			require.NoError(
				t,
				f.db.Model(&types.FeishuConnection{}).
					Where("user_id = ?", "A").
					Update("tokens", cipher).
					Error,
			)
			admin := context.WithValue(f.ctx, types.SystemAdminContextKey, true)
			_, err = f.s.PrepareSourceRebind(admin, ids, "not-the-session-actor")
			require.Error(t, err)
			count, err := f.s.SourceBindingAttention(f.ctx)
			require.NoError(t, err)
			require.Equal(t, 2, count)
			_, err = f.s.PrepareSourceRebind(f.ctx, ids, "A")
			require.Error(t, err)
			if scenario == "changed_account" {
				require.NoError(
					t,
					f.db.Model(&types.FeishuConnection{}).
						Where("user_id = ?", "A").
						Update("open_id", "another-account").
						Error,
				)
				_, err = f.s.PrepareSourceRebind(admin, ids, "A")
				require.Error(t, err)
				return
			}
			if scenario == "permission_denied" {
				deny.Store(true)
				_, err = f.s.PrepareSourceRebind(admin, ids, "A")
				require.Error(t, err)
				return
			}
			proposal, err := f.s.PrepareSourceRebind(admin, ids, "A")
			require.NoError(t, err)
			plan, err := readRebindPlan(proposal)
			require.NoError(t, err)
			require.Equal(t, old.AppID, plan.Items[0].Import.AppID)
			require.Equal(t, old.OpenID, plan.Items[0].Import.OpenID)
			grant, err := f.s.AuthorizeSourceRebindPublication(
				admin,
				proposal,
				"A",
				"NEW-publication-approval",
				time.Now().Add(time.Hour),
			)
			require.NoError(t, err)
			switch scenario {
			case "revoked_operator":
				require.NoError(
					t,
					f.db.Model(&types.User{}).
						Where("id = ?", "A").
						UpdateColumn("is_system_admin", false).
						Error,
				)
			case "second_source_conflict":
				require.NoError(
					t,
					f.db.Model(&types.DataSource{}).
						Where("id = ?", ids[1]).
						Update("status", types.DataSourceStatusPaused).
						Error,
				)
			case "changed_epoch":
				require.NoError(
					t,
					f.db.Model(&types.FeishuPersonalGrantEpoch{}).
						Where("user_id = ?", "A").
						Update("epoch", "newer").
						Error,
				)
			case "revoked_new_approval":
				require.NoError(
					t,
					f.db.Model(&types.FeishuSharingApproval{}).
						Where("reason = ?", "NEW-publication-approval").
						Update("state", "revoked").
						Error,
				)
			case "changed_index":
				require.NoError(
					t,
					f.db.Model(&types.Knowledge{}).
						Where("id = (?)", f.db.Model(&types.FeishuPersonalImport{}).
							Select("knowledge_id").
							Where("id = ?", ids[1])).
						Update("title", "changed after proposal").
						Error,
				)
			}
			apply := func() (string, error) {
				return f.s.ApplySourceRebind(
					admin,
					proposal,
					grant,
					"A",
					"NEW-migration-approval",
					strings.Repeat("a", 64),
				)
			}
			var receipt string
			if scenario == "concurrent_one_winner" {
				var wg sync.WaitGroup
				var winners atomic.Int32
				for range 4 {
					wg.Add(1)
					go func() {
						defer wg.Done()
						if _, e := apply(); e == nil {
							winners.Add(1)
						}
					}()
				}
				wg.Wait()
				require.EqualValues(t, 1, winners.Load())
			} else {
				receipt, err = apply()
			}
			if scenario == "atomic_success_and_rollback" ||
				scenario == "rollback_after_index_change" ||
				scenario == "concurrent_one_winner" {
				require.NoError(t, err)
				for _, id := range ids {
					var ds types.DataSource
					require.NoError(t, f.db.First(&ds, "id = ?", id).Error)
					require.Equal(t, "hold", ds.LegacyAccessState)
					require.Equal(t, string(original[id].LastSyncCursor), string(ds.LastSyncCursor))
					require.Equal(t, original[id].Status, ds.Status)
				}
				count, err = f.s.SourceBindingAttention(f.ctx)
				require.NoError(t, err)
				require.Zero(t, count)
				if scenario == "atomic_success_and_rollback" {
					require.NoError(
						t,
						f.s.RollbackSourceRebind(
							admin,
							receipt,
							"A",
							"NEW-rollback-approval",
						),
					)
				}
				if scenario == "rollback_after_index_change" {
					require.NoError(
						t,
						f.db.Model(&types.Knowledge{}).
							Where("id = (?)", f.db.Model(&types.FeishuPersonalImport{}).
								Select("knowledge_id").
								Where("id = ?", ids[0])).
							Update("title", "published change").
							Error,
					)
					require.Error(
						t,
						f.s.RollbackSourceRebind(
							admin,
							receipt,
							"A",
							"NEW-rollback-approval",
						),
					)
				}
			} else {
				require.Error(t, err)
			}
			if scenario != "concurrent_one_winner" && scenario != "rollback_after_index_change" {
				for _, id := range ids {
					var ds types.DataSource
					require.NoError(t, f.db.First(&ds, "id = ?", id).Error)
					require.JSONEq(t, string(original[id].Config), string(ds.Config))
					require.Equal(t, "hold", ds.LegacyAccessState)
					require.Equal(t, string(original[id].LastSyncCursor), string(ds.LastSyncCursor))
				}
			}
			var runs int64
			require.NoError(t, f.db.Model(&types.SyncLog{}).Count(&runs).Error)
			require.Zero(t, runs, "binding migration never creates or resumes an execution")
		})
	}
}
