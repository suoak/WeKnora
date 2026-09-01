package types

import "strings"

// RetrievalQueryType refines QueryIntent with the retrieval shape needed to
// answer a knowledge-base question safely.
type RetrievalQueryType string

const (
	RetrievalQueryFactLookup     RetrievalQueryType = "fact_lookup"
	RetrievalQueryExhaustiveList RetrievalQueryType = "exhaustive_list"
	RetrievalQueryCount          RetrievalQueryType = "count"
	RetrievalQueryFilter         RetrievalQueryType = "filter"
	RetrievalQueryComparison     RetrievalQueryType = "comparison"
)

const (
	RetrievalScopePartial     = "partial"
	RetrievalScopeEntityIndex = "entity_index"
	RetrievalScopeFullSheet   = "full_sheet"
)

// RetrievalCompleteness is runtime evidence about the scope used to produce
// an answer. Its zero value is deliberately partial and non-exhaustive.
type RetrievalCompleteness struct {
	Scope               string   `json:"scope"`
	IsExhaustive        bool     `json:"is_exhaustive"`
	ExpectedEntityCount int      `json:"expected_entity_count"`
	ObservedEntityCount int      `json:"observed_entity_count"`
	SourceFile          string   `json:"source_file,omitempty"`
	SourceSheets        []string `json:"source_sheets,omitempty"`
	EntityAxis          string   `json:"entity_axis,omitempty"`
}

func PartialRetrievalCompleteness() RetrievalCompleteness {
	return RetrievalCompleteness{Scope: RetrievalScopePartial}
}

// DetectRetrievalQueryType is an always-available rule layer. Unicode escapes
// keep this source stable on Windows checkouts with a non-UTF-8 console.
func DetectRetrievalQueryType(query string) RetrievalQueryType {
	normalized := strings.ToLower(strings.TrimSpace(query))
	if normalized == "" {
		return RetrievalQueryFactLookup
	}
	if containsAny(normalized, "\u5bf9\u6bd4", "\u6bd4\u8f83", "\u533a\u522b", "\u5dee\u5f02", "compare", "difference") {
		return RetrievalQueryComparison
	}
	if containsAny(normalized, "\u591a\u5c11\u4e2a", "\u4e00\u5171\u6709\u591a\u5c11", "\u603b\u5171\u6709\u591a\u5c11", "\u603b\u5171\u591a\u5c11", "\u7edf\u8ba1\u6570\u91cf", "how many") {
		return RetrievalQueryCount
	}
	collectionQuestion := containsAny(normalized, "\u54ea\u4e9b", "which")
	conditional := containsAny(normalized,
		"\u652f\u6301", "\u4e0d\u652f\u6301", "supported", "not supported", "\u6ee1\u8db3", "\u7b26\u5408")
	if collectionQuestion && conditional {
		return RetrievalQueryFilter
	}
	if collectionQuestion || containsAny(normalized,
		"\u5168\u91cf", "\u5168\u90e8", "\u6240\u6709", "\u5b8c\u6574", "\u6e05\u5355", "\u5217\u8868", "\u6709\u54ea\u4e9b", "\u5206\u522b\u6709\u54ea\u4e9b", "\u5217\u51fa", "full list", "list all", "complete list") {
		return RetrievalQueryExhaustiveList
	}
	return RetrievalQueryFactLookup
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}
