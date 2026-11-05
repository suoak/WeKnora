package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type processingMetadataLookup struct{}

func (*processingMetadataLookup) GetKnowledgeByIDOnly(context.Context, string) (*types.Knowledge, error) {
	return nil, types.ErrLegacySourceRestricted
}

func (*processingMetadataLookup) GetKnowledgeProcessingMetadata(context.Context, string) (*types.Knowledge, error) {
	return &types.Knowledge{ID: "doc", TenantID: 1, KnowledgeBaseID: "kb"}, nil
}

func TestProcessingMetadataScopeOnlyExistsOnRegisteredStatusGET(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		method, path string
		scoped       bool
	}{
		{http.MethodGet, "/knowledge/:id/spans", true},
		{http.MethodGet, "/knowledge/:id/stages", true},
		{http.MethodGet, "/knowledge/:id", false},
		{http.MethodPost, "/knowledge/:id/spans", false},
	} {
		r := gin.New()
		called := false
		observe := func(c *gin.Context) {
			called = true
			ctx := c.Request.Context()
			require.Equal(t, test.scoped, types.IsKnowledgeProcessingMetadata(ctx, "doc"))
			require.False(t, types.IsKnowledgeProcessingMetadata(ctx, "other"))
			require.False(t, types.IsKnowledgeDeletionMetadata(ctx, "doc"))
			c.Status(http.StatusOK)
		}
		r.Handle(test.method, test.path, KnowledgeProcessingMetadata(&processingMetadataLookup{}), observe)
		path := "/knowledge/doc"
		if test.path != "/knowledge/:id" {
			path += test.path[len("/knowledge/:id"):]
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(test.method, path, nil))
		require.Equal(t, http.StatusOK, w.Code)
		require.True(t, called)
	}
}
