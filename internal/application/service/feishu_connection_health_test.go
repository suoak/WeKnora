package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func connectionHealthFixture(
	t *testing.T,
) (*FeishuPersonalService, *atomic.Int32, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	s, _ := feishuFixture(t)
	t.Setenv("FEISHU_PERSONAL_CONNECTION_CHECKS_ENABLED", "1")
	require.NoError(t, s.db.AutoMigrate(&types.SystemSetting{}))
	require.NoError(
		t,
		s.db.Exec("ALTER TABLE users ADD COLUMN is_active boolean NOT NULL DEFAULT true").Error,
	)
	require.NoError(t, s.db.Exec("ALTER TABLE users ADD COLUMN deleted_at datetime").Error)
	cipher, err := sealFeishu(feishuTokens{UserID: "A", Access: "old-access", Refresh: "old-refresh"})
	require.NoError(t, err)
	require.NoError(
		t,
		s.db.Create(
			&types.FeishuConnection{
				UserID:           "A",
				AppID:            "cli_test",
				OpenID:           "open-A",
				Tokens:           cipher,
				ExpiresAt:        time.Now().Add(time.Hour),
				RefreshExpiresAt: time.Now().Add(24 * time.Hour),
			},
		).Error,
	)
	require.NoError(
		t,
		s.db.Create(&types.FeishuPersonalGrantEpoch{UserID: "A", Epoch: "original-consent"}).Error,
	)
	var mode, calls, refresh atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if mode.Load() == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if mode.Load() == 2 || mode.Load() == 3 && r.Header.Get("Authorization") == "Bearer old-access" {
			_, _ = io.WriteString(w, `{"code":99991668,"msg":"expired"}`)
			return
		}
		_, _ = io.WriteString(w, `{"code":0,"data":{"open_id":"open-A","tenant_key":"enterprise"}}`)
	}))
	t.Cleanup(server.Close)
	s.newClient = func(cfg *core.Config) *core.Client { cfg.BaseURL = server.URL; return core.NewClient(cfg) }
	s.api = &http.Client{Transport: feishuTestTransport(func(r *http.Request) (*http.Response, error) {
		refresh.Add(1)
		if mode.Load() == 2 {
			return &http.Response{
				StatusCode: 400,
				Body:       io.NopCloser(strings.NewReader(`{"error":"invalid_grant"}`)),
			}, nil
		}
		return feishuTokenResponse(), nil
	})}
	return s, &mode, &calls, &refresh
}

func TestFeishuOAuthConnectionHealthRefreshAndRateLimit(t *testing.T) {
	s, mode, calls, refresh := connectionHealthFixture(t)
	mode.Store(3)
	h, err := s.inspectConnection(personalUser("A"), "A")
	require.NoError(t, err)
	require.Equal(t, "connected", h.State)
	require.EqualValues(t, 1, refresh.Load(), "expired access token refreshes instead of unlinking")
	count := calls.Load()
	for range 3 {
		h, err = s.inspectConnection(personalUser("A"), "A")
		require.NoError(t, err)
		require.Equal(t, "connected", h.State)
	}
	require.Equal(t, count, calls.Load(), "status reads share a five-minute lease/cache across processes")
	_, stored, err := loadFeishuHealth(s.db, "A")
	require.NoError(t, err)
	stored.CheckAfter = time.Now().Add(-time.Minute)
	require.NoError(t, storeFeishuHealth(s.db, "A", stored))
	mode.Store(1)
	h, err = s.inspectConnection(personalUser("A"), "A")
	require.NoError(t, err)
	require.Equal(t, "unknown", h.State)
	require.EqualValues(t, 1, refresh.Load(), "5xx is not authorization revocation")
}

func TestFeishuOAuthConnectionHealthConfirmedRevocationAndDisconnect(t *testing.T) {
	s, mode, calls, _ := connectionHealthFixture(t)
	mode.Store(2)
	h, err := s.inspectConnection(personalUser("A"), "A")
	require.NoError(t, err)
	require.Equal(t, "reauthorize", h.State)
	count := calls.Load()
	_, err = s.UserToken(personalUser("A"), "A", "cli_test", "open-A")
	require.ErrorIs(t, err, ErrFeishuReconnect)
	require.Equal(t, count, calls.Load())
	var conn types.FeishuConnection
	require.NoError(t, s.db.Where("user_id = ?", "A").First(&conn).Error)
	require.NoError(t, s.Disconnect(personalUser("A")))
	_, err = s.UserToken(personalUser("A"), "A", "cli_test", "open-A")
	require.ErrorIs(t, err, ErrFeishuReconnect)
}

