package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestQuickImportMissingTargetNeverInvokesService(t *testing.T) {
	for _, body := range []string{
		`{"links":["https://test.feishu.cn/docx/allowed"]}`,
		`{"kb_id":"  ","links":["https://test.feishu.cn/docx/allowed"]}`,
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/me/feishu/imports", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		// A nil service proves rejection precedes any possible KB creation.
		(&FeishuPersonalHandler{}).QuickImport(c)
		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Contains(t, w.Body.String(), "feishu_target_knowledge_base_required")
	}
}
