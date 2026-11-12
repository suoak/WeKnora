package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const writableKBTool = "list_writable_knowledge_bases"

// ConfigurePersonalContentServer creates a separate external registry. The
// original registry and isolated TLS handler always retain exactly eight reads.
func ConfigurePersonalContentServer(s *PersonalServer, writer *service.PersonalContent) *PersonalServer {
	if !s.Enabled() || writer == nil {
		return s
	}
	s.content = writer
	s.writeSlots = make(chan struct{}, 2)
	m := server.NewMCPServer("knowhub-personal", "0.2.0", server.WithToolCapabilities(false), server.WithRecovery(), server.WithToolFilter(func(ctx context.Context, all []mcp.Tool) []mcp.Tool {
		scope, _ := types.TenantAPIKeyScopeFromContext(ctx)
		out := []mcp.Tool{}
		for _, t := range all {
			if (!personalContentTool(t.Name)) || !logger.IsolatedPersonalPayload(ctx) && scope.HasCapability(types.APIKeyCapabilityAddContent) {
				out = append(out, t)
			}
		}
		return out
	}))
	for _, t := range s.readTools() {
		m.AddTool(t, s.guard(s.call))
	}
	m.AddTool(mcp.NewTool(writableKBTool, mcp.WithDescription("List current knowledge bases where this Key's owner may add content. Rechecked on every write."), mcp.WithReadOnlyHintAnnotation(true)), s.callContent)
	m.AddTool(mcp.NewTool("add_document", mcp.WithDescription("Create Markdown/text knowledge with a required idempotency key. Processing is asynchronous. Reuse the key only for the identical request. URLs and local file paths are unsupported."),
		mcp.WithString("knowledge_base_id", mcp.Required()), mcp.WithString("title", mcp.Required()), mcp.WithString("content", mcp.Required()), mcp.WithString("idempotency_key", mcp.Required()), mcp.WithBoolean("publish"), mcp.WithReadOnlyHintAnnotation(false)), s.callContent)
	m.AddTool(mcp.NewTool("upload_document", mcp.WithDescription("Upload complete file bytes as canonical base64, never a local path or URL. Supports scanned PDF, DOCX, XLSX, TXT and Markdown up to 8 MiB. Reuse idempotency_key for retries. Discover a writable KB first; exact names are accepted."), mcp.WithString("knowledge_base_id", mcp.Required()), mcp.WithString("filename", mcp.Required()), mcp.WithString("content_base64", mcp.Required()), mcp.WithString("idempotency_key", mcp.Required()), mcp.WithReadOnlyHintAnnotation(false)), s.callContent)
	m.AddTool(mcp.NewTool("get_document_processing_status", mcp.WithDescription("Read processing status for a document created by this Key; permission is checked again. No document content or storage paths are returned."), mcp.WithString("document_id", mcp.Required()), mcp.WithReadOnlyHintAnnotation(true)), s.callContent)
	s.externalHandler = server.NewStreamableHTTPServer(m, server.WithStateLess(true))
	return s
}

