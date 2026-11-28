package repository

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type wikiPreservingGeneration struct {
	mu       sync.Mutex
	readable map[string]bool
	stale    map[string]string // slug -> exact previous row snapshot; never model input
}

func (p *wikiPreservingGeneration) pages() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	ids := make([]string, 0, len(p.readable))
	for id := range p.readable {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func preservingGeneration(ctx context.Context) *wikiPreservingGeneration {
	proof, _ := ctx.Value(wikiGenerationProofKey{}).(*wikiGenerationProof)
	if proof == nil {
		return nil
	}
	return proof.Replacement
}

// Enumerate the complete current KB on every check. Selecting all documents
// enables the existing personal-publication verifier without skipping failed,
// denied or newly added sources. A previous selection is never reused as the
// current inventory. The normal personal-KB opt-in still applies.
func wikiPreservingDependencies(
	db *gorm.DB, kb string, previous ...*wikiGenerationProof,
) (*wikiGenerationProof, error) {
	var ids []string
	if err := db.Table("knowledges").Where("knowledge_base_id=? AND deleted_at IS NULL", kb).
		Order("id").Limit(2049).Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	if len(ids) == 0 || len(ids) > 2048 {
		return nil, ErrWikiMCPUnverified
	}
	selected := make(map[string]bool, len(ids))
	for _, id := range ids {
		selected[id] = true
	}
	proof, err := wikiDependenciesSelected(db, kb, nil, selected, previous...)
	if err != nil {
		return nil, err
	}
	var current []string
	if err := db.Table("knowledges").Where("knowledge_base_id=? AND deleted_at IS NULL", kb).
		Order("id").Limit(2049).Pluck("id", &current).Error; err != nil {
		return nil, err
	}
	if wikiDigest(ids) != wikiDigest(current) {
		return nil, ErrWikiMCPInputsChanged
	}
	return proof, nil
}

// BeginWikiPreservingGeneration keeps historical pages in the browser namespace
// while excluding unverified old rows from every guarded generation read. The
// caller must separately authorize the operator recovery; this grants no source
// access and still verifies the complete current source closure.
func BeginWikiPreservingGeneration(
	ctx context.Context, repo interfaces.WikiPageRepository, kb string,
) (context.Context, error) {
	r, ok := repo.(*wikiPageRepository)
	if !ok {
		return ctx, ErrWikiMCPUnverified
	}
	var proof *wikiGenerationProof
	state := &wikiPreservingGeneration{readable: map[string]bool{}, stale: map[string]string{}}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := checkWikiMCPEpochCoverage(tx); err != nil {
			return err
		}
		before, err := readWikiMCPEpoch(tx, kb)
		if err != nil {
			return err
		}
		proof, err = wikiPreservingDependencies(tx, kb)
		if err != nil {
			return err
		}
		var pages []*types.WikiPage
		if err := tx.Session(&gorm.Session{SkipHooks: true}).Table("wiki_pages AS preserving_capture").
			Where("knowledge_base_id=? AND tenant_id=? AND deleted_at IS NULL", kb, proof.Tenant).
			Order("id").Limit(wikiMCPMaxPages + 1).Find(&pages).Error; err != nil {
			return err
		}
		if len(pages) > wikiMCPMaxPages {
			return ErrWikiMCPUnverified
		}
		for _, page := range pages {
			err := verifyWikiProof(tx, page, proof, false, nil)
			if err == nil {
				state.readable[page.ID] = true
			} else if errors.Is(err, ErrWikiMCPUnverified) {
				if state.stale[page.Slug] != "" {
					return ErrWikiPageConflict
				}
				state.stale[page.Slug] = wikiDigest(page)
			} else {
				return err
			}
		}
		after, err := readWikiMCPEpoch(tx, kb)
		if err != nil {
			return err
		}
		if before != after {
			return ErrWikiMCPInputsChanged
		}
		proof.WikiGeneration, proof.WikiIncarnation, proof.Replacement = after.Generation, after.Incarnation, state
		if err := recordWikiNoWikiSources(tx, proof); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return ctx, err
	}
	ctx = context.WithValue(ctx, wikiGenerationProofKey{}, proof)
	selection := &WikiDocumentSelection{Eligible: map[string]bool{}, Skipped: map[string]string{}}
	for id := range proof.Sources {
		if !proof.EmptySources[id] {
			selection.Eligible[id] = true
		}
	}
	ctx = context.WithValue(ctx, wikiDocumentSelectionKey{}, selection)
	guard := func(db *gorm.DB, knowledge, target string, page *types.WikiPage) error {
		if target != kb || knowledge != "" && proof.Sources[knowledge] == "" {
			return ErrWikiMCPUnverified
		}
		db = db.Session(&gorm.Session{NewDB: true}).Model(nil).Session(&gorm.Session{NewDB: true})
		fresh, err := wikiDependencies(db, kb, proof)
		if err != nil {
			return err
		}
		if fresh.DependencySHA256 != proof.DependencySHA256 || wikiDigest(fresh.Epochs) != wikiDigest(proof.Epochs) {
			return ErrWikiMCPInputsChanged
		}
		if page != nil {
			return verifyWikiProof(db, page, fresh, false, nil)
		}
		return nil
	}
	return types.WithWikiGenerationPageReadGuard(ctx, kb, state.pages, guard), nil
}

// Read the previous body solely inside the commit transaction, for revision
// history. It is never returned to the generation worker or sent to a model.
func replacePreservedWikiPage(ctx context.Context, tx *gorm.DB, page *types.WikiPage) (bool, error) {
	state := preservingGeneration(ctx)
	if state == nil {
		return false, nil
	}
	expected := state.stale[page.Slug]
	if expected == "" {
		return false, nil
	}
	var previous types.WikiPage
	err := tx.Session(&gorm.Session{SkipHooks: true}).Table("wiki_pages AS preserving_previous").
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("knowledge_base_id=? AND tenant_id=? AND slug=? AND deleted_at IS NULL",
			page.KnowledgeBaseID, page.TenantID, page.Slug).Take(&previous).Error
	if err != nil {
		return false, err
	}
	if wikiDigest(&previous) != expected || previous.Slug != page.Slug {
		return false, ErrWikiPageConflict
	}
	if err := retainWikiPublishedSnapshot(ctx, tx, &previous, page); err != nil {
		return false, err
	}
	revision := types.WikiPageRevision{
		ID: uuid.NewString(), TenantID: previous.TenantID, KnowledgeBaseID: previous.KnowledgeBaseID,
		PageID: previous.ID, Slug: previous.Slug, Version: previous.Version,
		Title: previous.Title, PageType: previous.PageType, Status: previous.Status,
		Content: previous.Content, Summary: previous.Summary, Aliases: previous.Aliases,
		EditSource: previous.LastEditSource, EditorID: previous.LastEditorID,
		EditedAt: previous.UpdatedAt, CreatedAt: time.Now().UTC(),
	}
	// A collision in history is evidence to inspect, never overwrite it.
	if err := tx.Create(&revision).Error; err != nil {
		return false, err
	}
	page.ID, page.Version, page.CreatedAt = previous.ID, previous.Version, previous.CreatedAt
	if err := updateWikiPageRow(tx, page); err != nil {
		return false, err
	}
	return true, nil
}

func readPreservingOutput(tx *gorm.DB, id string, saved *types.WikiPage) error {
	return tx.Session(&gorm.Session{SkipHooks: true}).Table("wiki_pages AS preserving_output").
		Where("id=? AND deleted_at IS NULL", id).Take(saved).Error
}
