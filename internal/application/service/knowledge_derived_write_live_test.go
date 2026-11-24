//go:build feishu_live_acceptance

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/models/api"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/database"
	"github.com/Tencent/WeKnora/internal/testutil/feishulegacy"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

type derivedAcceptanceSnapshot struct {
	Tenant                       uint64 `json:"tenant"`
	Source, Owner, KB, Knowledge string
	Tables                       map[string]json.RawMessage
	Env                          map[string]string
}

type derivedAcceptanceKB struct {
	interfaces.KnowledgeBaseService
	repo interfaces.KnowledgeBaseRepository
}

func (s derivedAcceptanceKB) GetKnowledgeBaseByID(ctx context.Context, id string) (*types.KnowledgeBase, error) {
	return s.repo.GetKnowledgeBaseByID(ctx, id)
}

type derivedAcceptanceChunks struct {
	interfaces.ChunkService
	repo interfaces.ChunkRepository
}

func (s derivedAcceptanceChunks) ListChunksByKnowledgeID(ctx context.Context, id string) ([]*types.Chunk, error) {
	return s.repo.ListChunksByKnowledgeID(ctx, types.MustTenantIDFromContext(ctx), id)
}

// Copied refresh tokens must never rotate the original test connection.
// All other calls retain the constructor's normal SSRF-safe transport.
type derivedAcceptanceNoRefresh struct{ base http.RoundTripper }

func (t derivedAcceptanceNoRefresh) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.Contains(req.URL.Path, "/oauth/token") || strings.Contains(req.URL.Path, "refresh_access_token") {
		return nil, errors.New("refresh prohibited in isolated acceptance clone")
	}
	return t.base.RoundTrip(req)
}

func derivedAcceptanceClone(
	t *testing.T,
	snapshot derivedAcceptanceSnapshot,
) (*gorm.DB, *knowledgeService, context.Context, *types.Knowledge) {
	t.Helper()
	require.EqualValues(t, 10000, snapshot.Tenant)
	dsn := feishulegacy.PrivateDSN(t)
	require.Contains(t, dsn, "feishu-pagination-pg-20261123", "only the designated disposable acceptance server")
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	t.Chdir(root)
	require.NoError(t, database.RunMigrations(dsn))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlog.Discard})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	tables := []string{
		"tenants",
		"users",
		"tenant_members",
		"knowledge_bases",
		"models",
		"model_catalog_configs",
		"system_settings",
		"feishu_connections",
		"feishu_personal_grant_epochs",
		"feishu_personal_imports",
		"feishu_import_principal_history",
		"feishu_personal_resources",
		"feishu_personal_attempts",
		"data_sources",
		"knowledges",
		"chunks",
		"embeddings",
		"feishu_legacy_resources",
		"feishu_legacy_attempts",
		"document_access_epochs",
		"kb_publication_policies",
		"knowledge_access_policies",
		"document_subject_grants",
		"feishu_sharing_approvals",
	}
	for _, table := range tables {
		data, ok := snapshot.Tables[table]
		require.True(t, ok)
		suffix := " ON CONFLICT DO NOTHING"
		if table == "document_access_epochs" {
			suffix = " ON CONFLICT(knowledge_id) DO UPDATE SET " +
				"incarnation=EXCLUDED.incarnation,generation=EXCLUDED.generation"
		}
		require.NoError(
			t,
			db.Exec(
				"INSERT INTO "+table+" SELECT * FROM jsonb_populate_recordset(NULL::"+table+",?::jsonb)"+suffix,
				string(data),
			).Error,
			table,
		)
	}
	// Resolve the unchanged production SSRF setting through the normal service.
	settings := &systemSettingService{
		repo:  repository.NewSystemSettingRepository(db),
		cache: make(map[string]*types.SystemSetting),
	}
	settings.applySSRFWhitelist(context.Background())
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	db = types.EnableLegacyReadGuards(types.EnablePrivateKBGuards(db))
	var user types.User
	require.NoError(t, db.Where("id=? AND is_active=true", snapshot.Owner).Take(&user).Error)
	var member types.TenantMember
	require.NoError(
		t,
		db.Where("user_id=? AND tenant_id=? AND status='active'", snapshot.Owner, snapshot.Tenant).Take(&member).Error,
	)
	ctx := context.WithValue(context.Background(), types.UserIDContextKey, snapshot.Owner)
	ctx = context.WithValue(ctx, types.TenantIDContextKey, snapshot.Tenant)
	ctx = types.WithCaller(ctx, types.Caller{UserID: snapshot.Owner, TenantID: snapshot.Tenant, Role: member.Role})
	ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: snapshot.Owner})
	repo := repository.NewKnowledgeRepository(db)
	kbRepo := repository.NewKnowledgeBaseRepository(db)
	ks := &knowledgeService{
		repo:      repo,
		kbService: derivedAcceptanceKB{repo: kbRepo},
		chunkRepo: repository.NewChunkRepository(db),
	}
	ks.chunkService = derivedAcceptanceChunks{repo: ks.chunkRepo}
	shares := NewKBShareService(
		repository.NewKBShareRepository(db),
		repository.NewOrganizationRepository(db),
		kbRepo,
		repo,
		nil,
		nil,
	)
	personal := NewFeishuPersonalService(db, repository.NewSystemSettingRepository(db), ks, ks.kbService, nil)
	personal.api.Transport = derivedAcceptanceNoRefresh{base: personal.api.Transport}
	require.NoError(t, EnablePersonalRecovery(personal, ks, shares, t.TempDir()))
	sources := &DataSourceService{}
	sources.SetFeishuIdentityService(personal)
	ConfigureFeishuDerivedWrites(ks, sources)
	knowledge, err := repo.GetKnowledgeByID(ctx, snapshot.Tenant, snapshot.Knowledge)
	require.NoError(t, err)
	return db, ks, ctx, knowledge
}

