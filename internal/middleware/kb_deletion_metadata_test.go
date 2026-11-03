package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestKBDeletionMetadataPreservesCanonicalWriteGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tenant := range []uint64{1, 2} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Params = gin.Params{{Key: "id", Value: "kb"}}
		c.Request = httptest.NewRequest(http.MethodDelete, "/knowledge-bases/kb", nil)
		c.Request = c.Request.WithContext(types.WithCaller(c.Request.Context(), types.Caller{
			TenantID: tenant, UserID: "user", Role: types.TenantRoleAdmin,
		}))
		KBDeletionMetadata()(c)
		require.True(t, types.IsKBDeletionMetadata(c.Request.Context(), "kb"))
		RequireKBAccess(KBIDFromParam("id"), types.OrgRoleEditor,
			&stubKBLookup{kbs: map[string]*types.KnowledgeBase{"kb": {ID: "kb", TenantID: 1}}},
			nil, nil, &config.Config{})(c)
		require.Equal(t, tenant != 1, c.IsAborted(), "metadata never grants cross-space writes")
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Params = gin.Params{{Key: "id", Value: "kb"}}
	c.Request = httptest.NewRequest(http.MethodGet, "/knowledge-bases/kb", nil)
	KBDeletionMetadata()(c)
	require.False(t, types.IsKBDeletionMetadata(c.Request.Context(), "kb"))
}
