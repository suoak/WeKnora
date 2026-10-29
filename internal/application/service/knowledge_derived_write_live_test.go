//go:build feishu_live_acceptance

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/database"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

// Opt-in cloud acceptance. No fixture issues a permission grant or mocks the
// source binder. Only the existing test-site OAuth connection can be refreshed;
// every publication mutation runs in a fresh clone without an APP/worker.
const derivedLiveHost = "knowhub-feishu-v2-d6e99d894d3a-postgres-1"

func derivedLiveDSN(t *testing.T, key string, snapshot bool) *url.URL {
	t.Helper()
	if os.Getenv("FEISHU_DERIVED_LIVE_ACCEPTANCE") != "1" {
		t.Skip("explicit live acceptance opt-in required")
	}
	u, err := url.Parse(os.Getenv(key))
	require.NoError(t, err)
	require.Equal(t, derivedLiveHost, u.Hostname(), "only the isolated test-site server is allowed")
	require.True(t, u.Scheme == "postgres" || u.Scheme == "postgresql")
	if snapshot {
		require.Equal(t, "/kh_derived_live_base", u.Path)
	} else {
		require.Equal(t, os.Getenv("FEISHU_DERIVED_TEST_SITE_DATABASE"), strings.TrimPrefix(u.Path, "/"))
		require.NotEmpty(t, os.Getenv("FEISHU_DERIVED_TEST_SITE_DATABASE"))
	}
	return u
}

func derivedLiveOpen(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlog.Discard})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	return db
}

// Run once BEFORE taking the disposable snapshot. This follows the normal
// serialized UserToken path on the test site; copying and rotating an old
// refresh token in a clone could invalidate the original test connection.
func TestFeishuDerivedLiveRefreshTestConnection(t *testing.T) {
	u := derivedLiveDSN(t, "FEISHU_DERIVED_TEST_SITE_DSN", false)
	db := derivedLiveOpen(t, u.String())
	var source types.DataSource
	require.NoError(t, db.Where("id=? AND legacy_access_state='active'", os.Getenv("FEISHU_DERIVED_LIVE_SOURCE_ID")).Take(&source).Error)
	cfg, err := source.ParseConfig()
	require.NoError(t, err)
	ref, err := types.ParseFeishuSourceReference(cfg)
	require.NoError(t, err)
	require.NotNil(t, ref)
	require.Equal(t, types.FeishuUserOAuth, ref.Kind)
	var connection types.FeishuConnection
	require.NoError(t, db.Where("user_id=?", ref.OwnerUserID).Take(&connection).Error)
	personal := NewFeishuPersonalService(db, repository.NewSystemSettingRepository(db), nil, nil, nil)
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), types.UserIDContextKey, ref.OwnerUserID), 45*time.Second)
	defer cancel()
	current, configErr := personal.app(ctx)
	if configErr != nil {
		t.Fatal("test-site app configuration could not be validated")
	}
	t.Logf("preflight oauth_enabled=%t source_app_matches=%t connection_app_matches=%t health_checks_enabled=%t", current.modeEnabled(types.FeishuUserOAuth), current.AppID == ref.AppID, current.AppID == connection.AppID, feishuHealthEnabled())
	t.Logf("preflight app_enabled=%t personal_configured=%t rollout_enabled=%t callback_nonloopback_http=%t exact_test_callback_allowed=%t", current.Enabled, current.modeConfigured(types.FeishuUserOAuth), os.Getenv("FEISHU_ACCESS_V2_ENABLED") == "1", feishuNonLoopbackHTTP(current.RedirectURI), feishuTestHTTPCallbackAllowed(current))
	_, health, healthErr := loadFeishuHealth(db, ref.OwnerUserID)
	if healthErr == nil && health != nil {
		t.Logf("preflight health_connected=%t health_reauthorize=%t health_unknown=%t", health.State == "connected", health.State == "reauthorize", health.State == "unknown")
	}
	var encrypted feishuTokens
	decodeErr := openFeishu(connection.Tokens, &encrypted)
	t.Logf("preflight token_envelope_valid=%t token_owner_matches=%t", decodeErr == nil, encrypted.UserID == ref.OwnerUserID)
	token, err := personal.UserToken(ctx, ref.OwnerUserID, ref.AppID, connection.OpenID)
	// Provider-controlled errors/credentials must never be dumped by assertions.
	if err != nil {
		t.Fatalf("normal test-site OAuth token resolution failed (category=%s); reconnect through the existing UI", feishuRemoteFailureCategory(err))
	}
	require.True(t, token != "")
	t.Log("test-site OAuth token resolved through normal serialized refresh; no source/document permission changed")
}

