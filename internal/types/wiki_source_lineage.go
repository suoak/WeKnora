package types

// WikiPageLineage identifies a sealed content revision, not a historical
// integer revision or a claim that every sentence has been fact checked.
// Sources are the page's recorded generation references, never the full input
// closure used to fence generation. Missing chunk attribution stays explicit.
type WikiPageLineage struct {
	PageID         string                  `json:"page_id"`
	RevisionSHA256 string                  `json:"revision_sha256"`
	Status         string                  `json:"status"`
	SourceCount    int                     `json:"source_count"`
	ChunkCount     int                     `json:"recorded_chunk_count"`
	Sources        []WikiSourceAttribution `json:"sources"`
}

type WikiSourceAttribution struct {
	KnowledgeID   string               `json:"knowledge_id"`
	Title         string               `json:"title"`
	Reference     string               `json:"reference"`
	VersionSHA256 string               `json:"source_version_sha256"`
	Level         string               `json:"attribution_level"`
	Chunks        []WikiChunkReference `json:"chunks"`
}

type WikiChunkReference struct {
	ChunkID   string `json:"chunk_id"`
	Reference string `json:"reference"`
}
