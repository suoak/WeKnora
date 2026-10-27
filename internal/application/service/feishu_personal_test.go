package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func personalUser(user string) context.Context {
	return context.WithValue(context.Background(), types.UserIDContextKey, user)
}

type feishuSettingRepo struct {
	rows map[string]*types.SystemSetting
}

func (r *feishuSettingRepo) Get(_ context.Context, key string) (*types.SystemSetting, error) {
	return r.rows[key], nil
}

func (r *feishuSettingRepo) List(context.Context) ([]*types.SystemSetting, error) { return nil, nil }

func (r *feishuSettingRepo) Upsert(_ context.Context, row *types.SystemSetting) error {
	r.rows[row.Key] = row
	return nil
}

func (r *feishuSettingRepo) UpsertBatch(ctx context.Context, rows []*types.SystemSetting) error {
	for _, row := range rows {
		_ = r.Upsert(ctx, row)
	}
	return nil
}

func (r *feishuSettingRepo) Delete(_ context.Context, key string) (bool, error) {
	_, ok := r.rows[key]
	delete(r.rows, key)
	return ok, nil
}

func feishuFixture(t *testing.T) (*FeishuPersonalService, *feishuSettingRepo) {
	t.Helper()
	t.Setenv("SYSTEM_AES_KEY", "01234567890123456789012345678901")
	t.Setenv("SSRF_WHITELIST", "127.0.0.1,localhost,open.feishu.cn")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Exec("CREATE TABLE users (id VARCHAR(36) PRIMARY KEY)").Error)
	require.NoError(t, db.Exec("CREATE TABLE knowledge_bases (id VARCHAR(36) PRIMARY KEY)").Error)
	ddl, err := os.ReadFile("../../../migrations/sqlite/003025_feishu_personal.up.sql")
	require.NoError(t, err)
	for _, statement := range strings.Split(string(ddl), ";") {
		if strings.TrimSpace(statement) != "" {
			require.NoError(t, db.Exec(statement).Error)
		}
	}
	for _, name := range []string{"003031_feishu_personal_recovery", "003033_feishu_application_grant"} {
		b, err := os.ReadFile("../../../migrations/sqlite/" + name + ".up.sql")
		require.NoError(t, err)
		require.NoError(t, db.Exec(string(b)).Error)
	}
	for _, user := range []string{"A", "B"} {
		require.NoError(t, db.Exec("INSERT INTO users(id) VALUES (?)", user).Error)
	}
	repo := &feishuSettingRepo{rows: map[string]*types.SystemSetting{}}
	s := NewFeishuPersonalService(db, repo, nil, nil, nil)
	admin := context.WithValue(personalUser("A"), types.SystemAdminContextKey, true)
	require.NoError(
		t,
		s.SaveApp(
			admin,
			&FeishuAppConfig{
				AppID:       "cli_test",
				Secret:      "super-secret",
				RedirectURI: "https://knowhub.example/feishu/callback",
				Enabled:     true,
			},
		),
	)
	return s, repo
}

func TestFeishuAppEncryptedAdminOnlyAndStatusRedacted(t *testing.T) {
	s, repo := feishuFixture(t)
	require.True(t, repo.rows[feishuAppSetting].IsSecret)
	require.NotContains(t, string(repo.rows[feishuAppSetting].Value), "super-secret")
	cfg, err := s.app(context.Background())
	require.NoError(t, err)
	require.Equal(t, "super-secret", cfg.Secret)
	encoded, err := json.Marshal(repo.rows[feishuAppSetting])
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "enc:v1:")
	status, err := json.Marshal(s.Status(personalUser("B")))
	require.NoError(t, err)
	require.NotContains(t, string(status), "cli_test")
	require.NotContains(t, string(status), "secret")
	err = s.SaveApp(personalUser("B"), cfg)
	require.Error(t, err)
	t.Setenv("SYSTEM_AES_KEY", "")
	_, err = sealFeishu(map[string]string{"access_token": "test"})
	require.Error(t, err)
}

