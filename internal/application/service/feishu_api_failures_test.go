package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/stretchr/testify/require"
)

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
}

func TestFeishuOAuthRefreshFailuresKeepSafeRequestEvidence(t *testing.T) {
	for _, tc := range []struct {
		body     string
		status   int
		want     error
		category core.ErrorCategory
	}{
		{`{"error":"invalid_grant","error_description":"sensitive"}`, 400, ErrFeishuRefreshRejected, core.ErrorCategoryAuth},
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
