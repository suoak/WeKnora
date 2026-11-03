package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type deletionMetadataLookup struct{ denied bool }

func (*deletionMetadataLookup) GetKnowledgeByIDOnly(context.Context, string) (*types.Knowledge, error) {
	return nil, types.ErrLegacySourceRestricted
}

func (l *deletionMetadataLookup) GetKnowledgeDeletionMetadata(
	ctx context.Context, id string,
) (*types.Knowledge, error) {
	if l.denied || !types.IsKnowledgeDeletionMetadata(ctx, id) {
		return nil, apperrors.NewForbiddenError("private owner required")
	}
	return &types.Knowledge{ID: id, TenantID: 1, KnowledgeBaseID: "kb"}, nil
}

func TestKnowledgeDeletionMetadataKeepsGuardsAndExactScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, denied := range []bool{false, true} {
		lookup := &deletionMetadataLookup{denied: denied}
		r := gin.New()
		r.Use(ErrorHandler())
		guards, handlers := 0, 0
		r.DELETE("/knowledge/:id", KnowledgeDeletionMetadata(lookup), func(c *gin.Context) {
			guards++
			require.True(t, types.IsKnowledgeDeletionMetadata(c.Request.Context(), "doc"))
			require.False(t, types.IsKnowledgeDeletionMetadata(c.Request.Context(), "other"))
			require.True(t, types.IsKBDeletionMetadata(c.Request.Context(), "kb"))
			c.Next()
		}, func(c *gin.Context) { handlers++; c.Status(http.StatusOK) })
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/knowledge/doc", nil))
		if denied {
			require.Equal(t, http.StatusForbidden, w.Code)
			require.Zero(t, guards)
			require.Zero(t, handlers)
		} else {
			require.Equal(t, http.StatusOK, w.Code)
			require.Equal(t, 1, guards)
			require.Equal(t, 1, handlers)
		}
	}
}