func TestFeishuDerivedLiveMigrateSnapshot(t *testing.T) {
	u := derivedLiveDSN(t, "FEISHU_DERIVED_LIVE_SNAPSHOT_DSN", true)
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	t.Chdir(root)
	require.NoError(t, database.RunMigrations(u.String()))
}

type derivedLiveKBService struct {
	interfaces.KnowledgeBaseService
	repo interfaces.KnowledgeBaseRepository
}

func (s derivedLiveKBService) GetKnowledgeBaseByID(ctx context.Context, id string) (*types.KnowledgeBase, error) {
	return s.repo.GetKnowledgeBaseByID(ctx, id)
}

type derivedLiveNoRefresh struct{}

func (derivedLiveNoRefresh) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("OAuth refresh is prohibited in a disposable clone")
}

type derivedLiveChunks struct {
	interfaces.ChunkService
	repo interfaces.ChunkRepository
}

func (s derivedLiveChunks) ListChunksByKnowledgeID(ctx context.Context, id string) ([]*types.Chunk, error) {
	return s.repo.ListChunksByKnowledgeID(ctx, types.MustTenantIDFromContext(ctx), id)
}

func derivedLiveClone(t *testing.T) (*gorm.DB, *knowledgeService, context.Context, *types.Knowledge, *types.DataSource) {
	t.Helper()
	u := derivedLiveDSN(t, "FEISHU_DERIVED_LIVE_SNAPSHOT_DSN", true)
	adminURL := *u
	adminURL.Path = "/postgres"
	admin := derivedLiveOpen(t, adminURL.String())
	name := "kh_derived_live_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, admin.Exec("CREATE DATABASE "+name+" TEMPLATE kh_derived_live_base").Error)
	// Cleanup executes after this clone's pool closes (LIFO).
	t.Cleanup(func() { require.NoError(t, admin.Exec("DROP DATABASE "+name).Error) })
	u.Path = "/" + name
	db := types.EnableLegacyReadGuards(types.EnablePrivateKBGuards(derivedLiveOpen(t, u.String())))
	var source types.DataSource
	require.NoError(t, db.Where("id=? AND legacy_access_state='active'", os.Getenv("FEISHU_DERIVED_LIVE_SOURCE_ID")).Take(&source).Error)
	cfg, err := source.ParseConfig()
	require.NoError(t, err)
	ref, err := types.ParseFeishuSourceReference(cfg)
	require.NoError(t, err)
	require.NotNil(t, ref)
	var user types.User
	require.NoError(t, db.Where("id=? AND is_active=true", ref.OwnerUserID).Take(&user).Error)
	require.Equal(t, "test", user.Username)
	require.False(t, user.IsSystemAdmin)
	require.False(t, user.CanAccessAllTenants)
	var member types.TenantMember
	require.NoError(t, db.Where("user_id=? AND tenant_id=? AND status='active'", user.ID, source.TenantID).Take(&member).Error)
	ctx := context.WithValue(context.Background(), types.UserIDContextKey, user.ID)
	ctx = context.WithValue(ctx, types.TenantIDContextKey, source.TenantID)
	ctx = types.WithCaller(ctx, types.Caller{UserID: user.ID, TenantID: source.TenantID, Role: member.Role})
	ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: user.ID})
	ks := &knowledgeService{repo: repository.NewKnowledgeRepository(db)}
	kbRepo := repository.NewKnowledgeBaseRepository(db)
	ks.kbService = derivedLiveKBService{repo: kbRepo}
	shares := NewKBShareService(repository.NewKBShareRepository(db), repository.NewOrganizationRepository(db), kbRepo, ks.repo, nil, nil)
	personal := NewFeishuPersonalService(db, repository.NewSystemSettingRepository(db), ks, derivedLiveKBService{repo: kbRepo}, nil)
	personal.api.Transport = derivedLiveNoRefresh{}
	require.NoError(t, EnablePersonalRecovery(personal, ks, shares, t.TempDir()))
	sources := &DataSourceService{}
	sources.SetFeishuIdentityService(personal)
	ConfigureFeishuDerivedWrites(ks, sources)
	binder := ks.derivedSourceBinder
	ks.derivedSourceBinder = func(ctx context.Context, source *types.DataSource) (context.Context, error) {
		bound, err := binder(ctx, source)
		if err != nil {
			t.Logf("live source binder rejected category=%s", feishuRemoteFailureCategory(err))
		}
		return bound, err
	}
	var resource types.FeishuLegacyResource
	require.NoError(t, db.Where("data_source_id=? AND state='active'", source.ID).Take(&resource).Error)
	knowledge, err := ks.repo.GetKnowledgeByID(ctx, source.TenantID, resource.KnowledgeID)
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusCompleted, knowledge.ParseStatus)
	return db, ks, ctx, knowledge, &source
}

