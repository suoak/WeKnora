//go:build sqlite_fts5

package router

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Called only from the existing disposable-PostgreSQL authenticated Go MCP
// fixture. Both entrances use real persisted Personal Keys and normal auth.
func verifyPersonalMetadataProtocol(t *testing.T, db *gorm.DB, handler *gin.Engine, key string) {
	t.Helper()
	call := func(path, name string, args any) (int, mcp.CallToolResult) {
		body, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/call",
			"params":  map[string]any{"name": name, "arguments": args},
		})
		r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+key)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		var envelope struct {
			Result mcp.CallToolResult `json:"result"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &envelope)
		return w.Code, envelope.Result
	}
	decode := func(result mcp.CallToolResult, value any) {
		t.Helper()
		require.False(t, result.IsError, "%+v", result.Content)
		require.NotEmpty(t, result.Content)
		require.NoError(t, json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), value))
	}
	paths := []string{"/api/v1/mcp/personal", "/internal/mcp/isolated/personal"}
	var reference []map[string]any
	for _, path := range paths {
		status, result := call(path, "list_knowledge_bases", map[string]any{})
		require.Equal(t, http.StatusOK, status)
		var rows []map[string]any
		decode(result, &rows)
		require.NotEmpty(t, rows)
		if reference == nil {
			reference = rows
		} else {
			require.Equal(t, reference, rows, "identical read contract across entrances")
		}
		for _, row := range rows {
			for _, field := range []string{
				"id",
				"tenant_id",
				"name",
				"space",
				"public",
				"capabilities",
				"searchable",
				"description",
				"creator_name",
				"created_at",
				"updated_at",
				"document_count",
				"type",
			} {
				require.Contains(t, row, field)
			}
			require.NotContains(t, row, "creator_id")
			require.NotContains(t, row, "email")
			var authoritative types.KnowledgeBase
			require.NoError(t, db.Where("id=?", row["id"]).First(&authoritative).Error)
			require.Equal(t, authoritative.Description, row["description"])
			require.Equal(t, authoritative.CreatedAt.Format("2006-01-02T15:04:05.999999999Z07:00"), row["created_at"])
			require.Equal(t, authoritative.UpdatedAt.Format("2006-01-02T15:04:05.999999999Z07:00"), row["updated_at"])
			status, result = call(path, "get_knowledge_base", map[string]any{"knowledge_base_id": row["id"]})
			require.Equal(t, http.StatusOK, status)
			var detail map[string]any
			decode(result, &detail)
			require.Equal(t, row, detail)
			status, result = call(path, "list_documents", map[string]any{
				"knowledge_base_id": row["id"],
				"page":              1,
				"page_size":         1,
			})
			require.Equal(t, http.StatusOK, status)
			var page struct {
				Total     int
				Documents []map[string]any
			}
			decode(result, &page)
			require.EqualValues(t, row["document_count"], page.Total)
			require.LessOrEqual(t, len(page.Documents), 1)
			for _, document := range page.Documents {
				for _, field := range []string{
					"knowledge_id",
					"knowledge_base_id",
					"title",
					"file_name",
					"file_type",
					"type",
					"parse_status",
					"created_at",
					"updated_at",
				} {
					require.Contains(t, document, field)
				}
			}
		}
		for _, id := range []string{"private-kb", "nonexistent", "PUBLIC_BATCH"} {
			status, result = call(path, "get_knowledge_base", map[string]any{"knowledge_base_id": id})
			require.True(t, status >= 400 || result.IsError, "unauthorized selector %q must fail", id)
		}
	}
	// Scope and metadata must remain separate under concurrent HTTP calls.
	passed := make(chan bool, 4)
	for i := 0; i < 4; i++ {
		go func(i int) {
			status, result := call(paths[i%2], "get_knowledge_base", map[string]any{"knowledge_base_id": "public-kb"})
			passed <- status == http.StatusOK && !result.IsError
		}(i)
	}
	for i := 0; i < 4; i++ {
		require.True(t, <-passed)
	}
	status, result := call(paths[1], "add_document", map[string]any{
		"knowledge_base_id": "public-kb",
		"content":           "MUST_NOT_WRITE",
	})
	require.True(t, status >= 400 || result.IsError)
}
