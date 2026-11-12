package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/perftrace"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// PersonalServer uses the existing Go MCP library/tool schemas, but never the
// workspace machine principal, endpoint scope, ask or write tool handlers.
type PersonalServer struct {
	usage     usageRecorder
	enabled   bool
	knowledge *service.PortalMCPKnowledge
	handler   http.Handler
	admission *personalAdmission
	protocol  *personalAdmission
	streams   *personalAdmission
}

func NewDisabledPersonalServer() *PersonalServer { return NewPersonalServer(false, nil) }
func (s *PersonalServer) Enabled() bool          { return s != nil && s.enabled && s.knowledge != nil }

func NewPersonalServer(enabled bool, knowledge *service.PortalMCPKnowledge) *PersonalServer {
	limits, _ := (config.PersonalMCPConcurrencyConfig{}).Resolve()
	return newPersonalServer(enabled, knowledge, limits)
}

func newPersonalServer(
	enabled bool, knowledge *service.PortalMCPKnowledge, limits config.PersonalMCPConcurrencyConfig,
) *PersonalServer {
	s := &PersonalServer{
		enabled: enabled, knowledge: knowledge,
		admission: newPersonalAdmission(limits, false), protocol: newPersonalAdmission(limits, true),
		streams: newPersonalStreamAdmission(limits),
	}
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
	var sink usageRecorder
	if s != nil {
		sink = s.usage
	}
	h := observeHTTP(sink, "go_personal", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s == nil || !s.enabled || s.knowledge == nil {
			http.Error(w, "personal MCP disabled", http.StatusForbidden)
			return
		}
		// Only the server-authenticated owner participates in scheduling. Keys and
		// workspace selections belonging to the same person share one quota.
		principal, ok := r.Context().Value(types.PrincipalContextKey).(types.Principal)
		if !ok || principal.Type != types.PrincipalWebUser || !principal.Valid() {
			http.Error(w, "personal MCP authentication required", http.StatusUnauthorized)
			return
		}
		perftrace.Authenticated(r.Context())
		bounded, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer func() {
			captureUsageContextError(bounded)
			cancel()
		}()
		ctx, _ := access.WithPortalDiagnostics(bounded)
		r = r.WithContext(ctx)
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var targets []string
		pool, class := s.protocol, "protocol"
		if r.Method == http.MethodGet {
			pool, class = s.streams, "stream"
		}
		if r.Method == http.MethodPost {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "invalid personal MCP request", http.StatusBadRequest)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			targets, err = personalHTTPRequestTargets(body)
			if err != nil {
				http.Error(w, "invalid personal MCP targets", http.StatusBadRequest)
				return
			}
			var envelope struct {
				Method string `json:"method"`
			}
			_ = json.Unmarshal(body, &envelope) // Already validated by personalHTTPRequestTargets.
			if envelope.Method == "tools/call" {
				pool, class = s.admission, "tool"
			}
		}
		waitStarted := time.Now()
		release, err := pool.acquire(r.Context(), principal.ID)
		outcome := "admitted"
		if err != nil {
			outcome = "concurrency_limited"
			if !errors.Is(err, errPersonalCapacity) {
				outcome = "canceled"
			}
		}
		perftrace.Admission(r.Context(), class, outcome, time.Since(waitStarted), pool.snapshot(principal.ID))
		if err != nil {
			if errors.Is(err, errPersonalCapacity) {
				if v, ok := r.Context().Value(invocationKey{}).(*invocation); ok {
					v.concurrencyLimited = true
				}
				// Queue completion depends on downstream latency; retry no earlier
				// than the configured waiting interval, rounded up to whole seconds.
				w.Header().Set("Retry-After", strconv.Itoa(int((pool.wait+time.Second-1)/time.Second)))
				w.Header().Set("Cache-Control", "private, no-store")
				w.Header().Set("X-KnowHub-Error-Code", "concurrency_limited")
				http.Error(w, "personal MCP concurrency capacity exceeded; retry later", http.StatusTooManyRequests)
			} else if errors.Is(err, context.DeadlineExceeded) {
				http.Error(w, "personal MCP request timed out", http.StatusGatewayTimeout)
			} else {
				http.Error(w, "personal MCP request canceled", http.StatusRequestTimeout)
			}
			return
		}
		defer func() {
			release()
			perftrace.AdmissionReleased(r.Context(), pool.snapshot(principal.ID))
		}()
		// Connected SSE listeners are idle most of the time. Their initial live
		// permission resolution still uses the short-operation budget, so a large
		// listener limit cannot fan out the same number of expensive DB reads.
		if r.Method == http.MethodGet {
			releaseScope, err := s.protocol.acquire(r.Context(), principal.ID)
			if err != nil {
				if errors.Is(err, errPersonalCapacity) {
					perftrace.Admission(r.Context(), "stream", "concurrency_limited",
						time.Since(waitStarted), s.protocol.snapshot(principal.ID))
					w.Header().Set("Retry-After", strconv.Itoa(int((s.protocol.wait+time.Second-1)/time.Second)))
					w.Header().Set("Cache-Control", "private, no-store")
					w.Header().Set("X-KnowHub-Error-Code", "concurrency_limited")
					http.Error(w, "personal MCP concurrency capacity exceeded; retry later", http.StatusTooManyRequests)
				} else {
					http.Error(w, "personal MCP request canceled", http.StatusRequestTimeout)
				}
				return
			}
			err = func() error {
				defer releaseScope()
				_, resolveErr := s.knowledge.Resolve(r.Context(), targets)
				return resolveErr
			}()
			if err != nil {
				http.Error(w, "personal MCP denied", http.StatusForbidden)
				return
			}
		} else if _, err := s.knowledge.Resolve(r.Context(), targets); err != nil {
			http.Error(w, "personal MCP denied", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		s.handler.ServeHTTP(w, r)
	}))
	return h
}

