package mcpserver

import (
	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/mark3labs/mcp-go/mcp"
)

type personalKnowledgeBaseSummary struct {
	access.PortalKBMetadata
	ID           string   `json:"id"`
	TenantID     uint64   `json:"tenant_id"`
	Name         string   `json:"name"`
	Space        string   `json:"space"`
	Public       bool     `json:"public"`
	Capabilities []string `json:"capabilities"`
	Searchable   bool     `json:"searchable"`
}

func personalListKnowledgeBasesTool() mcp.Tool {
	return mcp.NewTool("list_knowledge_bases",
		mcp.WithDescription("List all knowledge bases currently authorized for this Personal Key. "+
			"Returns id, name, space, tenant_id, public, capabilities, searchable, "+
			"description, creator_name, created_at, updated_at, type and "+
			"document_count. Counts cover readable documents only. Use exact IDs "+
			"to distinguish duplicate names; no per-KB detail calls are needed for "+
			"these fields."),
		mcp.WithReadOnlyHintAnnotation(true),
	)
}

func personalGetKnowledgeBaseTool() mcp.Tool {
	return mcp.NewTool("get_knowledge_base",
		mcp.WithDescription("Get authorized knowledge base metadata and readable document_count by "+
			"exact ID from list_knowledge_bases. Names are not accepted."),
		mcp.WithString("knowledge_base_id", mcp.Required(), mcp.Description("Exact authorized knowledge base ID")),
		mcp.WithReadOnlyHintAnnotation(true),
	)
}
