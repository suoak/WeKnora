package mcpserver

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/perftrace"
)

// Full-scope retrieval and Wiki proofs multiply database work. On the isolated
// two-core PostgreSQL instance two complete searches fit the existing budget;
// four do not. Keep the normal tool/user quotas and bound this expensive subset
// separately, before scope SQL. Excess work receives an explicit retryable 429.
func newPersonalSearchWork(c config.PersonalMCPConcurrencyConfig) *personalAdmission {
	c.GlobalLimit = min(c.GlobalLimit, 2)
	return newPersonalAdmission(c, false)
}

func personalNeedsSearchWork(tool string, targets []string) bool {
	if tool != "search_knowledge" && tool != "grep_chunks" && tool != "search_documents" &&
		tool != "wiki_search" && tool != "wiki_read_page" && tool != "wiki_index" {
		return false
	}
	// Eight is one retrieval batch. This bounds work, never the searched scope.
	return len(targets) == 0 || len(targets) > 8
}

func (s *PersonalServer) acquireSearchWork(w http.ResponseWriter, r *http.Request, user string) (func(), bool) {
	done := perftrace.Measure(r.Context(), perftrace.SearchWorkQueue)
	release, err := s.searchWork.acquire(r.Context(), user)
	done()
	if err == nil {
		return release, true
	}
	if errors.Is(err, errPersonalCapacity) {
		if v, ok := r.Context().Value(invocationKey{}).(*invocation); ok {
			v.concurrencyLimited = true
		}
		w.Header().Set("Retry-After", strconv.Itoa(int((s.searchWork.wait+time.Second-1)/time.Second)))
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-KnowHub-Error-Code", "concurrency_limited")
		http.Error(w, "personal MCP search work capacity exceeded; retry later", http.StatusTooManyRequests)
	} else if errors.Is(err, context.DeadlineExceeded) {
		http.Error(w, "personal MCP request timed out", http.StatusGatewayTimeout)
	} else {
		http.Error(w, "personal MCP request canceled", http.StatusRequestTimeout)
	}
	return nil, false
}
