package repository

import (
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// WikiDocumentSelectionEnabled checks the controlled activation of document selection.
func WikiDocumentSelectionEnabled() bool {
	return os.Getenv("KNOWHUB_WIKI_DOCUMENT_SELECTION") == "true"
}

// WikiDocumentSelectionForKB checks document-selection activation for one knowledge base.
// cannot move historical pages in every space out of the live namespace.
func WikiDocumentSelectionForKB(kb string) bool {
	if !WikiDocumentSelectionEnabled() {
		return false
	}
	for _, id := range strings.Split(os.Getenv("KNOWHUB_WIKI_DOCUMENT_SELECTION_KBS"), ",") {
		if strings.TrimSpace(id) == kb && kb != "" {
			path := os.Getenv("KNOWHUB_WIKI_DOCUMENT_SELECTION_ACTIVE_FILE")
			if path == "" {
				return true
			}
			file, err := os.Open(path)
			if err != nil {
				return false
			}
			defer func() { _ = file.Close() }()
			raw, err := io.ReadAll(io.LimitReader(file, (16<<10)+1))
			if err != nil || len(raw) > 16<<10 {
				return false
			}
			var active []string
			if json.Unmarshal(raw, &active) != nil || len(active) > 64 {
				return false
			}
			for _, enabled := range active {
				if enabled == kb {
					return true
				}
			}
			return false
		}
	}
	return false
}

// Read a page against the complete inputs recorded by its producer, rather
// than against unrelated current documents. Citations cannot reduce this set.
func wikiPageReadProof(db *gorm.DB, kb string, page *types.WikiPage) (*wikiGenerationProof, error) {
	if !WikiDocumentSelectionForKB(kb) {
		return wikiDependencies(db, kb)
	}
	if page == nil {
		if err := types.CheckPrivateKBRead(db, "", kb); err != nil {
			return nil, err
		}
		var anchor struct{ TenantID uint64 }
		err := db.Table("knowledge_bases kb").
			Select("kb.tenant_id").
			Joins("JOIN tenants t ON t.id=kb.tenant_id AND t.deleted_at IS NULL AND t.status='active'").
			Where("kb.id=? AND kb.deleted_at IS NULL AND kb.type='document' AND kb.is_temporary=false", kb).
			Take(&anchor).
			Error
		if err != nil {
			return nil, err
		}
		return &wikiGenerationProof{Tenant: anchor.TenantID, KB: kb}, nil
	}
	var seal WikiMCPProvenance
	if err := db.Where("page_id=? AND tenant_id=? AND knowledge_base_id=?", page.ID, page.TenantID, kb).
		Take(&seal).Error; err != nil {
		return nil, err
	}
	var sources map[string]string
	if json.Unmarshal([]byte(seal.SourceVersions), &sources) != nil || len(sources) == 0 ||
		len(sources) > 2048 {
		return nil, ErrWikiMCPUnverified
	}
	selected := map[string]bool{}
	for id := range sources {
		selected[id] = true
	}
	return wikiDependenciesSelected(db, kb, nil, selected)
}
