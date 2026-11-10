package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type usageRecorder interface {
	Record(context.Context, types.MCPUsageReport) error
}
type (
	invocationKey struct{}
	invocation    struct {
		report types.MCPUsageReport
		called bool
		scope  *types.MCPUsageScope
	}
)

// SetUsageRecorder attaches the shared analytics collector.
func (s *PersonalServer) SetUsageRecorder(r usageRecorder) { s.usage = r }

// SetUsageRecorder attaches the shared analytics collector.
func (s *Server) SetUsageRecorder(r usageRecorder) { s.usage = r }

func beginUsage(ctx context.Context, sink usageRecorder, report types.MCPUsageReport) {
	if recorder, ok := sink.(interface {
		Begin(context.Context, types.MCPUsageReport) error
	}); ok {
		if recorder.Begin(ctx, report) != nil {
			logger.Errorf(ctx, "[mcp-usage] admission spool failed; coverage=degraded")
		}
	}
}

func emitUsage(ctx context.Context, sink usageRecorder, report types.MCPUsageReport) {
	if sink == nil {
		return
	}
	if err := sink.Record(ctx, report); err != nil {
		logger.Errorf(ctx, "[mcp-usage] enqueue failed; event=%s coverage=degraded", report.EventKey)
	}
}

// observeTool is outside guard/retry handling. HTTP admission refusals are
// finalized by observeHTTP; direct calls use the same recorder here.
func observeTool(sink usageRecorder, entry string, next server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (result *mcp.CallToolResult, err error) {
		started := time.Now()
		v, outer := ctx.Value(invocationKey{}).(*invocation)
		if !outer {
			var scope *types.MCPUsageScope
			ctx, scope = types.WithMCPUsageScope(ctx)
			v = &invocation{report: newUsageReport(entry, req.Params.Name), scope: scope}
		}
		if !outer {
			beginUsage(ctx, sink, v.report)
		}
		v.called = true
		defer func() {
			panicked := recover()
			v.report.Success = panicked == nil && err == nil && result != nil && !result.IsError && ctx.Err() == nil
			v.report.ErrorCode = usageError(ctx, result, err)
			if panicked != nil {
				v.report.ErrorCode = "panic"
			}
			if session, ok := server.ClientSessionFromContext(ctx).(server.SessionWithClientInfo); ok && entry != "go_personal" {
				info := session.GetClientInfo()
				if safeUsageString(info.Name, 128) && info.Name != "" {
					v.report.ClientName, v.report.ClientSource = info.Name, "negotiated"
					if safeUsageString(info.Version, 64) {
						v.report.ClientVersion = info.Version
					}
				}
			}
			if v.report.Success {
				resultResources(&v.report, req, result)
				v.report.HitKnowledgeBaseIDs = append(v.report.HitKnowledgeBaseIDs, v.scope.Hits()...)
			}
			v.report.TargetKnowledgeBaseIDs = v.scope.Targets()
			if ep, e := endpointFromContext(ctx); e == nil {
				v.report.EndpointID = ep.ID
			}
			if !outer {
				v.report.LatencyMs = time.Since(started).Milliseconds()
				emitUsage(ctx, sink, v.report)
			}
			if panicked != nil {
				panic(panicked)
			}
		}()
		return next(ctx, req)
	}
}

func newUsageReport(entry, tool string) types.MCPUsageReport {
	if _, known := types.LookupMCPEndpointTool(tool); !known {
		tool = "unknown"
	}
	return types.MCPUsageReport{
		EventKey: "mcp:" + uuid.NewString(), EntryPoint: entry, ToolName: tool,
		Transport: "streamable-http", ClientName: "unknown", ClientSource: "unknown", OccurredAt: time.Now(),
	}
}

func safeUsageString(s string, limit int) bool {
	return len(s) <= limit && !strings.ContainsAny(s, "\r\n\x00")
}

func usageError(ctx context.Context, result *mcp.CallToolResult, err error) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if err != nil {
		return "execution_error"
	}
	if result == nil {
		return "empty_result"
	}
	if !result.IsError {
		return ""
	}
	// Match only fixed server error categories; never retain error text.
	for _, content := range result.Content {
		if text, ok := content.(mcp.TextContent); ok {
			s := strings.ToLower(text.Text)
			if strings.Contains(s, "rate limit") {
				return "rate_limited"
			}
			for _, term := range []string{
				"denied", "not allowed", "not enabled", "unauthorized", "authorization", "outside this endpoint",
			} {
				if strings.Contains(s, term) {
					return "permission_denied"
				}
			}
			if strings.Contains(s, "required") || strings.Contains(s, "invalid") {
				return "invalid_arguments"
			}
		}
	}
	return "tool_error"
}

type usageHTTPWriter struct {
	http.ResponseWriter
	status int
}

type usageReplayBody struct {
	io.Reader
	io.Closer
}

