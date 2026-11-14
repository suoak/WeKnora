package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKnowledgeCatalogCountUnknownIsNotEmpty(t *testing.T) {
	unknown := formatKnowledgeBaseList([]*KnowledgeBaseInfo{{ID: "kb", Name: "ELOG", DocCount: -1}})
	require.Contains(t, unknown, `doc_count="unknown"`)
	require.NotContains(t, unknown, `doc_count="0"`)
	require.Contains(t, unknown, "does not mean the knowledge base is empty")
	empty := formatKnowledgeBaseList([]*KnowledgeBaseInfo{{ID: "kb", DocCount: 0}})
	require.Contains(t, empty, `doc_count="0"`)
	require.NotContains(t, empty, "catalog_status")
	populated := formatKnowledgeBaseList([]*KnowledgeBaseInfo{{ID: "kb", DocCount: 128}})
	require.Contains(t, populated, `doc_count="128"`)
}
