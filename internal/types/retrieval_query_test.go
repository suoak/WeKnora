package types

import "testing"

func TestDetectRetrievalQueryType(t *testing.T) {
	tests := []struct {
		query string
		want  RetrievalQueryType
	}{
		{"R13 \u5168\u91cf\u652f\u6301\u578b\u53f7\u6e05\u5355", RetrievalQueryExhaustiveList},
		{"R13 \u6709\u54ea\u4e9b\u578b\u53f7\uff1f", RetrievalQueryExhaustiveList},
		{"\u5168\u90e8\u578b\u53f7", RetrievalQueryExhaustiveList},
		{"R13 \u4e00\u5171\u6709\u591a\u5c11\u4e2a\u578b\u53f7\uff1f", RetrievalQueryCount},
		{"\u54ea\u4e9b\u578b\u53f7\u652f\u6301 WLAN\uff1f", RetrievalQueryFilter},
		{"\u54ea\u4e9b\u578b\u53f7\u4e0d\u652f\u6301 WLAN\uff1f", RetrievalQueryFilter},
		{"Model-A WLAN \u662f\u5426\u652f\u6301\uff1f", RetrievalQueryFactLookup},
	}
	for _, test := range tests {
		t.Run(test.query, func(t *testing.T) {
			if got := DetectRetrievalQueryType(test.query); got != test.want {
				t.Fatalf("DetectRetrievalQueryType() = %q, want %q", got, test.want)
			}
		})
	}
}
