package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFeishuSourceWriteAccessUsesSpaceWriterAndKBPermissions(t *testing.T) {
	for _, tc := range []struct {
		name, kind, oldKind, owner string
		role                      types.TenantRole
		tenant                    uint64
		update                    bool
		status                    int
	}{
		{name: "wiki_writer", kind: "feishu", role: "contributor", tenant: 1, status: 204},
		{name: "drive_writer", kind: "feishu_drive", role: "contributor", tenant: 1, status: 204},
		{name: "viewer", kind: "feishu", role: "viewer", tenant: 1, status: 403},
		{name: "other_connector", kind: "gitlab", role: "contributor", tenant: 1, status: 403},
		{name: "other_connector_admin", kind: "gitlab", role: "admin", tenant: 1, status: 204},
		{name: "other_space", kind: "feishu", role: "contributor", tenant: 2, status: 403},
		{name: "private_owner", kind: "feishu", role: "contributor", tenant: 1, owner: "writer", status: 204},
		{name: "other_private_owner", kind: "feishu", role: "admin", tenant: 1, owner: "other", status: 403},
		{
			name: "update_body_preserved",
			kind: "feishu",
			oldKind: "feishu",
			role: "contributor",
			tenant: 1,
			update: true,
			status: 204
		},
		{
			name: "convert_to_admin_connector",
			kind: "gitlab",
			oldKind: "feishu",
			role: "contributor",
			tenant: 1,
			update: true,
			status: 403
		},
		{
			name: "take_admin_connector",
			kind: "feishu",
			oldKind: "gitlab",
			role: "contributor",
			tenant: 1,
			update: true,
			status: 403
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kb := &types.KnowledgeBase{ID: "kb", TenantID: tc.tenant, PrivateOwnerID: tc.owner}
			h := NewDataSourceHandler(&stubDataSourceService{
				getDataSource: func(context.Context, string) (*types.DataSource, error) {
					return &types.DataSource{ID: "source", TenantID: 1, KnowledgeBaseID: "kb", Type: tc.oldKind}, nil
				},
			}, &stubKBServiceForDS{
				getByID: func(context.Context, string) (*types.KnowledgeBase, error) { return kb, nil },
			})
			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.Use(func(c *gin.Context) { c.Set(types.TenantIDContextKey.String(), uint64(1)) })
			method, path, target := http.MethodPost, "/api/v1/datasource", "/api/v1/datasource"
			if tc.update {
				method, path, target = http.MethodPut, "/api/v1/datasource/:id", "/api/v1/datasource/source"
			}
			called := false
			r.Handle(method, path, h.SourceWriteAccess, func(c *gin.Context) {
				called = true
				var body types.DataSource
				require.NoError(t, c.ShouldBindJSON(&body))
				require.Equal(t, tc.kind, body.Type)
				require.Equal(t, "kb", body.KnowledgeBaseID)
				require.NoError(t, access.RequireKBWrite(c.Request.Context(), kb))
				c.Status(http.StatusNoContent)
			})
			request := httptest.NewRequest(method, target, strings.NewReader(
				fmt.Sprintf(`{"type":%q,"knowledge_base_id":"kb","config":{"settings":{}}}`, tc.kind)))
			request.Header.Set("Content-Type", "application/json")
			ctx := types.WithCaller(request.Context(), types.Caller{TenantID: 1, UserID: "writer", Role: tc.role})
			request = request.WithContext(ctx)
			response := httptest.NewRecorder()
			r.ServeHTTP(response, request)
			require.Equal(t, tc.status, response.Code, response.Body.String())
			require.Equal(t, tc.status == http.StatusNoContent, called)
		})
	}
}