// Capture the execution cause before a handler releases its bounded context.
// An admission check may translate cancellation to an existing HTTP denial.
func captureUsageContextError(ctx context.Context) {
	v, ok := ctx.Value(invocationKey{}).(*invocation)
	if !ok || ctx.Err() == nil || v.report.ErrorCode == "panic" {
		return
	}
	v.report.Success = false
	v.report.ErrorCode = usageError(ctx, nil, ctx.Err())
}

func (w *usageHTTPWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *usageHTTPWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *usageHTTPWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Parsing only the protocol envelope also accounts for authenticated tools/call
// rejected before dispatch. initialize/list/authentication are never tool events.
func observeHTTP(sink usageRecorder, entry string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sink == nil || r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}
		original := r.Body
		body, err := io.ReadAll(io.LimitReader(original, (1<<20)+1))
		r.Body = usageReplayBody{Reader: io.MultiReader(bytes.NewReader(body), original), Closer: original}
		var envelope struct {
			Method string `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		if err != nil || len(body) > 1<<20 || json.Unmarshal(body, &envelope) != nil ||
			envelope.Method != "tools/call" ||
			envelope.Params.Name == "" {
			next.ServeHTTP(w, r)
			return
		}
		ctx, scope := types.WithMCPUsageScope(r.Context())
		v := &invocation{report: newUsageReport(entry, envelope.Params.Name), scope: scope}
		ctx = context.WithValue(ctx, invocationKey{}, v)
		if ep, e := endpointFromContext(ctx); e == nil {
			v.report.EndpointID = ep.ID
		}
		beginUsage(ctx, sink, v.report)
		writer := &usageHTTPWriter{ResponseWriter: w, status: 200}
		started := time.Now()
		defer func() {
			captureUsageContextError(ctx)
			terminal := v.report.ErrorCode == "timeout" || v.report.ErrorCode == "canceled" ||
				v.report.ErrorCode == "panic"
			if !v.called && !terminal {
				v.report.ErrorCode = "protocol_error"
				if v.report.ToolName == "unknown" {
					v.report.ErrorCode = "unknown_tool"
				}
			}
			if writer.status >= 400 && !terminal {
				v.report.Success = false
				switch writer.status {
				case 400, 422:
					v.report.ErrorCode = "invalid_arguments"
				case 403:
					v.report.ErrorCode = "permission_denied"
				case 429:
					v.report.ErrorCode = "rate_limited"
				case 408, 504:
					v.report.ErrorCode = "timeout"
				default:
					v.report.ErrorCode = "protocol_error"
				}
			}
			v.report.LatencyMs = time.Since(started).Milliseconds()
			emitUsage(ctx, sink, v.report)
		}()
		next.ServeHTTP(writer, r.WithContext(ctx))
	})
}

func resultResources(report *types.MCPUsageReport, req mcp.CallToolRequest, result *mcp.CallToolResult) {
	search := req.Params.Name == "search_knowledge" || req.Params.Name == "grep_chunks" ||
		req.Params.Name == "wiki_search"
	read := req.Params.Name == "read_document"
	if !search && !read && req.Params.Name != "wiki_read_page" && req.Params.Name != "wiki_index" &&
		req.Params.Name != "list_documents" {
		return
	}
	seen := map[string]bool{}
	var walk func(any, int)
	walk = func(value any, depth int) {
		if depth > 12 || len(seen) >= types.MaxMCPUsageResources {
			return
		}
		switch v := value.(type) {
		case []any:
			for _, item := range v {
				walk(item, depth+1)
			}
		case map[string]any:
			_, hasDocument := v["knowledge_id"]
			_, hasPage := v["slug"]
			if id, ok := v["knowledge_base_id"].(string); ok && safeUsageString(id, 64) && id != "" &&
				(!search || hasDocument || hasPage) {
				seen[id] = true
			}
			for _, key := range []string{
				"results", "data", "chunks", "pages", "documents", "document", "knowledge_base",
			} {
				if item, ok := v[key]; ok {
					walk(item, depth+1)
				}
			}
		}
	}
	if result.StructuredContent != nil {
		if raw, e := json.Marshal(result.StructuredContent); e == nil {
			var value any
			if json.Unmarshal(raw, &value) == nil {
				walk(value, 0)
			}
		}
	} else {
		for _, c := range result.Content {
			if text, ok := c.(mcp.TextContent); ok {
				var value any
				if json.Unmarshal([]byte(text.Text), &value) == nil {
					walk(value, 0)
				}
			}
		}
	}
	for id := range seen {
		if search {
			report.HitKnowledgeBaseIDs = append(report.HitKnowledgeBaseIDs, id)
		} else {
			report.KnowledgeBaseIDs = append(report.KnowledgeBaseIDs, id)
		}
	}
	if read {
		if id := req.GetString("knowledge_id", ""); id != "" {
			report.KnowledgeIDs = []string{id}
		}
	}
}
