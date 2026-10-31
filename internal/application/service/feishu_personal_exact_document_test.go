package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPersonalRecoveryExactSourceAttestation(t *testing.T) {
	for _, change := range []string{
		"valid", "empty_registry_hash", "nonce", "generation", "grant", "grant_during_remote",
		"expired_lease", "revision", "acl", "binding", "legacy_overlap",
	} {
		t.Run(change, func(t *testing.T) {
			f := newMVPFixture(t)
			m := f.prepare("https://tenant.feishu.cn/file/real")
			require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
			k, _, _ := mvpPublicTask(t, f, m)
			require.NoError(
				t,
				f.db.AutoMigrate(
					&types.StoredResource{},
					&types.ResourceBinding{},
					&types.DataSource{},
				),
			)
			require.NoError(t, f.db.Exec(`CREATE TABLE feishu_legacy_resources (
id text PRIMARY KEY,tenant_id bigint,knowledge_base_id text,knowledge_id text,state text,
original_file_path text,external_id text)`).Error)
			require.NoError(
				t,
				f.db.Exec(
					`CREATE TABLE feishu_legacy_attempts (id text,resource_id text,manifest jsonb)`,
				).Error,
			)
			// An unrelated held source remains present in exactly the same KB.
			require.NoError(t, f.db.Create(&types.DataSource{
				ID: "held-sibling", TenantID: 1,
				KnowledgeBaseID: "kb", Type: "feishu_drive", Status: "error",
				LegacyAccessState: "hold", Config: types.JSON(`{}`),
			}).Error)
			manifest, err := f.p.store.read(mustPersonalAttempt(t, f, m.AttemptID))
			require.NoError(t, err)
			resource := types.StoredResource{
				ID:           "exact-file",
				Handle:       "abcdefghijklmnopqrstuv",
				TenantID:     1,
				Provider:     "local",
				PhysicalPath: k.FilePath,
				LocationHash: "fixture-location",
				Size:         k.FileSize,
				ContentHash:  manifest.Hash,
				State:        "active",
				Lifecycle:    "persistent",
			}
			if change == "empty_registry_hash" {
				resource.ContentHash = ""
			}
			require.NoError(t, f.db.Create(&resource).Error)
			require.NoError(
				t,
				f.db.Create(&types.ResourceBinding{
					ID: "exact-binding", ResourceID: resource.ID,
					TenantID: 1, OwnerType: "knowledge", OwnerID: k.ID, Relation: "source_file",
				}).Error,
			)
			require.NoError(
				t,
				f.db.Exec(
					"UPDATE knowledges SET file_path=? WHERE id=?",
					types.BuildResourcePath(resource.Handle),
					k.ID,
				).Error,
			)
			types.EnableLegacyReadGuards(f.db)
			owner, err := f.p.repo.Claim(context.Background(), m.AttemptID, f.p.lease)
			require.NoError(t, err)
			server := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("Authorization") != "Bearer access-A" {
						w.WriteHeader(http.StatusForbidden)
						return
					}
					switch r.URL.Path {
					case "/open-apis/drive/v1/metas/batch_query":
						revision := "1"
						if change == "revision" {
							revision = "2"
						}
						_, _ = fmt.Fprintf(
							w,
							`{"code":0,"data":{"metas":[{"doc_type":"file","doc_token":"real",`+
								`"latest_modify_time":%q}]}}`,
							revision,
						)
					case "/open-apis/drive/v1/permissions/real/members/auth":
						if change == "grant_during_remote" &&
							r.URL.Query().Get("action") == "copy" {
							err := f.db.Exec(
								"UPDATE feishu_personal_grant_epochs SET epoch='revoked' WHERE user_id='A'",
							).Error
							if err != nil {
								w.WriteHeader(500)
								return
							}
						}
						_ = json.NewEncoder(w).
							Encode(map[string]any{"code": 0, "data": map[string]bool{"auth_result": change != "acl"}})
					default:
						w.WriteHeader(http.StatusNotFound)
					}
				}),
			)
			defer server.Close()
			f.s.newClient = func(cfg *core.Config) *core.Client { cfg.BaseURL = server.URL; return core.NewClient(cfg) }
			switch change {
			case "expired_lease":
				require.NoError(
					t,
					f.db.Exec(
						"UPDATE feishu_personal_attempts SET lease_until=now()-interval '1 second' WHERE id=?",
						owner.ID,
					).Error,
				)
			case "nonce":
				owner.OwnerNonce = "forged"
			case "generation":
				require.NoError(
					t,
					f.db.Exec(
						"UPDATE feishu_personal_resources SET generation=generation+1 WHERE import_id=?",
						m.ID,
					).Error,
				)
			case "grant":
				require.NoError(
					t,
					f.db.Exec(
						"UPDATE feishu_personal_grant_epochs SET epoch='revoked' WHERE user_id='A'",
					).Error,
				)
			case "binding":
				require.NoError(
					t,
					f.db.Create(&types.ResourceBinding{
						ResourceID: resource.ID, TenantID: 1,
						OwnerType: "knowledge", OwnerID: "other", Relation: "source_file",
					}).Error,
				)
			case "legacy_overlap":
				require.NoError(
					t,
					f.db.Exec(
						"INSERT INTO feishu_legacy_resources VALUES ('legacy',1,'kb',?,'revoked','','file:real')",
						k.ID,
					).Error,
				)
			}
			proof, err := f.p.prepareExactPersonalDocument(context.Background(), owner)
			if change == "valid" || change == "empty_registry_hash" {
				require.NoError(t, err)
				require.Equal(t, k.ID, proof.knowledge)
				require.Equal(t, owner.Fence, proof.generation)
				require.Equal(t, manifest.Hash, proof.fileSHA256)
				require.Len(t, proof.file, int(k.FileSize))
			} else {
				require.Error(t, err)
				require.Nil(t, proof)
			}
			// Source attestation alone does not authorize content or a whole KB.
			var body types.Knowledge
			require.ErrorIs(
				t,
				f.db.WithContext(context.Background()).Where("id=?", k.ID).First(&body).Error,
				types.ErrLegacySourceRestricted,
			)
		})
	}
}

func mustPersonalAttempt(
	t *testing.T,
	f *personalRecoveryFixture,
	id string,
) *types.FeishuPersonalAttempt {
	t.Helper()
	_, _, a, err := f.p.repo.Load(context.Background(), id)
	require.NoError(t, err)
	return a
}
