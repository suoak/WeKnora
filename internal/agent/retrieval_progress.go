package agent

import (
	"encoding/json"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

const (
	maxRetrievalToolCalls   = 24
	retrievalSynthesisCalls = 12
	retrievalTurnBudget     = 3 * time.Minute
)

// Retrieval progress is measured by returned chunk identities, not a model's
// changing search terms. Other workflows keep their existing iteration policy.
type retrievalProgress struct {
	calls, unchanged    int
	started             time.Time
	disabled, synthesis bool
	seen                map[string]bool
}

func (p *retrievalProgress) observe(calls []types.ToolCall, now time.Time) {
	if p.disabled || len(calls) == 0 {
		return
	}
	progress := false
	for _, call := range calls {
		switch call.Name {
		case "search_knowledge", "grep_chunks", "read_document", "list_documents":
		default:
			p.disabled = true
			return
		}
		if p.started.IsZero() {
			p.started = now
			p.seen = make(map[string]bool)
		}
		p.calls++
		if call.Result == nil || !call.Result.Success {
			continue
		}
		// Tool Data has one canonical, content-independent representation for
		// both native typed rows and JSON-decoded results. Errors grant no progress.
		encoded, err := json.Marshal(call.Result.Data)
		if err != nil {
			continue
		}
		var data map[string]any
		if json.Unmarshal(encoded, &data) != nil {
			continue
		}
		for _, field := range []string{"chunks", "results"} {
			rows, _ := data[field].([]any)
			for _, value := range rows {
				row, _ := value.(map[string]any)
				id, _ := row["chunk_id"].(string)
				if id == "" {
					id, _ = row["id"].(string)
				}
				if id != "" && !p.seen[id] {
					p.seen[id] = true
					progress = true
				}
			}
		}
	}
	if progress {
		p.unchanged = 0
	} else {
		p.unchanged++
	}
}

func (p *retrievalProgress) exhausted(now time.Time) bool {
	return !p.disabled && !p.started.IsZero() && (p.calls >= maxRetrievalToolCalls || now.Sub(p.started) >= retrievalTurnBudget)
}

func (p *retrievalProgress) needsSynthesis() bool {
	return !p.disabled && !p.synthesis && p.calls > 0 && (p.calls >= retrievalSynthesisCalls || p.unchanged >= 3)
}
