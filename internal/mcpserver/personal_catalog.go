package mcpserver

// Personal MCP has its own registry; endpoint tools are a separate product.
var personalToolCatalog = [...]string{
	"list_knowledge_bases", "search_knowledge", "grep_chunks", "list_documents",
	"read_document", "wiki_search", "wiki_read_page", "wiki_index",
	"list_writable_knowledge_bases", "add_document", "upload_document", "get_document_processing_status",
}

func knownPersonalTool(name string) bool {
	for _, tool := range personalToolCatalog {
		if tool == name {
			return true
		}
	}
	return false
}
