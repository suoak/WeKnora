package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

type partialWikiSearch struct{ *fakeWikiPageService }

func (s *partialWikiSearch) SearchPagesWithStatus(
	context.Context, string, string, int,
) ([]*types.WikiPage, bool, error) {
	return nil, true, nil
}

func TestWikiSearchReportsVerifiedPartialEvenWithNoHits(t *testing.T) {
	tool := NewWikiSearchTool(&partialWikiSearch{}, nil, NewWikiScopesFromKBIDs([]string{"kb"}), nil)
	args, err := json.Marshal(map[string]interface{}{"query": "synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := tool.Execute(context.Background(), args)
	if err != nil || result == nil || !result.Success {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if result.Data["partial"] != true || !strings.Contains(result.Output, "partial=\"true\"") ||
		!strings.Contains(result.Output, "do not establish absence") {
		t.Fatalf("partial empty result lost its limitation: %+v", result)
	}
}