func TestFeishuDerivedLiveAuthorizationLifecycle(t *testing.T) {
	t.Setenv("FEISHU_SAFE_INCREMENTAL_ENABLED", "true")
	t.Setenv("FEISHU_PERSONAL_MVP_FIRST_CREATE", "1")
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	t.Chdir(root)
	for _, phase := range []string{"legal-write-and-content-fences", "revocation", "connection-revocation", "membership-revocation", "version-change", "concurrency", "historical-task", "missing-summary-model", "async-summary-real-model", "rollback-reapply"} {
		t.Run(phase, func(t *testing.T) {
			if phase == "async-summary-real-model" && os.Getenv("FEISHU_DERIVED_LIVE_MODEL_CALL") != "1" {
				t.Skip("explicit opt-in for one bounded configured-model request required")
			}
			db, ks, ctx, k, source := derivedLiveClone(t)
			version, managed, err := knowledgeDerivedVersion(ctx, ks.repo, k.ID)
			require.NoError(t, err)
			require.True(t, managed)
			ctx = context.WithValue(ctx, derivedTaskVersionKey{}, version)
			bound, protected, err := ks.beginDerivedSummary(ctx, k.ID, k.TenantID)
			if err != nil {
				t.Fatal("real Feishu source authorization failed; no mock/bypass/fallback was used")
			}
			require.True(t, protected)
			switch phase {
			case "legal-write-and-content-fences":
				k.Description = "isolated real-cloud authorization acceptance"
				k.Profile = &types.KnowledgeProfile{Gist: "isolated acceptance"}
				k.SummaryStatus = types.SummaryStatusCompleted
				k.UpdatedAt = time.Now()
				require.NoError(t, ks.saveSummaryState(bound, k))
				metadata := map[string]interface{}{}
				if len(k.Metadata) != 0 {
					require.NoError(t, json.Unmarshal(k.Metadata, &metadata))
				}
				metadata["derived"] = map[string]interface{}{"acceptance": true}
				value, err := json.Marshal(metadata)
				require.NoError(t, err)
				require.NoError(t, ks.repo.UpdateKnowledgeColumn(bound, k.ID, "metadata", types.JSON(value)))
				var saved bool
				require.NoError(t, db.Raw("SELECT description=? AND profile->>'gist'=? AND summary_status=? AND metadata->'derived'->>'acceptance'='true' FROM knowledges WHERE id=?", k.Description, k.Profile.Gist, k.SummaryStatus, k.ID).Scan(&saved).Error)
				require.True(t, saved)
				var grants int64
				require.NoError(t, db.Table("feishu_derived_write_grants").Count(&grants).Error)
				require.Zero(t, grants)
				for _, table := range []string{"knowledges", "chunks", "embeddings"} {
					column, predicate := "content", "knowledge_id=?"
					if table == "knowledges" {
						column, predicate = "title", "id=?"
					}
					var count int64
					require.NoError(t, db.Table(table).Where(predicate, k.ID).Count(&count).Error)
					require.Positive(t, count)
					err := db.Exec("UPDATE "+table+" SET "+column+"="+column+" WHERE "+predicate, k.ID).Error
					require.Error(t, err, "unmanaged content writes must stay fenced")
				}
			case "revocation":
				require.NoError(t, db.Model(&types.DataSource{}).Where("id=?", source.ID).Update("legacy_access_state", "revoked").Error)
				require.ErrorIs(t, ks.repo.UpdateKnowledgeColumn(bound, k.ID, "description", "denied"), repository.ErrDerivedWriteRejected)
				_, _, err := ks.beginDerivedSummary(ctx, k.ID, k.TenantID)
				require.ErrorIs(t, err, repository.ErrDerivedWriteRejected)
			case "connection-revocation":
				recovery, ok := ks.personalRecovery.(*PersonalRecovery)
				require.True(t, ok)
				require.NoError(t, recovery.service.Disconnect(ctx))
				require.ErrorIs(t, ks.repo.UpdateKnowledgeColumn(bound, k.ID, "description", "denied"), repository.ErrDerivedWriteRejected)
			case "membership-revocation":
				owner, ok := types.UserIDFromContext(ctx)
				require.True(t, ok)
				require.NoError(t, db.Where("user_id=? AND tenant_id=?", owner, k.TenantID).Delete(&types.TenantMember{}).Error)
				require.ErrorIs(t, ks.repo.UpdateKnowledgeColumn(bound, k.ID, "description", "denied"), repository.ErrDerivedWriteRejected)
			case "version-change":
				require.NoError(t, db.Model(&types.FeishuLegacyResource{}).Where("knowledge_id=?", k.ID).Update("generation", gorm.Expr("generation+1")).Error)
				require.ErrorIs(t, ks.repo.UpdateKnowledgeColumn(bound, k.ID, "description", "stale"), repository.ErrDerivedWriteRejected)
			case "concurrency":
				second, _, err := ks.beginDerivedSummary(ctx, k.ID, k.TenantID)
				require.NoError(t, err)
				require.NoError(t, ks.repo.UpdateKnowledgeColumn(bound, k.ID, "description", "newer"))
				require.ErrorIs(t, ks.repo.UpdateKnowledgeColumn(second, k.ID, "description", "older"), repository.ErrDerivedWriteRejected)
			case "historical-task":
				for _, taskVersion := range []string{"", "superseded"} {
					payload, err := json.Marshal(types.SummaryGenerationPayload{TenantID: k.TenantID, KnowledgeBaseID: k.KnowledgeBaseID, KnowledgeID: k.ID, SourceVersion: taskVersion})
					require.NoError(t, err)
					require.NoError(t, ks.ProcessSummaryGeneration(ctx, asynq.NewTask(types.TypeSummaryGeneration, payload)))
				}
			case "missing-summary-model":
				require.NoError(t, db.Model(&types.KnowledgeBase{}).Where("id=?", k.KnowledgeBaseID).Update("summary_model_id", "").Error)
				payload, err := json.Marshal(types.SummaryGenerationPayload{TenantID: k.TenantID, KnowledgeBaseID: k.KnowledgeBaseID, KnowledgeID: k.ID, SourceVersion: version})
				require.NoError(t, err)
				require.NoError(t, ks.ProcessSummaryGeneration(ctx, asynq.NewTask(types.TypeSummaryGeneration, payload)))
				var status string
				require.NoError(t, db.Raw("SELECT summary_status FROM knowledges WHERE id=?", k.ID).Scan(&status).Error)
				require.Equal(t, types.SummaryStatusFailed, status)
			case "async-summary-real-model":
				ks.chunkRepo = repository.NewChunkRepository(db)
				ks.chunkService = derivedLiveChunks{repo: ks.chunkRepo}
				ks.modelService = NewModelService(repository.NewModelRepository(db), repository.NewKnowledgeBaseRepository(db), nil, nil, nil, nil)
				ks.config = &config.Config{Conversation: &config.ConversationConfig{
					GenerateSummaryPrompt: "Summarize only the supplied document in Chinese, without inventing facts.",
					Summary:               &config.SummaryConfig{MaxInputChars: 2048, MaxCompletionTokens: 256},
				}}
				modelCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
				defer cancel()
				payload, err := json.Marshal(types.SummaryGenerationPayload{TenantID: k.TenantID, KnowledgeBaseID: k.KnowledgeBaseID, KnowledgeID: k.ID, SourceVersion: version})
				require.NoError(t, err)
				// Do not print model errors or generated source-derived text.
				if err := ks.ProcessSummaryGeneration(modelCtx, asynq.NewTask(types.TypeSummaryGeneration, payload)); err != nil {
					if errors.Is(err, types.ErrLegacySourceRestricted) {
						t.Fatal("existing source/model input protection rejected this fixture; real-model positive acceptance has NOT passed")
					}
					t.Fatal("real configured-model summary task failed; inspect restricted test-host logs")
				}
				var completed bool
				require.NoError(t, db.Raw("SELECT summary_status=? AND length(description)>0 FROM knowledges WHERE id=?", types.SummaryStatusCompleted, k.ID).Scan(&completed).Error)
				require.True(t, completed)
			case "rollback-reapply":
				for _, direction := range []string{"down", "up"} {
					ddl, err := os.ReadFile("migrations/versioned/003043_feishu_derived_write." + direction + ".sql")
					require.NoError(t, err)
					require.NoError(t, db.Exec(string(ddl)).Error)
					if direction == "down" {
						require.Error(t, db.Exec("UPDATE knowledges SET description=description WHERE id=?", k.ID).Error)
					}
				}
				bound, _, err := ks.beginDerivedSummary(ctx, k.ID, k.TenantID)
				require.NoError(t, err)
				require.NoError(t, ks.repo.UpdateKnowledgeColumn(bound, k.ID, "description", "reapplied"))
			}
		})
	}
}
