package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/application/repository"
)

// Default-off APPs preserve their existing Wiki generation behavior. In the
// explicitly enabled personal-MCP composition, unknown generation inputs must
// stop before any model call; never silently fall back to an unproved context.
func (s *wikiIngestService) personalWikiProofContext(ctx context.Context, kb string) (context.Context, error) {
	if !s.personalWikiProofEnabled() {
		return ctx, nil
	}
	pages, ok := s.wikiService.(*wikiPageService)
	if !ok {
		return nil, repository.ErrWikiMCPUnverified
	}
	return pages.personalWikiGenerationContext(ctx, kb)
}

func (s *wikiIngestService) personalWikiProofEnabled() bool {
	knowledge, ok := s.knowledgeSvc.(*knowledgeService)
	return ok && knowledge.config != nil && knowledge.config.PersonalMCP != nil && knowledge.config.PersonalMCP.Enabled
}

// Historical queue text has no generation-input proof. Retraction can contain
// deleted source bodies; it must not be fed into a newly certified generation.
func (s *wikiIngestService) checkPersonalWikiPending(ops []WikiPendingOp) error {
	if !s.personalWikiProofEnabled() {
		return nil
	}
	for _, op := range ops {
		if op.Op != WikiOpIngest || op.DocSummary != "" || op.DocTitle != "" || len(op.PageSlugs) != 0 || len(op.FolderIDs) != 0 {
			return repository.ErrWikiMCPUnverified
		}
	}
	return nil
}

func (s *wikiIngestService) personalWikiFinalizeInputs(description string, refs []linkRef) (string, []linkRef) {
	if !s.personalWikiProofEnabled() {
		return description, refs
	}
	// Rebuild from the currently proved pages/documents instead of trusting
	// old queued summaries, titles or removed-source text. IDs still control
	// durable queue completion; no historical text reaches the model.
	return "Refresh the directory using only the current verified pages and documents.", nil
}