func TestFeishuDerivedCurrentProtocolLive(t *testing.T) {
	if os.Getenv("FEISHU_DERIVED_LIVE_ACCEPTANCE") != "1" {
		t.Skip("explicit isolated live acceptance opt-in required")
	}
	path := os.Getenv("FEISHU_DERIVED_SNAPSHOT")
	require.Equal(t, "snapshot-10000.private.json", filepath.Base(path))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var snapshot derivedAcceptanceSnapshot
	require.NoError(t, json.Unmarshal(data, &snapshot))
	allowed := map[string]bool{
		"KNOWHUB_ENVIRONMENT":                        true,
		"FEISHU_INTERNAL_HTTP_CALLBACK_ENABLED":      true,
		"FEISHU_INTERNAL_HTTP_CALLBACK_APP_ID":       true,
		"FEISHU_INTERNAL_HTTP_CALLBACK_URL":          true,
		"FEISHU_INTERNAL_HTTP_CALLBACK_IPV4":         true,
		"FEISHU_INTERNAL_HTTP_CALLBACK_APPROVAL_REF": true,

		"SYSTEM_AES_KEY":                             true,
		"SSRF_WHITELIST_EXTRA":                       true,
		"SSRF_DNS_WHITELIST_ONLY":                    true,
		"SSRF_WHITELIST":                             true,
		"FEISHU_PERSONAL_MVP_FIRST_CREATE":           true,
		"FEISHU_PERSONAL_EXACT_RECOVERY":             true,
		"FEISHU_PERSONAL_WORKSPACE_IMPORT_SNAPSHOTS": true,
		"FEISHU_SAFE_INCREMENTAL_ENABLED":            true,
		"FEISHU_ACCESS_V2_ENABLED":                   true,
		"FEISHU_PERSONAL_CONNECTION_CHECKS_ENABLED":  true,
		"FEISHU_DNS_SERVERS":                         true,
	}
	for key, value := range snapshot.Env {
		require.True(t, allowed[key])
		t.Setenv(key, value)
	}
	require.True(t, safeLegacyIncrementalEnabled())
	require.Equal(t, "1", os.Getenv("FEISHU_ACCESS_V2_ENABLED"))
	for _, phase := range []string{
		"derived-and-content-fences", "source-revocation", "connection-revocation", "member-revocation",
		"generation-change", "missing-model", "real-model",
	} {
		t.Run(phase, func(t *testing.T) {
			db, ks, ctx, k := derivedAcceptanceClone(t, snapshot)
			checksum := func() string {
				var result string
				require.NoError(
					t,
					db.Raw(`SELECT md5(
 COALESCE((SELECT string_agg(to_jsonb(c)::text,'' ORDER BY id) FROM chunks c WHERE knowledge_id=?),'')||
 COALESCE((SELECT string_agg(to_jsonb(e)::text,'' ORDER BY id) FROM embeddings e WHERE knowledge_id=?),''))`,
						k.ID, k.ID).
						Scan(&result).
						Error,
				)
				return result
			}
			before := checksum()
			version, managed, err := knowledgeDerivedVersion(ctx, ks.repo, k.ID)
			require.NoError(t, err)
			require.True(t, managed)
			ctx = context.WithValue(ctx, derivedTaskVersionKey{}, version)
			bound, protected, err := ks.beginDerivedSummary(ctx, k.ID, k.TenantID)
			require.NoError(t, err, "real source authorization must succeed without a mock or grant bypass")
			require.True(t, protected)
			switch phase {
			case "derived-and-content-fences":
				k.Description = "isolated derived write acceptance"
				k.Profile = &types.KnowledgeProfile{Gist: "isolated test"}
				k.SummaryStatus = types.SummaryStatusCompleted
				k.UpdatedAt = time.Now()
				require.NoError(t, ks.saveSummaryState(bound, k))
				for _, table := range []string{"knowledges", "chunks", "embeddings"} {
					field, predicate := "content", "knowledge_id=?"
					if table == "knowledges" {
						field, predicate = "title", "id=?"
					}
					query := "UPDATE " + table + " SET " + field + "=COALESCE(" + field +
						",'')||' audit unauthorized' WHERE " + predicate
					require.Error(t, db.Exec(query, k.ID).Error)
				}
			case "source-revocation":
				require.NoError(
					t,
					db.Exec("UPDATE data_sources SET legacy_access_state='revoked' WHERE id=?", snapshot.Source).Error,
				)
				require.Error(t, ks.repo.UpdateKnowledgeColumn(bound, k.ID, "description", "denied"))
			case "connection-revocation":
				require.NoError(
					t,
					db.Exec(
						"UPDATE feishu_personal_grant_epochs SET epoch='isolated-revoked' WHERE user_id=?",
						snapshot.Owner,
					).Error,
				)
				require.Error(t, ks.repo.UpdateKnowledgeColumn(bound, k.ID, "description", "denied"))
			case "member-revocation":
				require.NoError(
					t,
					db.Exec(
						"UPDATE tenant_members SET role='viewer' WHERE user_id=? AND tenant_id=?",
						snapshot.Owner,
						snapshot.Tenant,
					).Error,
				)
				require.Error(t, ks.repo.UpdateKnowledgeColumn(bound, k.ID, "description", "denied"))
			case "generation-change":
				require.NoError(
					t,
					db.Exec(
						"UPDATE feishu_legacy_resources SET generation=generation+1 WHERE knowledge_id=?",
						k.ID,
					).Error,
				)
				require.Error(t, ks.repo.UpdateKnowledgeColumn(bound, k.ID, "description", "denied"))
			case "missing-model":
				require.NoError(
					t,
					db.Exec("UPDATE knowledge_bases SET summary_model_id='' WHERE id=?", k.KnowledgeBaseID).Error,
				)
				payload, err := json.Marshal(
					types.SummaryGenerationPayload{
						TenantID:        k.TenantID,
						KnowledgeBaseID: k.KnowledgeBaseID,
						KnowledgeID:     k.ID,
						SourceVersion:   version,
					},
				)
				require.NoError(t, err)
				require.NoError(
					t,
					ks.ProcessSummaryGeneration(ctx, asynq.NewTask(types.TypeSummaryGeneration, payload)),
				)
				var status string
				require.NoError(t, db.Raw("SELECT summary_status FROM knowledges WHERE id=?", k.ID).Scan(&status).Error)
				require.Equal(t, types.SummaryStatusFailed, status)
			case "real-model":
				if tunnel := os.Getenv("FEISHU_DERIVED_MODEL_TUNNEL"); tunnel != "" {
					var kb types.KnowledgeBase
					require.NoError(t, db.Where("id=?", k.KnowledgeBaseID).Take(&kb).Error)
					var model types.Model
					modelQuery := db.Where("id=? AND tenant_id=?", kb.SummaryModelID, k.TenantID)
					require.NoError(t, modelQuery.Take(&model).Error)
					endpoint, err := url.Parse(model.Parameters.BaseURL)
					require.NoError(t, err)
					require.NotEmpty(t, endpoint.Host)
					target := endpoint.Host
					if endpoint.Port() == "" {
						target = net.JoinHostPort(endpoint.Hostname(), "443")
						if endpoint.Scheme == "http" {
							target = net.JoinHostPort(endpoint.Hostname(), "80")
						}
					}
					dial := func(ctx context.Context, network, address string) (net.Conn, error) {
						if address != target {
							return nil, fmt.Errorf("isolated model tunnel refuses another target")
						}
						return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, tunnel)
					}
					transport := &http.Transport{DialContext: dial}
					// The original URL, normal SSRF validation, redirect policy and
					// safety accounting remain active. Only this exact model's
					// connection uses a temporary authenticated SSH route.
					previous := api.HTTPClient
					api.HTTPClient = utils.NewSSRFSafeHTTPClientWithTransport(
						utils.SSRFSafeHTTPClientConfig{MaxRedirects: 10},
						feishusafety.Transport{Base: transport, BackgroundModel: true})
					t.Cleanup(func() { api.HTTPClient = previous; transport.CloseIdleConnections() })
				}

				ks.modelService = NewModelService(
					repository.NewModelRepository(db),
					repository.NewKnowledgeBaseRepository(db),
					nil,
					nil,
					nil,
					nil,
				)
				ks.config = &config.Config{
					Conversation: &config.ConversationConfig{
						GenerateSummaryPrompt: "Summarize the supplied document in Chinese; do not invent facts.",
						Summary:               &config.SummaryConfig{MaxInputChars: 2048, MaxCompletionTokens: 256},
					},
				}
				payload, err := json.Marshal(
					types.SummaryGenerationPayload{
						TenantID:        k.TenantID,
						KnowledgeBaseID: k.KnowledgeBaseID,
						KnowledgeID:     k.ID,
						SourceVersion:   version,
					},
				)
				require.NoError(t, err)
				work, cancel := context.WithTimeout(ctx, 90*time.Second)
				defer cancel()
				if err := ks.ProcessSummaryGeneration(
					work, asynq.NewTask(types.TypeSummaryGeneration, payload),
				); err != nil {
					t.Fatal("configured-model summary failed; inspect the restricted acceptance log")
				}
				var completed bool
				require.NoError(
					t,
					db.Raw(
						"SELECT summary_status=? AND length(description)>0 FROM knowledges WHERE id=?",
						types.SummaryStatusCompleted, k.ID,
					).
						Scan(&completed).
						Error,
				)
				require.True(t, completed)
			}
			require.Equal(t, before, checksum(), "published chunks and vectors must remain byte-for-byte unchanged")
			var tickets int64
			require.NoError(t, db.Table("feishu_derived_write_grants").Count(&tickets).Error)
			require.Zero(t, tickets)
			t.Logf("tenant=%d phase=%s verified; no production mutations", snapshot.Tenant, phase)
		})
	}
}