func TestFeishuOAuthAuthorizationRevisionChangesOnlyOnConsent(t *testing.T) {
	s, _ := feishuFixture(t)
	require.NoError(t, s.db.Create(&types.FeishuConnection{
		UserID: "A", AppID: "cli_test", OpenID: "open-A", Tokens: "sealed-not-returned",
		ExpiresAt: time.Now().Add(time.Hour), RefreshExpiresAt: time.Now().Add(24 * time.Hour),
	}).Error)
	require.NoError(t, s.db.Create(&types.FeishuPersonalGrantEpoch{UserID: "A", Epoch: "consent-one"}).Error)
	before := s.Status(personalUser("A"))["authorization_revision"]
	require.NotEmpty(t, before)
	require.NotContains(t, before, "consent-one")
	require.NoError(t, s.db.Model(&types.FeishuConnection{}).Where("user_id = ?", "A").
		Updates(map[string]any{"tokens": "refreshed-sealed", "updated_at": time.Now()}).Error)
	require.Equal(
		t,
		before,
		s.Status(personalUser("A"))["authorization_revision"],
		"token refresh cannot confirm a new consent",
	)
	require.NoError(
		t,
		s.db.Model(&types.FeishuPersonalGrantEpoch{}).
			Where("user_id = ?", "A").
			Update("epoch", "consent-two").
			Error,
	)
	require.NotEqual(t, before, s.Status(personalUser("A"))["authorization_revision"])
	require.Nil(t, s.Status(personalUser("B"))["authorization_revision"])
	require.NoError(t, s.Disconnect(personalUser("A")))
	require.Nil(t, s.Status(personalUser("A"))["authorization_revision"])
}

func TestFeishuOAuthStateOwnerReplayExpiryAndDisconnect(t *testing.T) {
	s, _ := feishuFixture(t)
	cfg, err := s.app(context.Background())
	require.NoError(t, err)
	state := strings.Repeat("s", 43)
	require.NoError(
		t,
		s.db.Create(
			&types.FeishuOAuthState{
				Hash:          stateHash(state),
				UserID:        "A",
				ConfigVersion: cfg.Version,
				ExpiresAt:     time.Now().Add(time.Minute),
			},
		).Error,
	)
	var exchanges atomic.Int32
	s.api = &http.Client{
		Transport: feishuTestTransport(
			func(_ *http.Request) (*http.Response, error) { exchanges.Add(1); return nil, errors.New("offline") },
		),
	}
	require.Error(t, s.Complete(personalUser("B"), state, "code"))
	require.EqualValues(t, 0, exchanges.Load())
	require.Error(t, s.Complete(personalUser("A"), state, "code"))
	require.EqualValues(t, 1, exchanges.Load())
	require.Error(t, s.Complete(personalUser("A"), state, "code"))
	require.EqualValues(t, 1, exchanges.Load())
	require.NoError(t, s.Disconnect(personalUser("A")))
	var count int64
	require.NoError(t, s.db.Model(&types.FeishuOAuthState{}).Where("user_id = ?", "A").Count(&count).Error)
	require.Zero(t, count)
	expired := strings.Repeat("x", 43)
	require.NoError(
		t,
		s.db.Create(
			&types.FeishuOAuthState{
				Hash:          stateHash(expired),
				UserID:        "A",
				ConfigVersion: cfg.Version,
				ExpiresAt:     time.Now().Add(-time.Second),
			},
		).Error,
	)
	require.Error(t, s.Complete(personalUser("A"), expired, "code"))
	require.EqualValues(t, 1, exchanges.Load())
}

type feishuTestTransport func(*http.Request) (*http.Response, error)

