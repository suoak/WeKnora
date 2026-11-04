package handler

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSteadyManualSyncErrorsAreSpecificAndNeverExposeProviderDetails(t *testing.T) {
	for _, test := range []struct {
		reason, message string
		status          int
	}{
		{"sync_source_paused", "数据源已暂停", 409},
		{"sync_source_protected", "数据源暂不可同步，请联系管理员", 409},
		{"sync_policy_expired", "飞书同步策略已到期，请联系管理员恢复", 409},
		{"sync_source_not_approved", "该来源尚未启用同步，请联系管理员", 409},
		{"sync_platform_stopped", "飞书同步服务暂时停止，管理员正在恢复", 503},
		{"sync_source_permission_denied", "当前账号无权同步此来源", 403},
	} {
		t.Run(test.reason, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			err := &datasource.SyncAdmissionError{
				Reason: test.reason, Cause: errors.New("private-provider-token-url-marker"),
			}
			require.True(t, writeSyncAdmissionFailure(c, err))
			require.Equal(t, test.status, w.Code)
			require.Contains(t, w.Body.String(), test.message)
			require.Contains(t, w.Body.String(), test.reason)
			require.NotContains(t, w.Body.String(), "private-provider")
		})
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	require.True(t, writeSyncAdmissionFailure(c, core.ErrAuth))
	require.Equal(t, 403, w.Code)
	require.Contains(t, w.Body.String(), "飞书授权失效，请重新授权")
}

func TestProtectedSourceRecoveryInstructionsPreserveErrorCode(t *testing.T) {
	for _, tc := range []struct{ state, action, message string }{
		{"hold", "reconcile_source", "数据源已保存"},
		{"revoked", "reauthorize_source", "来源授权已撤销"},
	} {
		t.Run(tc.state, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			require.True(t, writeSyncAdmissionFailure(c, &datasource.SyncAdmissionError{
				Reason: "sync_source_protected", ProtectionState: tc.state,
				Cause: errors.New("private-provider-marker"),
			}))
			require.Equal(t, 409, w.Code)
			require.Contains(t, w.Body.String(), tc.message)
			require.Contains(t, w.Body.String(), `"code":"sync_source_protected"`)
			require.Contains(t, w.Body.String(), `"recovery_action":"`+tc.action+`"`)
			require.NotContains(t, w.Body.String(), "private-provider-marker")
		})
	}
}
