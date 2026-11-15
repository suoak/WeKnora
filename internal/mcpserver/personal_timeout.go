package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// Track protocol completion, not result bodies. mcp-go may send progress
// notifications before dropping the final response on context expiry.
type personalRPCResponse struct {
	http.ResponseWriter
	mu       sync.Mutex
	id       json.RawMessage
	status   int
	complete bool
	line     []byte
}

func newPersonalRPCResponse(w http.ResponseWriter, id json.RawMessage) *personalRPCResponse {
	return &personalRPCResponse{ResponseWriter: w, id: id}
}

func (w *personalRPCResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *personalRPCResponse) WriteHeader(status int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}

func (w *personalRPCResponse) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status == 0 {
		w.status = http.StatusOK
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *personalRPCResponse) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(p)
	if n > 0 && !w.complete {
		if strings.HasPrefix(w.Header().Get("Content-Type"), "text/event-stream") {
			w.observeEvents(p[:n])
		} else {
			w.complete = true
		}
	}
	return n, err
}

func (w *personalRPCResponse) observeEvents(p []byte) {
	// Bound memory independently of document size. An oversized/unknown data
	// frame is treated as committed output; never append a second RPC response.
	const maxLine = 64 << 10
	for len(p) > 0 && !w.complete {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			i = len(p)
		}
		if len(w.line)+i > maxLine {
			w.complete = true
			w.line = nil
			return
		}
		w.line = append(w.line, p[:i]...)
		if i == len(p) {
			return
		}
		p = p[i+1:]
		if data, ok := bytes.CutPrefix(w.line, []byte("data:")); ok {
			var message struct {
				Method string `json:"method"`
			}
			// Notifications have a method; RPC results/errors do not. Do not
			// retain any document text or duplicate a response at a deadline race.
			w.complete = json.Unmarshal(bytes.TrimSpace(data), &message) != nil || message.Method == ""
		}
		w.line = w.line[:0]
	}
}

func (w *personalRPCResponse) finish(ctx context.Context) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.id) == 0 || !errors.Is(ctx.Err(), context.DeadlineExceeded) || w.complete ||
		(w.status != 0 && w.status != http.StatusOK) {
		return
	}
	// Fixed, public protocol error: never serialize internal errors, credentials
	// or a partial tool result after its authorization context expired.
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": w.id,
		"error": map[string]any{
			"code":    -32000,
			"message": "personal MCP request timed out; retry with a narrower scope",
		},
	})
	if w.status != 0 && strings.HasPrefix(w.Header().Get("Content-Type"), "text/event-stream") {
		_, _ = fmt.Fprintf(w.ResponseWriter, "event: message\ndata: %s\n\n", body)
		_ = http.NewResponseController(w.ResponseWriter).Flush()
		return
	}
	if w.status == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-KnowHub-Error-Code", "request_timeout")
		w.ResponseWriter.WriteHeader(http.StatusGatewayTimeout)
	}
	_, _ = w.ResponseWriter.Write(append(body, '\n'))
}