func (f feishuTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func feishuTokenResponse() *http.Response {
	return &http.Response{
		StatusCode: 200,
		Body: io.NopCloser(
			strings.NewReader(
				"{\"code\":0,\"access_token\":\"new-access\"," +
					"\"refresh_token\":\"new-refresh\",\"expires_in\":7200," +
					"\"refresh_token_expires_in\":86400}",
			),
		),
		Header: make(http.Header),
	}
}

func TestFeishuRefreshOwnerBoundAndDisconnectStopsFetch(t *testing.T) {
	s, _ := feishuFixture(t)
	encrypted, err := sealFeishu(feishuTokens{UserID: "A", Access: "old", Refresh: "refresh"})
	require.NoError(t, err)
	require.NoError(
		t,
		s.db.Create(
			&types.FeishuConnection{
				UserID:           "A",
				AppID:            "cli_test",
				OpenID:           "open-A",
				Tokens:           encrypted,
				ExpiresAt:        time.Now().Add(-time.Second),
				RefreshExpiresAt: time.Now().Add(time.Hour),
			},
		).Error,
	)
	var refreshes atomic.Int32
	_, err = s.UserToken(personalUser("B"), "A", "cli_test", "open-A")
	require.Error(t, err)
	s.api = &http.Client{Transport: feishuTestTransport(func(r *http.Request) (*http.Response, error) {
		refreshes.Add(1)
		var payload map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		require.Equal(t, "refresh_token", payload["grant_type"])
		return feishuTokenResponse(), nil
	})}
	_, err = s.UserToken(personalUser("B"), "B", "cli_test", "open-A")
	require.Error(t, err)
	require.Zero(t, refreshes.Load())
	token, err := s.UserToken(personalUser("A"), "A", "cli_test", "open-A")
	require.NoError(t, err)
	require.Equal(t, "new-access", token)
	_, err = s.UserToken(personalUser("A"), "A", "cli_test", "open-B")
	require.Error(t, err)
	require.NoError(t, s.Disconnect(personalUser("A")))
	_, err = s.UserToken(personalUser("A"), "A", "cli_test", "open-A")
	require.Error(t, err)
	require.EqualValues(t, 1, refreshes.Load())
}

type personalKBStub struct {
	interfaces.KnowledgeBaseService
	kb *types.KnowledgeBase
}

func (s *personalKBStub) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

type personalKnowledgeStub struct {
	interfaces.KnowledgeService
	creates, replaces int
	item              *types.Knowledge
}

func (s *personalKnowledgeStub) CreateKnowledgeFromFile(
	_ context.Context,
	kb string,
	_ *multipart.FileHeader,
	meta map[string]string,
	_ *bool,
	_ string,
	_ []string,
	channel string,
	_ *types.KnowledgeProcessOverrides,
) (*types.Knowledge, error) {
	s.creates++
	s.item = &types.Knowledge{
		ID:              "knowledge",
		KnowledgeBaseID: kb,
		ParseStatus:     types.ParseStatusCompleted,
		Source:          meta["source_url"],
		Channel:         channel,
	}
	return s.item, nil
}

func (s *personalKnowledgeStub) ReplaceKnowledgeFile(
	_ context.Context,
	_ string,
	_ *multipart.FileHeader,
	_ string,
	_ map[string]string,
) (*types.Knowledge, error) {
	s.replaces++
	return s.item, nil
}

func (s *personalKnowledgeStub) GetKnowledgeByID(context.Context, string) (*types.Knowledge, error) {
	return s.item, nil
}

func TestFeishuImportPermissionIdentityDedupeAndFailedResyncPreservesKnowledge(t *testing.T) {
	s, _ := feishuFixture(t)
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 1}
	s.kbs = &personalKBStub{kb: kb}
	knowledge := &personalKnowledgeStub{}
	s.knowledge = knowledge
	require.NoError(t, s.db.Exec("INSERT INTO knowledge_bases(id) VALUES ('kb')").Error)
	for _, user := range []string{"A", "B"} {
		encrypted, err := sealFeishu(
			feishuTokens{UserID: user, Access: "access-" + user, Refresh: "refresh"},
		)
		require.NoError(t, err)
		require.NoError(
			t,
			s.db.Create(
				&types.FeishuConnection{
					UserID:           user,
					AppID:            "cli_test",
					OpenID:           "open-" + user,
					Tokens:           encrypted,
					ExpiresAt:        time.Now().Add(time.Hour),
					RefreshExpiresAt: time.Now().Add(time.Hour),
				},
			).Error,
		)
	}
	modified := "1"
	denyDownload := false
	denyWiki := false
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer access-A" {
			w.WriteHeader(403)
			_, _ = io.WriteString(w, `{"code":1069902}`)
			return
		}
		switch r.URL.Path {
		case "/open-apis/wiki/v2/spaces/get_node":
			if denyWiki {
				w.WriteHeader(403)
				_, _ = io.WriteString(w, `{"code":1069902}`)
				return
			}
			_, _ = io.WriteString(
				w,
				"{\"code\":0,\"data\":{\"node\":{\"obj_type\":\"file\","+
					"\"obj_token\":\"real\",\"title\":\"test.pdf\"}}}",
			)
		case "/open-apis/drive/v1/metas/batch_query":
			_, _ = io.WriteString(
				w,
				"{\"code\":0,\"data\":{\"metas\":[{\"doc_type\":\"file\","+
					"\"doc_token\":\"real\",\"title\":\"test.pdf\","+
					"\"latest_modify_time\":\""+modified+`"}]}}`,
			)
		case "/open-apis/drive/v1/files/real/download":
			if denyDownload {
				w.WriteHeader(403)
				_, _ = io.WriteString(w, `{"code":1069902}`)
			} else {
				_, _ = io.WriteString(w, "%PDF-1.7 test")
			}
		default:
			t.Errorf("unexpected API: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	s.newClient = func(cfg *core.Config) *core.Client { cfg.BaseURL = server.URL; return core.NewClient(cfg) }
	_, err := s.Import(personalUser("A"), "kb", "https://team.feishu.cn/file/real")
	require.Error(t, err)
	require.Zero(t, requests.Load())
	ctx, err := access.WithKBTaskWrite(personalUser("A"), kb, 1)
	require.NoError(t, err)
	row, err := s.Import(ctx, "kb", "https://team.feishu.cn/wiki/node?from=share")
	require.NoError(t, err)
	require.Equal(t, "knowledge", row.KnowledgeID)
	require.Equal(t, "file", row.ResourceType)
	require.Equal(t, 1, knowledge.creates)
	denyWiki = true
	_, err = s.Import(ctx, "kb", "https://team.feishu.cn/wiki/node?from=another_link")
	require.Error(t, err)
	var deniedWiki types.FeishuPersonalImport
	require.NoError(t, s.db.First(&deniedWiki, "id = ?", row.ID).Error)
	require.Equal(t, "needs_attention", deniedWiki.Status)
	require.Equal(t, "knowledge", deniedWiki.KnowledgeID)
	require.Equal(t, "1", deniedWiki.SourceModified)
	require.Zero(t, knowledge.replaces)
	denyWiki = false
	duplicate, err := s.Import(ctx, "kb", "https://team.feishu.cn/file/real")
	require.NoError(t, err)
	require.Equal(t, row.ID, duplicate.ID)
	require.Equal(t, 1, knowledge.creates)
	require.Zero(t, knowledge.replaces)
	bCtx, err := access.WithKBTaskWrite(personalUser("B"), kb, 1)
	require.NoError(t, err)
	_, err = s.Import(bCtx, "kb", "https://team.feishu.cn/file/real")
	require.Error(t, err)
	require.Equal(t, 1, knowledge.creates)
	modified = "2"
	denyDownload = true
	_, err = s.Import(ctx, "kb", "https://team.feishu.cn/file/real")
	require.Error(t, err)
	require.Zero(t, knowledge.replaces)
	var persisted types.FeishuPersonalImport
	require.NoError(t, s.db.First(&persisted, "id = ?", row.ID).Error)
	require.Equal(t, "knowledge", persisted.KnowledgeID)
	require.Equal(t, "1", persisted.SourceModified)
	require.Equal(t, "needs_attention", persisted.Status)
	denyDownload = false
	// MVP explicitly withdraws legacy automatic updates. The original test
	// source is preserved in the stability tree / reuse-source manifest.
	_, err = s.Import(ctx, "kb", "https://team.feishu.cn/file/real")
	require.ErrorIs(t, err, repository.ErrPersonalUpdateDisabled)
	require.Zero(t, knowledge.replaces)
	require.NoError(t, s.db.First(&persisted, "id = ?", row.ID).Error)
	require.Equal(t, "knowledge", persisted.KnowledgeID)
	require.Equal(t, "1", persisted.SourceModified)
}

func TestFeishuDisconnectDuringOAuthCannotResurrectConnection(t *testing.T) {
	s, _ := feishuFixture(t)
	cfg, err := s.app(context.Background())
	require.NoError(t, err)
	state := strings.Repeat("q", 43)
	require.NoError(
		t,
		s.db.Create(
			&types.FeishuOAuthState{
				Hash:          stateHash(state),
				UserID:        "A",
				ConfigVersion: cfg.Version,
				ExpiresAt:     time.Now().Add(time.Minute),
			},
		).Error,
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"code":0,"data":{"open_id":"open-A"}}`)
	}))
	defer server.Close()
	s.newClient = func(cfg *core.Config) *core.Client { cfg.BaseURL = server.URL; return core.NewClient(cfg) }
	s.api = &http.Client{Transport: feishuTestTransport(func(*http.Request) (*http.Response, error) {
		require.NoError(t, s.Disconnect(personalUser("A")))
		return feishuTokenResponse(), nil
	})}
	require.Error(t, s.Complete(personalUser("A"), state, "code"))
	var count int64
	require.NoError(t, s.db.Model(&types.FeishuConnection{}).Where("user_id = ?", "A").Count(&count).Error)
	require.Zero(t, count)
}

func TestFeishuPlaintextAndSwappedOwnerRejected(t *testing.T) {
	s, _ := feishuFixture(t)
	for _, tokens := range []string{`{"owner":"A","access_token":"plaintext"}`, func() string {
		x, err := sealFeishu(feishuTokens{UserID: "B", Access: "wrong-owner"})
		require.NoError(t, err)
		return x
	}()} {
		require.NoError(
			t,
			s.db.Save(
				&types.FeishuConnection{
					UserID:           "A",
					AppID:            "cli_test",
					OpenID:           "open-A",
					Tokens:           tokens,
					ExpiresAt:        time.Now().Add(time.Hour),
					RefreshExpiresAt: time.Now().Add(time.Hour),
				},
			).Error,
		)
		_, err := s.UserToken(personalUser("A"), "A", "cli_test", "open-A")
		require.Error(t, err)
	}
}

func TestFeishuOAuthRejectsOversizedValidJSONPrefix(t *testing.T) {
	s, _ := feishuFixture(t)
	s.api = &http.Client{Transport: feishuTestTransport(func(*http.Request) (*http.Response, error) {
		body := "{\"access_token\":\"access\",\"refresh_token\":\"refresh\"," +
			"\"expires_in\":3600,\"refresh_token_expires_in\":7200}" + strings.Repeat(
			" ",
			1024*1024,
		)
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})}
	_, err := s.oauthRequest(context.Background(), map[string]string{"grant_type": "synthetic"})
	require.ErrorIs(t, err, core.ErrResponseTooLarge)
}
