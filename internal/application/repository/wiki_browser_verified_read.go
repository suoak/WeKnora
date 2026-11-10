package repository

import (
	"context"
	"errors"
	"sync"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

// This serves current proved pages only. It neither grants document-body
// access nor certifies historical revisions, and never mutates Wiki state.
func BeginVerifiedWikiBrowserRead(ctx context.Context, repo interfaces.WikiPageRepository, kb string) (context.Context, error) {
	r, ok := repo.(*wikiPageRepository)
	if !ok || !WikiDocumentSelectionForKB(kb) {
		return ctx, ErrWikiMCPUnverified
	}
	tenant := types.WorkspaceWebReadTenant(ctx)
	if tenant == 0 {
		return ctx, types.ErrLegacySourceRestricted
	}
	var proof *wikiGenerationProof
	ids := []string{}
	versions := map[string]string{}
	partial := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		anchor, err := wikiPageReadProof(tx, kb, nil)
		if err != nil {
			return err
		}
		if anchor.Tenant != tenant {
			return types.ErrLegacySourceRestricted
		}
		selection, captured, err := selectWikiDocumentInputs(tx, kb, tenant)
		if err != nil {
			return err
		}
		proof = captured
		partial = len(selection.Skipped) > 0
		var pages []*types.WikiPage
		if err := tx.Session(&gorm.Session{SkipHooks: true}).Table("wiki_pages AS browser_validation_inputs").Where("tenant_id=? AND knowledge_base_id=?", tenant, kb).Order("id").Limit(wikiMCPMaxPages + 1).Find(&pages).Error; err != nil {
			return err
		}
		if len(pages) > wikiMCPMaxPages {
			return ErrWikiMCPUnverified
		}
		for _, page := range pages {
			err := verifyWikiProof(tx, page, proof, false, nil)
			if errors.Is(err, ErrWikiMCPUnverified) {
				partial = true
				continue
			}
			if err != nil {
				return err
			}
			ids = append(ids, page.ID)
			versions[page.ID] = wikiPageDigest(page)
		}
		return nil
	})
	if err != nil {
		return ctx, err
	}
	var readsMu sync.Mutex
	reads := map[string]string{}
	guard := func(db *gorm.DB, knowledge, target string, page *types.WikiPage) error {
		if target != kb || knowledge != "" || types.WorkspaceWebReadTenant(db.Statement.Context) != tenant {
			return types.ErrLegacySourceRestricted
		}
		// This can run inside an ORM COUNT/projection callback. Force an
		// empty statement before deriving nested readers; a second Session
		// alone would retain the parent's SELECT and destination.
		db = db.Session(&gorm.Session{NewDB: true}).Model(nil).Session(&gorm.Session{NewDB: true})
		if err := types.CheckPrivateKBRead(db, "", kb); err != nil {
			return err
		}
		if len(proof.SelectedSources) > 0 {
			fresh, err := wikiDependenciesSelected(db, kb, nil, proof.SelectedSources, proof)
			if err != nil {
				return err
			}
			if fresh.DependencySHA256 != proof.DependencySHA256 || wikiDigest(fresh.Epochs) != wikiDigest(proof.Epochs) {
				return ErrWikiMCPInputsChanged
			}
		} else {
			if _, err := wikiPageReadProof(db, kb, nil); err != nil {
				return err
			}
		}
		if page != nil {
			if versions[page.ID] == "" || versions[page.ID] != wikiPageDigest(page) {
				return ErrWikiMCPInputsChanged
			}
			if err := verifyWikiProof(db, page, proof, false, nil); err != nil {
				return err
			}
			readsMu.Lock()
			reads[page.ID] = versions[page.ID]
			readsMu.Unlock()
			return nil
		}
		readsMu.Lock()
		defer readsMu.Unlock()
		if len(reads) > 0 {
			readIDs := make([]string, 0, len(reads))
			for id := range reads {
				readIDs = append(readIDs, id)
			}
			var current []*types.WikiPage
			if err := db.Session(&gorm.Session{SkipHooks: true}).Table("wiki_pages AS browser_output_inputs").Where("tenant_id=? AND knowledge_base_id=? AND id IN ?", tenant, kb, readIDs).Find(&current).Error; err != nil {
				return err
			}
			if len(current) != len(reads) {
				return ErrWikiMCPInputsChanged
			}
			for _, p := range current {
				if reads[p.ID] != wikiPageDigest(p) {
					return ErrWikiMCPInputsChanged
				}
			}
		}
		return nil
	}
	ctx = types.WithWikiVerifiedPageReadGuard(ctx, kb, ids, guard)
	types.RecordLegacyCandidateRead(ctx, kb)
	if partial {
		types.RecordLegacyReadExclusion(ctx)
	}
	return ctx, nil
}
