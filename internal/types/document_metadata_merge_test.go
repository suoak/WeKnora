package types

import (
	"encoding/json"
	"testing"
)

func TestSetDocumentMetadataPreservesParserMetadata(t *testing.T) {
	chunk := &Chunk{Metadata: JSON(`{
		"parser.sheet":"SPEC",
		"parser.row":"3",
		"spreadsheet.entity_axis":"columns"
	}`)}
	meta := &DocumentChunkMetadata{GeneratedQuestions: []GeneratedQuestion{{ID: "q1", Question: "question"}}}
	if err := chunk.SetDocumentMetadata(meta); err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(chunk.Metadata, &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"parser.sheet", "parser.row", "spreadsheet.entity_axis", "generated_questions"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("metadata key %q was lost: %s", key, chunk.Metadata)
		}
	}
}
