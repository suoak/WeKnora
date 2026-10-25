package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestFeishuV2CandidateFailurePolicy(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		category string
		retry    bool
	}{
		{
			"storage permission", &os.PathError{Op: "mkdir", Path: "private", Err: os.ErrPermission},
			"storage_permission_denied", false,
		},
		{
			"storage missing", &os.PathError{Op: "open", Path: "private", Err: os.ErrNotExist},
			"storage_unavailable", false,
		},
		{"timeout", context.DeadlineExceeded, "temporary_failure", true},
		{"rate limit", core.ErrRateLimited, "temporary_failure", true},
		{"temporary API", core.ErrTransient, "temporary_failure", true},
		{"worker stop", context.Canceled, "worker_interrupted", true},
		{"version change", errors.New("personal_resource_revision_changed"), "source_version_changed", false},
		{"unknown", errors.New("permanent failure"), "candidate_requires_review", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			category, retry := personalCandidateFailure(tc.err)
			require.Equal(t, tc.category, category)
			require.Equal(t, tc.retry, retry)
		})
	}
}

func TestFeishuV2PostgresTemporaryCandidateFailureHasFiniteRetries(t *testing.T) {
	f := newMVPFixture(t)
	m := f.prepare("https://tenant.feishu.cn/file/real")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/open-apis/drive/v1/metas/batch_query" {
			_, _ = io.WriteString(
				w,
				`{"code":0,"data":{"metas":[{"doc_type":"file","doc_token":"real",`+
					`"title":"test.pdf","latest_modify_time":"1"}]}}`,
			)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	f.s.newClient = func(cfg *core.Config) *core.Client { cfg.BaseURL = server.URL; return core.NewClient(cfg) }
	for i := 1; i <= 4; i++ {
		require.Error(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
		var attempt types.FeishuPersonalAttempt
		require.NoError(t, f.db.First(&attempt, "id = ?", m.AttemptID).Error)
		require.Equal(t, i, attempt.RetryCount)
		if i <= 3 {
			require.Equal(t, "recovery", attempt.Status)
			require.Equal(t, "temporary_failure", attempt.LastErrorCategory)
			require.NotNil(t, attempt.RetryAfter)
			require.NoError(
				t,
				f.db.Model(&attempt).UpdateColumn("retry_after", time.Now().Add(-time.Second)).Error,
			)
		} else {
			require.Equal(t, "needs_attention", attempt.Status)
			require.Equal(t, "retry_exhausted", attempt.LastErrorCategory)
			require.Nil(t, attempt.RetryAfter)
		}
	}
}

func TestFeishuV2PostgresStorageFailureTerminatesOriginalAttempt(t *testing.T) {
	f := newMVPFixture(t)
	m := f.prepare("https://tenant.feishu.cn/file/real")
	blockedRoot := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(blockedRoot, []byte("blocking file"), 0o600))
	f.p.store.root = blockedRoot
	require.Error(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
	var attempt types.FeishuPersonalAttempt
	require.NoError(t, f.db.First(&attempt, "id = ?", m.AttemptID).Error)
	require.Equal(t, "needs_attention", attempt.Status)
	require.Equal(t, "storage_unavailable", attempt.LastErrorCategory)
	require.Nil(t, attempt.RetryAfter)
	require.Equal(t, 1, attempt.RetryCount)
	var imported types.FeishuPersonalImport
	require.NoError(t, f.db.First(&imported, "id = ?", m.ID).Error)
	require.Equal(t, "needs_attention", imported.Status)
	require.Empty(t, imported.KnowledgeID)
}
