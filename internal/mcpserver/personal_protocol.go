package mcpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/logger"
)

const (
	personalPath         = "/api/v1/mcp/personal"
	isolatedPersonalPath = "/internal/mcp/isolated/personal"
)

var errPersonalProtocol = errors.New("invalid personal MCP request")

// The boundary belongs to the existing permanently read-only PersonalServer,
// never to the workspace catalog. It runs before analytics and tool dispatch.
func personalHTTPBoundary(next http.Handler, path string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		r = r.WithContext(logger.WithIsolatedPersonalPayload(r.Context()))
		if r.URL.EscapedPath() != path || r.URL.RawQuery != "" {
			http.Error(w, "personal MCP path denied", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost && r.Method != http.MethodGet && r.Method != http.MethodDelete {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" ||
				(u.Scheme != "http" && u.Scheme != "https") || !strings.EqualFold(u.Host, r.Host) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
		}
		if enc := r.Header.Get("Content-Encoding"); enc != "" && !strings.EqualFold(enc, "identity") {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			return
		}
		if r.Method != http.MethodPost {
			b, err := io.ReadAll(io.LimitReader(r.Body, 1))
			if err != nil || len(b) != 0 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" || len(params) > 1 ||
			(len(params) == 1 && !strings.EqualFold(params["charset"], "utf-8")) {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		b, err := io.ReadAll(r.Body)
		if err != nil {
			status := http.StatusBadRequest
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				status = http.StatusRequestEntityTooLarge
			}
			w.WriteHeader(status)
			return
		}
		if err := validatePersonalRPC(b); err != nil {
			http.Error(w, "invalid personal MCP request", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(b))
		next.ServeHTTP(w, r)
	})
}

// Reject duplicate keys and excessive nesting before ordinary JSON decoding.
func personalJSONValue(d *json.Decoder, depth int) error {
	if depth > 16 {
		return errPersonalProtocol
	}
	t, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	for d.More() {
		if delim == '{' {
			key, err := d.Token()
			name, ok := key.(string)
			if err != nil || !ok || seen[name] {
				return errPersonalProtocol
			}
			seen[name] = true
		}
		if err := personalJSONValue(d, depth+1); err != nil {
			return err
		}
	}
	_, err = d.Token()
	return err
}

func onlyPersonalFields(m map[string]any, fields string) bool {
	allowed := map[string]bool{}
	for _, name := range strings.Fields(fields) {
		allowed[name] = true
	}
	for k := range m {
		if !allowed[k] {
			return false
		}
	}
	return true
}

func validatePersonalRPC(b []byte) error {
	if !utf8.Valid(b) {
		return errPersonalProtocol
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if personalJSONValue(d, 0) != nil {
		return errPersonalProtocol
	}
	if _, err := d.Token(); err != io.EOF {
		return errPersonalProtocol
	}
	var envelope map[string]any
	if json.Unmarshal(b, &envelope) != nil || envelope["jsonrpc"] != "2.0" ||
		!onlyPersonalFields(envelope, "jsonrpc id method params") {
		return errPersonalProtocol
	}
	params, _ := envelope["params"].(map[string]any)
	if v, present := envelope["params"]; present && v != nil && params == nil {
		return errPersonalProtocol
	}
	switch envelope["method"] {
	case "initialize":
		if !onlyPersonalFields(params, "protocolVersion capabilities clientInfo _meta") {
			return errPersonalProtocol
		}
	case "ping", "notifications/initialized":
		if !onlyPersonalFields(params, "_meta") {
			return errPersonalProtocol
		}
	case "notifications/cancelled":
		if !onlyPersonalFields(params, "requestId reason _meta") {
			return errPersonalProtocol
		}
	case "tools/list":
		if !onlyPersonalFields(params, "cursor _meta") {
			return errPersonalProtocol
		}
	case "tools/call":
		if !onlyPersonalFields(params, "name arguments _meta") {
			return errPersonalProtocol
		}
		name, _ := params["name"].(string)
		args, _ := params["arguments"].(map[string]any)
		if v, present := params["arguments"]; present && v != nil && args == nil {
			return errPersonalProtocol
		}
		fields := ""
		switch name {
		case "list_knowledge_bases":
		case "search_knowledge", "grep_chunks":
			fields = "query mode limit knowledge_base_ids"
		case "list_documents":
			fields = "knowledge_base_id keyword page page_size"
		case "read_document":
			fields = "knowledge_id offset limit query"
		case "wiki_search":
			fields = "query regex knowledge_base_ids knowledge_base_id limit offset"
		case "wiki_read_page":
			fields = "slug knowledge_base_id offset"
		case "wiki_index":
			fields = "knowledge_base_id limit offset"
		default:
			return errPersonalProtocol
		}
		if !onlyPersonalFields(args, fields) {
			return errPersonalProtocol
		}
		for k, v := range args {
			switch k {
			case "query", "keyword", "slug", "knowledge_base_id", "knowledge_id", "mode":
				s, ok := v.(string)
				if !ok || len(s) > 4096 {
					return errPersonalProtocol
				}
			case "knowledge_base_ids":
				ids, ok := v.([]any)
				if !ok || len(ids) > 128 {
					return errPersonalProtocol
				}
				for _, id := range ids {
					s, ok := id.(string)
					if !ok || len(s) > 256 {
						return errPersonalProtocol
					}
				}
			case "regex":
				if v != false {
					return errPersonalProtocol
				}
			default:
				n, ok := v.(float64)
				if !ok || n < 0 || n > 10000 || n != float64(int(n)) {
					return errPersonalProtocol
				}
			}
		}
	default:
		return errPersonalProtocol
	}
	return nil
}
