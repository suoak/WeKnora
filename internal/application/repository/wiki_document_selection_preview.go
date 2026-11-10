package repository

import (
	"context"
	"database/sql"
	"errors"
	"sort"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// The preview contains identifiers, counts and reasons, never historical body
// text. The production operator command uses a database READ ONLY transaction.
type WikiDocumentSelectionPreview struct {
	TenantID          uint64            `json:"tenant_id"`
	KnowledgeBaseID   string            `json:"knowledge_base_id"`
	EligibleDocuments []string          `json:"eligible_documents"`
	SkippedDocuments  map[string]string `json:"skipped_documents"`
	RetainedPages     int               `json:"retained_pages"`
	IsolatedPages     []string          `json:"isolated_pages"`
	QueueDocuments    []string          `json:"queue_documents"`
	PendingOps        int64             `json:"pending_operations"`
	Snapshot          string            `json:"snapshot"`
}

func PreviewWikiDocumentSelection(ctx context.Context, db *gorm.DB, tenant uint64, kb string) (*WikiDocumentSelectionPreview, error) {
	if tenant == 0 || kb == "" {
		return nil, errors.New("exact tenant and KB required")
	}
	plan := &WikiDocumentSelectionPreview{TenantID: tenant, KnowledgeBaseID: kb, IsolatedPages: []string{}, QueueDocuments: []string{}}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var anchor struct{ TenantID uint64 }
		if err := tx.Table("knowledge_bases kb").Select("kb.tenant_id").Joins("JOIN tenants t ON t.id=kb.tenant_id AND t.status='active' AND t.deleted_at IS NULL").Where("kb.id=? AND kb.tenant_id=? AND kb.deleted_at IS NULL AND kb.type='document' AND kb.is_temporary=false", kb, tenant).Take(&anchor).Error; err != nil {
			return err
		}
		if err := types.CheckPrivateKBRead(tx, "", kb); err != nil {
			return err
		}
		if err := checkWikiMCPEpochCoverage(tx); err != nil {
			return err
		}
		selection, proof, err := selectWikiDocumentInputs(tx, kb, tenant)
		if err != nil {
			return err
		}
		plan.EligibleDocuments = wikiSelectionIDs(selection.Eligible)
		plan.SkippedDocuments = selection.Skipped
		var pages []*types.WikiPage
		if err := tx.Session(&gorm.Session{SkipHooks: true}).Table("wiki_pages AS preview_inputs").Where("tenant_id=? AND knowledge_base_id=?", tenant, kb).Order("id").Limit(wikiMCPMaxPages + 1).Find(&pages).Error; err != nil {
			return err
		}
		if len(pages) > wikiMCPMaxPages {
			return ErrWikiMCPUnverified
		}
		queue := map[string]bool{}
		for _, page := range pages {
			err := verifyWikiProof(tx, page, proof, false, nil)
			if err == nil {
				plan.RetainedPages++
				continue
			}
			if !errors.Is(err, ErrWikiMCPUnverified) {
				return err
			}
			plan.IsolatedPages = append(plan.IsolatedPages, page.ID)
			for _, ref := range page.SourceRefs {
				id := types.WikiSourceKnowledgeID(ref)
				if selection.Eligible[id] {
					queue[id] = true
				}
			}
		}
		for id := range queue {
			plan.QueueDocuments = append(plan.QueueDocuments, id)
		}
		sort.Strings(plan.QueueDocuments)
		var pending []types.TaskPendingOp
		if err := tx.Where("tenant_id=? AND scope='knowledge_base' AND scope_id=? AND task_type IN ('wiki:ingest','wiki:finalize')", tenant, kb).Order("id").Find(&pending).Error; err != nil {
			return err
		}
		plan.PendingOps = int64(len(pending))
		plan.Snapshot = wikiDigest([]any{tenant, kb, proof.Sources, proof.Epochs, selection.Skipped, pages, pending})
		return nil
	}, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	return plan, err
}
