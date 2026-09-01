package chatpipeline

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestRetrievalCompletenessGuard(t *testing.T) {
	partial := retrievalCompletenessGuard(&types.ChatManage{PipelineState: types.PipelineState{
		RetrievalQueryType:    types.RetrievalQueryExhaustiveList,
		RetrievalCompleteness: types.PartialRetrievalCompleteness(),
	}})
	if !strings.Contains(partial, "\u5f53\u524d\u4ec5\u83b7\u5f97\u90e8\u5206\u7ed3\u679c") || !strings.Contains(partial, `exhaustive="false"`) {
		t.Fatalf("missing partial-result guard: %s", partial)
	}
	fact := retrievalCompletenessGuard(&types.ChatManage{PipelineState: types.PipelineState{
		RetrievalQueryType: types.RetrievalQueryFactLookup,
	}})
	if fact != "" {
		t.Fatalf("fact lookup must not receive collection guard: %s", fact)
	}
}
