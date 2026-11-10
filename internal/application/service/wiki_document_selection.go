package service

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
)

func wikiDocumentSelectionEnabled() bool {
	return repository.WikiDocumentSelectionEnabled()
}

func (s *wikiIngestService) selectWikiPending(
	ctx context.Context,
	kb string,
	ops []WikiPendingOp,
	ids []int64,
	claims map[int64]time.Time,
) ([]WikiPendingOp, []int64, error) {
	if !repository.WikiDocumentSelectionForKB(kb) || !s.personalWikiProofEnabled() {
		return ops, ids, nil
	}
	pages, ok := s.wikiService.(*wikiPageService)
	if !ok {
		return nil, nil, repository.ErrWikiMCPUnverified
	}
	archived, err := repository.ArchiveWikiSkippedOperations(ctx, pages.repo, kb, ids, claims)
	if err != nil {
		return nil, nil, err
	}
	removed := map[int64]bool{}
	for _, id := range archived {
		removed[id] = true
	}
	kept := make([]WikiPendingOp, 0, len(ops))
	keptIDs := make([]int64, 0, len(ids))
	for _, op := range ops {
		if !removed[op.dbID] {
			// Queue text predates the captured current-content proof. Preserve
			// its original durable payload, but never forward it to generation.
			op.DocTitle, op.DocSummary = "", ""
			op.PageSlugs, op.FolderIDs = nil, nil
			kept = append(kept, op)
		}
	}
	for _, id := range ids {
		if !removed[id] {
			keptIDs = append(keptIDs, id)
		}
	}
	return kept, keptIDs, nil
}

func wikiSelectionMayContinue(err error) bool {
	return err == nil || errors.Is(err, repository.ErrWikiNoEligibleDocuments)
}

func (s *wikiIngestService) parkWikiFinalize(ctx context.Context, kb string, ids []int64) error {
	pages, ok := s.wikiService.(*wikiPageService)
	if !ok {
		return repository.ErrWikiMCPUnverified
	}
	return repository.ParkWikiFinalizeWithoutInputs(ctx, pages.repo, kb, ids)
}
