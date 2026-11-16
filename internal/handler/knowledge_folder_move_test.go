package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type folderMetadataMoveService struct {
	interfaces.KnowledgeService
	called bool
}

func (s *folderMetadataMoveService) GetKnowledgeBatch(
	context.Context, uint64, []string,
) ([]*types.Knowledge, error) {
	return nil, types.ErrLegacySourceRestricted
}

func (s *folderMetadataMoveService) MoveKnowledgeToFolder(
	ctx context.Context, kb string, ids []string, folder string,
) (int64, error) {
	s.called = kb == "kb-1" && len(ids) == 1 && ids[0] == "doc" && folder == "target"
	return 1, nil
}

func TestFolderMoveHandlerDoesNotPreflightSourceContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/knowledge/folder",
		strings.NewReader(`{"kb_id":"kb-1","knowledge_ids":["doc"],"folder_path":"target"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, uint64(1))
	ctx = context.WithValue(ctx, types.TenantRoleContextKey, types.TenantRoleOwner)
	ctx = context.WithValue(ctx, types.UserIDContextKey, "owner")
	c.Request = c.Request.WithContext(ctx)
	svc := &folderMetadataMoveService{}
	h := &KnowledgeHandler{kbService: &stubFolderKBService{}, kgService: svc}
	h.MoveKnowledgeToFolder(c)
	require.Empty(t, c.Errors)
	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, svc.called)
	require.Contains(t, w.Body.String(), `"moved_count":1`)
}
