package repository

import (
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

const (
	wikiEpochVersionBatchDocuments = 16
	wikiEpochVersionBatchChunks    = 16384
)

type wikiEpochVersion struct {
	version string
	tenant  uint64
}

// This cache lives only inside one complete proof calculation. It stores no
// rights or source state. Both input-stamp fences and every connector check
// remain live in wikiDependenciesSelected, including the final output boundary.
type wikiEpochVersions struct {
	db        *gorm.DB
	kb        string
	tenant    uint64
	ids       []string
	positions map[string]int
	versions  map[string]wikiEpochVersion
}

func newWikiEpochVersions(db *gorm.DB, kb string, tenant uint64, ids []string) *wikiEpochVersions {
	positions := make(map[string]int, len(ids))
	for i, id := range ids {
		positions[id] = i
	}
	return &wikiEpochVersions{
		db:        db,
		kb:        kb,
		tenant:    tenant,
		ids:       ids,
		positions: positions,
		versions:  map[string]wikiEpochVersion{},
	}
}

func (v *wikiEpochVersions) get(id string) (string, uint64, error) {
	if err := v.db.Statement.Context.Err(); err != nil {
		return "", 0, err
	}
	if prior, ok := v.versions[id]; ok {
		return prior.version, prior.tenant, nil
	}
	position, ok := v.positions[id]
	if !ok {
		return "", 0, ErrWikiMCPUnverified
	}
	start := position / wikiEpochVersionBatchDocuments * wikiEpochVersionBatchDocuments
	ids := v.ids[start:min(start+wikiEpochVersionBatchDocuments, len(v.ids))]
	var chunks int64
	if err := v.db.Table("chunks").Where("knowledge_id IN ?", ids).Count(&chunks).Error; err != nil {
		return "", 0, err
	}
	if chunks > wikiEpochVersionBatchChunks {
		// Large documents retain their original per-document path.
		// Never allocate all chunk metadata for an unbounded KB batch.
		return v.single(id)
	}
	docs := make([]types.EnterpriseDocumentState, len(ids))
	for i, id := range ids {
		docs[i].ID = id
	}
	if err := populateCanonicalDocumentVersions(v.db, docs, nil, false, wikiEpochVersionBatchChunks); err != nil {
		if errors.Is(err, errDocumentVersionBatchTooLarge) {
			// A concurrent writer may enlarge the batch after its count. The
			// actual fetch has its own LIMIT and never retains an oversized set.
			return v.single(id)
		}
		return "", 0, err
	}
	var owners []struct {
		ID              string
		TenantID        uint64
		KnowledgeBaseID string
	}
	if err := v.db.Table("knowledges").Select("id,tenant_id,knowledge_base_id").
		Where("id IN ? AND deleted_at IS NULL", ids).Find(&owners).Error; err != nil {
		return "", 0, err
	}
	if len(owners) != len(ids) {
		return "", 0, ErrWikiMCPInputsChanged
	}
	allowed := make(map[string]bool, len(owners))
	for _, owner := range owners {
		if owner.TenantID != v.tenant || owner.KnowledgeBaseID != v.kb {
			return "", 0, ErrWikiMCPInputsChanged
		}
		allowed[owner.ID] = true
	}
	for _, doc := range docs {
		if !allowed[doc.ID] || doc.CurrentVersion == "" {
			return "", 0, ErrWikiMCPUnverified
		}
		v.versions[doc.ID] = wikiEpochVersion{doc.CurrentVersion, v.tenant}
	}
	version, ok := v.versions[id]
	if !ok {
		return "", 0, ErrWikiMCPUnverified
	}
	return version.version, version.tenant, nil
}

func (v *wikiEpochVersions) single(id string) (string, uint64, error) {
	var owner struct {
		TenantID        uint64
		KnowledgeBaseID string
	}
	if err := v.db.Table("knowledges").Select("tenant_id,knowledge_base_id").
		Where("id=? AND deleted_at IS NULL", id).Take(&owner).Error; err != nil {
		return "", 0, err
	}
	if owner.TenantID != v.tenant || owner.KnowledgeBaseID != v.kb {
		return "", 0, ErrWikiMCPInputsChanged
	}
	version, tenant, err := resourceVersion(v.db, types.EnterprisePolicyTarget{Kind: "knowledge", ID: id})
	if err == nil {
		v.versions[id] = wikiEpochVersion{version, tenant}
	}
	return version, tenant, err
}
