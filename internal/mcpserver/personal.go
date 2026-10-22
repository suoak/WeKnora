package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// PersonalServer uses the existing Go MCP library/tool schemas, but never the
// workspace machine principal, endpoint scope, ask or write tool handlers.
type PersonalServer struct {
	enabled   bool
	knowledge *service.PortalMCPKnowledge
	handler   http.Handler
	slots     chan struct{}
}

func NewDisabledPersonalServer() *PersonalServer { return NewPersonalServer(false, nil) }
func (s *PersonalServer) Enabled() bool          { return s != nil && s.enabled && s.knowledge != nil }
func NewPersonalServer(enabled bool, knowledge *service.PortalMCPKnowledge) *PersonalServer {
	s := &PersonalServer{enabled: enabled, knowledge: knowledge, slots: make(chan struct{}, 8)}
	m := server.NewMCPServer("knowhub-personal", "0.1.0", server.WithToolCapabilities(false), server.WithRecovery(), server.WithToolHandlerMiddleware(s.guard))
	for _, tool := range []mcp.Tool{listKnowledgeBasesTool(), searchKnowledgeTool(), grepChunksTool(), listDocumentsTool(), readDocumentTool(), wikiSearchTool(), wikiReadPageTool(), wikiIndexTool()} {
		if tool.Name == "wiki_search" {
			tool.InputSchema.Properties["knowledge_base_ids"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional exact authorized KB IDs; omit to search all personal wikis."}
			tool.Description = "Search verified published Wiki pages across the current personal knowledge scope using bounded literal text. Unknown or revoked sources are omitted with a partial warning."
		}
		if tool.Name == "wiki_index" {
			tool.Description = "Read a verified Wiki directory without creating an index page. Results are bounded; truncation and unavailable provenance are explicit."
		}
		if strings.HasPrefix(tool.Name, "wiki_") {
			tool.InputSchema.Properties["offset"] = map[string]any{"type": "number", "description": "Optional result offset (0..1000); large scopes may be explicitly truncated."}
		}
		m.AddTool(tool, s.call)
	}
	// Wiki reads require a persisted, version-bound trusted generation proof.
	s.handler = server.NewStreamableHTTPServer(m, server.WithStateLess(true))
	return s
}
func (s *PersonalServer) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s == nil || !s.enabled || s.knowledge == nil {
			http.Error(w, "personal MCP disabled", http.StatusForbidden)
			return
		}
		select {
		case s.slots <- struct{}{}:
			defer func() { <-s.slots }()
		default:
			w.Header().Set("Retry-After", "1")
			w.Header().Set("Cache-Control", "private, no-store")
			http.Error(w, "personal MCP busy; retry later", http.StatusTooManyRequests)
			return
		}
		bounded, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		ctx, _ := access.WithPortalDiagnostics(bounded)
		r = r.WithContext(ctx)
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if _, err := s.knowledge.Resolve(r.Context(), nil); err != nil {
			http.Error(w, "personal MCP denied", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		s.handler.ServeHTTP(w, r)
	})
}
func (s *PersonalServer) guard(next server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if s == nil || !s.enabled || s.knowledge == nil {
			return mcp.NewToolResultError("personal MCP disabled"), nil
		}
		ctx, diagnostics := access.WithPortalDiagnostics(ctx)
		switch r.Params.Name {
		case "list_knowledge_bases", "search_knowledge", "grep_chunks", "list_documents", "read_document", "wiki_search", "wiki_read_page", "wiki_index":
		default:
			return mcp.NewToolResultError("tool not allowed"), nil
		}
		// Only these eight read-only operations may be retried. A source revoked
		// during execution discards the entire first response, then resolves a
		// fresh scope once. Identity/DB/unknown errors never enter this retry.
		for attempt := 0; attempt < 2; attempt++ {
			targets, err := s.knowledge.Resolve(ctx, nil)
			if err != nil {
				return mcp.NewToolResultError("authorization denied"), nil
			}
			result, err := next(ctx, r)
			if err != nil {
				return nil, err
			}
			retry := false
			for _, t := range targets {
				if e := t.Revalidate(ctx); e != nil {
					if attempt == 0 && errors.Is(e, types.ErrLegacySourceRestricted) {
						retry = true
						continue
					}
					return mcp.NewToolResultError("authorization changed"), nil
				}
			}
			if retry {
				types.DiscardLegacyReadAttempt(ctx)
				continue
			}
			if result != nil && !result.IsError && diagnostics.Partial() {
				result.Content = append(result.Content, mcp.NewTextContent(access.PortalPartialWarning))
			}
			return result, nil
		}
		return mcp.NewToolResultError("authorization changed"), nil
	}
}
func personalJSON(value any) (*mcp.CallToolResult, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(string(b)), nil
}
func (s *PersonalServer) call(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	fail := func() (*mcp.CallToolResult, error) {
		return mcp.NewToolResultError("resource denied or unavailable"), nil
	}
	switch r.Params.Name {
	case "list_knowledge_bases":
		ts, e := s.knowledge.Resolve(ctx, nil)
		if e != nil {
			return fail()
		}
		rows := []map[string]any{}
		for _, t := range ts {
			d := t.Descriptor()
			rows = append(rows, map[string]any{"id": d.ID, "tenant_id": d.TenantID, "name": d.Name, "space": d.SpaceName, "public": t.Public(), "capabilities": []string{"keyword", "semantic", "hybrid"}})
		}
		return personalJSON(rows)
	case "list_documents":
		rows, e := s.knowledge.ListDocuments(ctx, r.GetString("knowledge_base_id", ""))
		if e != nil {
			return fail()
		}
		if keyword := strings.ToLower(r.GetString("keyword", "")); keyword != "" {
			filtered := []service.PortalMCPHit{}
			for _, row := range rows {
				if strings.Contains(strings.ToLower(row.Title), keyword) {
					filtered = append(filtered, row)
				}
			}
			rows = filtered
		}
		size := r.GetInt("page_size", 20)
		page := r.GetInt("page", 1)
		if page < 1 || page > 10000 || size < 1 || size > 100 {
			return fail()
		}
		start := (page - 1) * size
		if start > len(rows) {
			start = len(rows)
		}
		end := start + size
		if end > len(rows) {
			end = len(rows)
		}
		return personalJSON(map[string]any{"total": len(rows), "documents": rows[start:end]})
	case "read_document":
		rows, e := s.knowledge.ReadDocument(ctx, r.GetString("knowledge_id", ""))
		if e != nil {
			return fail()
		}
		if query := strings.ToLower(r.GetString("query", "")); query != "" {
			keep := map[int]bool{}
			for i, row := range rows {
				if strings.Contains(strings.ToLower(row.Text), query) {
					keep[i] = true
					if i > 0 {
						keep[i-1] = true
					}
					if i+1 < len(rows) {
						keep[i+1] = true
					}
				}
			}
			filtered := []service.PortalMCPHit{}
			for i, row := range rows {
				if keep[i] {
					filtered = append(filtered, row)
				}
			}
			rows = filtered
		}
		offset := r.GetInt("offset", 0)
		limit := r.GetInt("limit", 20)
		if offset < 0 || limit < 1 || limit > 100 {
			return fail()
		}
		if offset > len(rows) {
			offset = len(rows)
		}
		end := offset + limit
		if end > len(rows) {
			end = len(rows)
		}
		return personalJSON(map[string]any{"total": len(rows), "chunks": rows[offset:end]})
	case "wiki_search", "wiki_read_page", "wiki_index":
		if _, present := r.GetArguments()["regex"]; present && r.GetBool("regex", false) {
			return mcp.NewToolResultError("Personal wiki_search supports bounded literal text; regex is not available"), nil
		}
		kb := r.GetString("knowledge_base_id", "")
		ids := r.GetStringSlice("knowledge_base_ids", nil)
		rows, e := s.knowledge.PersonalWiki(ctx, r.Params.Name, r.GetString("query", ""), kb, r.GetString("slug", ""), r.GetInt("limit", 50), r.GetInt("offset", 0), ids...)
		if e != nil {
			return fail()
		}
		return personalJSON(rows)
	case "search_knowledge", "grep_chunks":
		ids := r.GetStringSlice("knowledge_base_ids", nil)
		mode := r.GetString("mode", "hybrid")
		if r.Params.Name == "grep_chunks" {
			mode = "grep"
		}
		rows, e := s.knowledge.Search(ctx, ids, r.GetString("query", ""), mode, r.GetInt("limit", 10))
		if e != nil {
			return fail()
		}
		return personalJSON(rows)
	}
	return fail()
}