func (s *PersonalServer) callContent(ctx context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s == nil || s.content == nil || logger.IsolatedPersonalPayload(ctx) {
		return mcp.NewToolResultError("tool not allowed"), nil
	}
	scope, ok := types.TenantAPIKeyScopeFromContext(ctx)
	if !ok || !scope.HasCapability(types.APIKeyCapabilityAddContent) {
		return mcp.NewToolResultError("add_content capability required"), nil
	}
	switch r.Params.Name {
	case "upload_document":
		args := r.GetArguments()
		if !onlyPersonalFields(args, "knowledge_base_id filename content_base64 idempotency_key") {
			return mcp.NewToolResultError("unexpected upload arguments; send file bytes, not paths or URLs"), nil
		}
		for _, name := range []string{"knowledge_base_id", "filename", "content_base64", "idempotency_key"} {
			if _, ok := args[name].(string); !ok {
				return mcp.NewToolResultError("missing string argument: " + name), nil
			}
		}
		kb, err := s.content.ResolveContentSelector(ctx, r.GetString("knowledge_base_id", ""))
		if err != nil {
			return mcp.NewToolResultError("choose a current writable knowledge base using list_writable_knowledge_bases"), nil
		}
		row, err := s.content.Upload(ctx, service.PersonalUploadRequest{KBID: kb, Filename: r.GetString("filename", ""), ContentBase64: r.GetString("content_base64", ""), IdempotencyKey: r.GetString("idempotency_key", "")})
		if err != nil {
			return mcp.NewToolResultError("upload denied or rejected; verify format, size and scanner availability, then retry with the same idempotency_key"), nil
		}
		return personalProcessingResult(ctx, row)
	case "get_document_processing_status":
		if !onlyPersonalFields(r.GetArguments(), "document_id") {
			return mcp.NewToolResultError("unexpected arguments"), nil
		}
		row, err := s.content.Status(ctx, r.GetString("document_id", ""))
		if err != nil {
			return mcp.NewToolResultError("status unavailable or authorization revoked"), nil
		}
		return personalProcessingResult(ctx, row)
	case writableKBTool:
		if len(r.GetArguments()) != 0 {
			return mcp.NewToolResultError("unexpected arguments"), nil
		}
		rows, err := s.content.ListWritable(ctx)
		if err != nil {
			return mcp.NewToolResultError("authorization denied"), nil
		}
		return personalJSON(ctx, rows)
	case "add_document":
		args := r.GetArguments()
		if !onlyPersonalFields(args, "knowledge_base_id title content idempotency_key publish") {
			return mcp.NewToolResultError("unexpected arguments; URLs and paths are not supported"), nil
		}
		for _, name := range []string{"knowledge_base_id", "title", "content", "idempotency_key"} {
			if _, ok := args[name].(string); !ok {
				return mcp.NewToolResultError("invalid string argument"), nil
			}
		}
		if value, present := args["publish"]; present {
			if _, ok := value.(bool); !ok {
				return mcp.NewToolResultError("invalid publish argument"), nil
			}
		}
		kb, err := s.content.ResolveContentSelector(ctx, r.GetString("knowledge_base_id", ""))
		if err != nil {
			return mcp.NewToolResultError("choose a current writable knowledge base using list_writable_knowledge_bases"), nil
		}
		row, err := s.content.Add(ctx, service.PersonalContentRequest{KBID: kb, Title: r.GetString("title", ""), Content: r.GetString("content", ""), IdempotencyKey: r.GetString("idempotency_key", ""), Publish: r.GetBool("publish", true)})
		if err != nil {
			return mcp.NewToolResultError("creation denied or failed; retry only with the same idempotency_key"), nil
		}
		return personalProcessingResult(ctx, row)
	}
	return mcp.NewToolResultError("tool not allowed"), nil
}

// routeContent runs before read admission and consumes separate bounded slots.
// Isolated requests never reach the external registry, including tools/list.
func (s *PersonalServer) routeContent(w http.ResponseWriter, r *http.Request) bool {
	if s.externalHandler == nil || logger.IsolatedPersonalPayload(r.Context()) || r.Method != http.MethodPost {
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 12<<20)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "request too large or invalid", http.StatusRequestEntityTooLarge)
		return true
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	var env struct {
		Method string `json:"method"`
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	if json.Unmarshal(body, &env) != nil || env.Method != "tools/call" || !personalContentTool(env.Params.Name) {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(body))
	if personalJSONValue(d, 0) != nil {
		http.Error(w, "invalid request", 400)
		return true
	}
	if _, err := d.Token(); err != io.EOF {
		http.Error(w, "invalid request", 400)
		return true
	}
	select {
	case s.writeSlots <- struct{}{}:
		defer func() { <-s.writeSlots }()
	default:
		w.Header().Set("Retry-After", "1")
		http.Error(w, "content service busy", 429)
		return true
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	w.Header().Set("Cache-Control", "private, no-store")
	s.externalHandler.ServeHTTP(w, r.WithContext(ctx))
	return true
}

func personalContentTool(name string) bool {
	switch name {
	case writableKBTool, "add_document", "upload_document", "get_document_processing_status":
		return true
	}
	return false
}
func personalProcessingResult(ctx context.Context, row *types.Knowledge) (*mcp.CallToolResult, error) {
	state, reason := "processing", ""
	switch row.ParseStatus {
	case "completed":
		state = "success"
	case "failed":
		state = "failed"
		reason = "Document processing failed; an authorized operator can retry the existing document."
	case "draft":
		state = "draft"
	}
	return personalJSON(ctx, map[string]any{"knowledge_base_id": row.KnowledgeBaseID, "document_id": row.ID, "status": state, "parse_status": row.ParseStatus, "failure_reason": reason, "next_step": "Use get_document_processing_status with document_id. Retry admission with the same idempotency_key; do not create a replacement."})
}
