package spreadsheet

import (
	"slices"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func syntheticSchema() Schema {
	return Schema{
		SheetName:     "SPEC",
		Headers:       []string{"Feature", "Description", "Model-A", "Model-B", "Model-C", "Model-D"},
		CommonColumns: []string{"Feature", "Description"},
		EntityAxis:    "columns",
		EntityHeaders: []string{"Model-A", "Model-B", "Model-C", "Model-D"},
		EntityCount:   4,
		Confidence:    0.95,
		Records: []Record{
			{Common: map[string]string{"Feature": "Performance", "Description": "x"}, Entities: map[string]string{"Model-A": "100", "Model-B": "200", "Model-C": "300", "Model-D": "400"}},
			{Common: map[string]string{"Feature": "WLAN", "Description": "x"}, Entities: map[string]string{"Model-A": "Supported", "Model-B": "Not supported", "Model-C": "Supported", "Model-D": "Supported"}},
			{Common: map[string]string{"Feature": "SD-WAN", "Description": "x"}, Entities: map[string]string{"Model-A": "Supported", "Model-B": "Supported", "Model-C": "Not supported", "Model-D": "Supported"}},
		},
	}
}

func TestExecuteSpreadsheetCollectionMatrix(t *testing.T) {
	schema := syntheticSchema()
	for _, query := range []string{"\u5168\u91cf\u652f\u6301\u578b\u53f7\u6e05\u5355", "\u6709\u54ea\u4e9b\u578b\u53f7\uff1f"} {
		result := Execute(types.RetrievalQueryExhaustiveList, query, "fixture.xlsx", []Schema{schema})
		if !result.Completeness.IsExhaustive || !slices.Equal(result.Entities, schema.EntityHeaders) {
			t.Fatalf("list result = %#v", result)
		}
	}
	count := Execute(types.RetrievalQueryCount, "\u4e00\u5171\u6709\u591a\u5c11\u4e2a\u578b\u53f7\uff1f", "fixture.xlsx", []Schema{schema})
	if count.Count != 4 || count.Completeness.ExpectedEntityCount != 4 {
		t.Fatalf("count result = %#v", count)
	}
	positive := Execute(types.RetrievalQueryFilter, "\u54ea\u4e9b\u578b\u53f7\u652f\u6301 WLAN\uff1f", "fixture.xlsx", []Schema{schema})
	if !positive.Completeness.IsExhaustive || !slices.Equal(positive.Entities, []string{"Model-A", "Model-C", "Model-D"}) {
		t.Fatalf("positive filter = %#v", positive)
	}
	negative := Execute(types.RetrievalQueryFilter, "\u54ea\u4e9b\u578b\u53f7\u4e0d\u652f\u6301 WLAN\uff1f", "fixture.xlsx", []Schema{schema})
	if !negative.Completeness.IsExhaustive || !slices.Equal(negative.Entities, []string{"Model-B"}) {
		t.Fatalf("negative filter = %#v", negative)
	}
}

func TestExecuteUnknownAxisIsPartial(t *testing.T) {
	result := Execute(types.RetrievalQueryExhaustiveList, "\u5168\u90e8\u578b\u53f7\u6709\u54ea\u4e9b\uff1f", "unknown.xlsx", []Schema{{EntityAxis: "unknown"}})
	if result.Completeness.IsExhaustive || result.Content != "" {
		t.Fatalf("unknown axis must remain partial: %#v", result)
	}
}
