package mcpserver

import "github.com/mark3labs/mcp-go/mcp"

const personalServerInstructions = `Start with list_knowledge_bases to discover the current Key's authorized scope and real search capabilities. Never guess unauthorized KB IDs. Use search_documents first for document titles, filenames or existence questions across all authorized KBs; optionally narrow by exact KB IDs. Use grep_chunks or search_knowledge mode=keyword for precise terms, semantic for meaning, and hybrid (default) for general retrieval. An index or runtime fallback is reported explicitly. Use read_document with the returned knowledge_id; page with offset and limit until all chunks are read. Partial, failed, no_scope and no_searchable_index do not mean that knowledge does not exist. Report failures or incomplete scope honestly; never claim absence from an incomplete search. Optional knowledge_base_ids omitted, null or [] means all authorized scope. Ambiguous KB names require an exact ID.`

func personalSearchDocumentsTool() mcp.Tool {
	return mcp.NewTool("search_documents",
		mcp.WithDescription("Find document titles and filenames across all currently authorized public and private knowledge bases. Use this first to check whether a named document exists. Returns knowledge_id for read_document, KB ID/name, title, filename and parse status. Exact matches precede prefixes and substrings; then most recently updated. Bounded results report truncation."),
		mcp.WithString("query", mcp.Required(), mcp.Description("Document title or filename, matched as literal case-insensitive text")),
		mcp.WithArray("knowledge_base_ids", mcp.WithStringItems(), mcp.Description("Optional authorized KB IDs or unique names; omitted, null or [] searches all authorized KBs")),
		mcp.WithNumber("limit", mcp.Description("Maximum documents, default 20, max 100")),
		mcp.WithReadOnlyHintAnnotation(true),
	)
}
