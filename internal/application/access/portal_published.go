package access

import (
	"github.com/Tencent/WeKnora/internal/types"
)

// Published is the public authority in this product mode. An absent historical
// approved record is intentionally irrelevant. Existing explicit sensitive/MCP
// denies conservatively hold the whole KB; unmarked mixed content is NOT made
// safe by this rule and is not claimed as protected.
func portalPublishedDocuments(s *types.EnterpriseReadSnapshot) ([]string, error) {
	if s == nil || !s.TenantActive || !s.PortalPublished || s.KBTemporary || s.KBType != "document" || s.TenantID == 0 || s.KBID == "" || s.PortalPublicBlocked || types.ExplicitPortalPublicBlock(s.KB) || types.ExplicitPortalPublicBlock(s.Space) {
		return nil, ErrForbidden
	}
	return portalEffectiveDocuments(s)
}

func portalEffectiveDocuments(s *types.EnterpriseReadSnapshot) ([]string, error) {
	if s == nil || !s.TenantActive || s.KBTemporary || s.KBType != "document" || len(s.Documents) > 2048 {
		return nil, ErrForbidden
	}
	ids := []string{}
	for _, d := range s.Documents {
		if d.TenantID != s.TenantID || d.KnowledgeBaseID != s.KBID || d.ID == "" || d.CurrentVersion == "" {
			return nil, ErrForbidden
		}
		if types.ExplicitPortalPublicBlock(d.EnterprisePolicy) {
			return nil, ErrForbidden
		}
		// Canonical enabled parsed text only. Published is the authority for
		// imported/synced ordinary text too, not a web/api channel heuristic.
		if d.ParseStatus == types.ParseStatusCompleted && d.EnableStatus == "enabled" && (d.Type == "file" || d.Type == types.KnowledgeTypeManual) {
			ids = append(ids, d.ID)
		}
	}
	return ids, nil
}