func TestFeishuOAuthRevocationHintNeverDeletesRenewableGrant(t *testing.T) {
	s, _, calls, _ := connectionHealthFixture(t)
	ctx := personalUser("A")
	h, err := s.inspectConnection(ctx, "A")
	require.NoError(t, err)
	require.NoError(
		t,
		s.HandleRevocationHint(
			ctx,
			"event-one",
			"cli_test",
			"enterprise",
			"open-A",
			time.Now(),
			"user_access_token",
		),
	)
	require.NoError(
		t,
		s.HandleRevocationHint(
			ctx,
			"event-one",
			"cli_test",
			"enterprise",
			"open-A",
			time.Now(),
			"user_access_token",
		),
	)
	before := calls.Load()
	h, err = s.inspectConnection(ctx, "A")
	require.NoError(t, err)
	require.Equal(t, "connected", h.State)
	require.Greater(t, calls.Load(), before)
	// Late old-consent and wrong-tenant events cannot alter the current grant.
	after := h.CheckAfter
	require.NoError(
		t,
		s.HandleRevocationHint(
			ctx,
			"late-old",
			"cli_test",
			"enterprise",
			"open-A",
			h.ConsentAt.Add(-time.Second),
			"refresh_token",
		),
	)
	require.NoError(
		t,
		s.HandleRevocationHint(
			ctx,
			"wrong-tenant",
			"cli_test",
			"other",
			"open-A",
			time.Now(),
			"refresh_token",
		),
	)
	_, h, err = loadFeishuHealth(s.db, "A")
	require.NoError(t, err)
	require.Equal(t, after, h.CheckAfter)
	// A late result from a failed refresh cannot overwrite a new encrypted grant.
	var old types.FeishuConnection
	require.NoError(t, s.db.Where("user_id = ?", "A").First(&old).Error)
	require.NoError(
		t,
		s.db.Model(&types.FeishuConnection{}).
			Where("user_id = ?", "A").
			UpdateColumn("tokens", "newer-encrypted-credential").
			Error,
	)
	s.noteConnectionFailure(context.Background(), &old, ErrFeishuRefreshRejected)
	_, h, err = loadFeishuHealth(s.db, "A")
	require.NoError(t, err)
	require.Equal(t, "connected", h.State)
	var events int64
	require.NoError(
		t,
		s.db.Model(&types.SystemSetting{}).
			Where("key LIKE ?", "integration.feishu.revoke_event.%").
			Count(&events).
			Error,
	)
	require.EqualValues(t, 1, events)
}

func TestFeishuV2PostgresConnectionHealthLeaseAndReauthorizationFence(t *testing.T) {
	f := v2PostgresFixture(t)
	t.Setenv("FEISHU_PERSONAL_CONNECTION_CHECKS_ENABLED", "1")
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		_, _ = io.WriteString(w, `{"code":0,"data":{"open_id":"open-A","tenant_key":"enterprise"}}`)
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	f.s.newClient = func(cfg *core.Config) *core.Client { cfg.BaseURL = server.URL; return core.NewClient(cfg) }
	result := make(chan error, 1)
	go func() { _, err := f.s.inspectConnection(f.ctx, "A"); result <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("probe not started")
	}
	// A second worker cannot duplicate the in-flight remote probe.
	_, err := f.s.inspectConnection(f.ctx, "A")
	require.NoError(t, err)
	require.EqualValues(t, 1, calls.Load())
	require.NoError(t, f.db.Transaction(func(tx *gorm.DB) error {
		if e := tx.
			Model(&types.FeishuPersonalGrantEpoch{}).
			Where("user_id = ?", "A").
			Update("epoch", "new-consent").Error; e != nil {
			return e
		}
		return storeFeishuHealth(
			tx,
			"A",
			&feishuConnectionHealth{
				AppID:      "cli_test",
				OpenID:     "open-A",
				Epoch:      "new-consent",
				State:      "connected",
				CheckedAt:  time.Now(),
				CheckAfter: time.Now().Add(5 * time.Minute),
				ConsentAt:  time.Now(),
			},
		)
	}))
	close(release)
	require.ErrorIs(t, <-result, ErrFeishuReconnect)
	_, h, err := loadFeishuHealth(f.db, "A")
	require.NoError(t, err)
	require.Equal(t, "new-consent", h.Epoch)
	require.Equal(t, "connected", h.State)
}