// IsolatedHandler is the strict read-only route used only by the dedicated
// TLS proxy. It shares the original server, identity and knowledge service.
func (s *PersonalServer) IsolatedHandler() http.Handler {
	return personalHTTPBoundary(s.Handler(), isolatedPersonalPath)
}

func (s *PersonalServer) guard(next server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		h := observeTool(s.usage, "go_personal", s.enforceGuard(next))
		if logger.IsolatedPersonalPayload(ctx) {
			return personalToolBoundary(h)(ctx, req)
		}
		return h(ctx, req)
	}
}

func (s *PersonalServer) enforceGuard(next server.ToolHandlerFunc) server.ToolHandlerFunc {
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
		ids, err := personalOperationTargets(r.Params.Name, r.GetArguments())
		if err != nil {
			return mcp.NewToolResultError("invalid knowledge base selection"), nil
		}
		// Only these eight read-only operations may be retried. A source revoked
		// during execution discards the entire first response, then resolves a
		// fresh scope once. Identity/DB/unknown errors never enter this retry.
		for attempt := 0; attempt < 2; attempt++ {
			targets, err := s.knowledge.Resolve(ctx, ids)
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

func personalJSON(ctx context.Context, value any) (*mcp.CallToolResult, error) {
	done := perftrace.Measure(ctx, perftrace.Serialize)
	defer done()
	b, err := json.Marshal(value)
	if err != nil && !logger.IsolatedPersonalPayload(ctx) {
		return nil, err
	}
	if err == nil && logger.IsolatedPersonalPayload(ctx) {
		b, err = redactPersonalOutput(b)
	}
	if err != nil {
		return mcp.NewToolResultError("resource denied or unavailable"), nil
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
		return personalJSON(ctx, rows)
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
		return personalJSON(ctx, map[string]any{"total": len(rows), "documents": rows[start:end]})
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
		return personalJSON(ctx, map[string]any{"total": len(rows), "chunks": rows[offset:end]})
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
		return personalJSON(ctx, rows)
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
		return personalJSON(ctx, rows)
	}
	return fail()
}
