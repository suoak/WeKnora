package handler

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFeishuFailurePreservesAuthorizationAndDuplicateContract(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		http int
		code string
	}{
		{"unauthenticated", access.ErrUnauthorized, 401, "authentication_required"},
		{"forbidden", access.ErrForbidden, 403, "feishu_kb_write_forbidden"},
		{"missing", access.ErrNotFound, 404, "resource_not_found"},
		{"duplicate", repository.ErrPersonalAlreadyImported, 400, "feishu_already_imported"},
		{"internal", errors.New("private database detail"), 400, "feishu_operation_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			feishuFailureResponse(c, errors.Join(errors.New("internal detail"), tc.err))
			require.Equal(t, tc.http, w.Code)
			var body map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			require.Equal(t, tc.code, body["error"])
			require.NotContains(t, w.Body.String(), "internal detail")
			require.NotContains(t, w.Body.String(), "private database detail")
			require.NotContains(t, w.Body.String(), "knowledge_id")
			require.NotContains(t, w.Body.String(), "imported_by")
		})
	}
}
