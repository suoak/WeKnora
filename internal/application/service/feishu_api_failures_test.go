package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestFeishuRefreshRejectedGenerationUsesPersistedOwnerGrant(t *testing.T) {
	s, _ := feishuFixture(t)
	encrypted, err := sealFeishu(feishuTokens{UserID: "A", Access: "old-access", Refresh: "existing-refresh"})
	require.NoError(t, err)
	require.NoError(t, s.db.Create(&types.FeishuConnection{
		UserID: "A", AppID: "cli_test", OpenID: "open-A",
		Tokens: encrypted, ExpiresAt: time.Now().Add(time.Hour), RefreshExpiresAt: time.Now().Add(24 * time.Hour),
	}).Error)
	refreshes, requests := 0, 0
	s.api = &http.Client{Transport: feishuTestTransport(func(*http.Request) (*http.Response, error) {
		refreshes++
		return feishuTokenResponse(), nil
	})}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") == "Bearer old-access" {
			_, _ = io.WriteString(w, `{"code":99991677}`)
			return
		}
		require.Equal(t, "Bearer new-access", r.Header.Get("Authorization"))
		_, _ = io.WriteString(w, `{"code":0}`)
	}))
	defer server.Close()
	ctx := core.WithUserIdentity(personalUser("A"), func(ctx context.Context) (string, error) {
		return s.UserToken(ctx, "A", "cli_test", "open-A")
	})
	err = core.NewClient(&core.Config{BaseURL: server.URL}).DoRequest(ctx, http.MethodGet, "/resource", nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, refreshes)
	require.Equal(t, 2, requests)
	token, err := s.UserToken(personalUser("A"), "A", "cli_test", "open-A")
	require.NoError(t, err)
	require.Equal(t, "new-access", token)
	require.Equal(t, 1, refreshes)
	_, err = s.UserToken(personalUser("B"), "A", "cli_test", "open-A")
	require.Error(t, err)
	require.Equal(t, 1, refreshes)
}

func TestFeishuRemoteFailureSeparatesScopeTokenAndExportStages(t *testing.T) {
	for operation, want := range map[string]string{
		"create_export_task":     "export_create_denied",
		"get_export_task_status": "export_status_denied", "download_export_file": "export_download_denied",
		"resolve_personal_resource": "source_metadata_denied",
	} {
		err := &core.APIError{
			Operation:  operation,
			Category:   core.ErrorCategoryPermission,
			HTTPStatus: 403,
			Code:       1069902,
		}
		require.Equal(t, want, feishuRemoteFailureCategory(err))
	}
	require.Equal(
		t,
		"app_scope_missing",
		feishuRemoteFailureCategory(&core.APIError{Category: core.ErrorCategoryAppScope}),
	)
	require.Equal(
		t,
		"user_scope_missing",
		feishuRemoteFailureCategory(&core.APIError{Category: core.ErrorCategoryUserScope}),
	)
	require.Equal(t, "refresh_token_expired", feishuRemoteFailureCategory(ErrFeishuRefreshExpired))
	require.Equal(t, "refresh_rejected", feishuRemoteFailureCategory(ErrFeishuRefreshRejected))
	require.Equal(t, "oauth_app_configuration", feishuRemoteFailureCategory(ErrFeishuOAuthConfiguration))
	policy := errors.Join(core.ErrPersonalBlocksPolicyUnverified,
		&core.APIError{Category: core.ErrorCategoryPermission})
	require.Equal(t, "source_policy_verification_required", feishuRemoteFailureCategory(policy))
	require.Equal(t, "source_policy_verification_required", feishuV2Error(policy))
	require.Equal(t, "blocks_content_requires_review", feishuV2Error(core.ErrPersonalBlocksIncomplete))
}

func TestFeishuOAuthRefreshFailuresKeepSafeRequestEvidence(t *testing.T) {
	for _, tc := range []struct {
		body     string
		status   int
		want     error
		category core.ErrorCategory
	}{
		{
			`{"error":"invalid_grant","error_description":"sensitive"}`,
			400, ErrFeishuRefreshRejected, core.ErrorCategoryAuth,
		},
		{`{"error":"invalid_client"}`, 400, ErrFeishuOAuthConfiguration, core.ErrorCategoryAuth},
		{`upstream unavailable`, 503, core.ErrTransient, core.ErrorCategoryTransient},
		{`rate limited`, 429, core.ErrRateLimited, core.ErrorCategoryRateLimit},
	} {
		s, _ := feishuFixture(t)
		s.api = &http.Client{Transport: feishuTestTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: tc.status,
				Header:     http.Header{"X-Tt-Logid": []string{"opaque-test-id"}},
				Body:       io.NopCloser(strings.NewReader(tc.body)),
			}, nil
		})}
		var evidence []core.APIObservation
		ctx := core.WithAPIObserver(
			context.Background(),
			func(e core.APIObservation) { evidence = append(evidence, e) },
		)
		_, err := s.oauthRequest(ctx, map[string]string{"grant_type": "refresh_token"})
		require.True(t, errors.Is(err, tc.want), "%v", err)
		require.NotContains(t, err.Error(), "sensitive")
		require.Len(t, evidence, 1)
		require.Equal(t, "refresh_user_token", evidence[0].Operation)
		require.Equal(t, tc.category, evidence[0].Category)
		require.Equal(t, tc.status, evidence[0].HTTPStatus)
		require.NotEmpty(t, evidence[0].RequestIDHash)
	}
}
