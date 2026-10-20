package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"

	"github.com/Tencent/WeKnora/internal/types"
)

// MVP first additions preserve the existing body verbatim. A whole-page LLM
// rewrite is not evidence that another source's contribution survived.
// The per-KID marker is committed with body/refs in the same guarded page write.
func (s *wikiIngestService) firstAddDependencies(
	ctx context.Context,
	page *types.WikiPage,
	updates []SlugUpdate,
) ([]SlugUpdate, map[string]bool, error) {
	candidates := map[string]bool{}
	for _, u := range updates {
		if u.Type == "retract" || u.Type == "retractStale" {
			continue
		}
		first, err := s.isMVPWikiKnowledge(ctx, u.KnowledgeID)
		if err != nil {
			return nil, nil, err
		}
		if first {
			candidates[u.KnowledgeID] = true
		}
	}
	if len(candidates) == 0 {
		return updates, nil, nil
	}
	var meta map[string]json.RawMessage
	if len(page.PageMetadata) > 0 {
		if err := json.Unmarshal(page.PageMetadata, &meta); err != nil {
			return nil, nil, err
		}
	}
	seen := map[string]bool{}
	if b := meta["feishu_first_add"]; len(b) > 0 {
		if err := json.Unmarshal(b, &seen); err != nil {
			return nil, nil, err
		}
	}
	active := map[string]bool{}
	filtered := make([]SlugUpdate, 0, len(updates))
	hasRetract := false
	for _, u := range updates {
		if u.Type == "retract" || u.Type == "retractStale" {
			hasRetract = true
			filtered = append(filtered, u)
			continue
		}
		if candidates[u.KnowledgeID] {
			if seen[u.KnowledgeID] {
				continue
			}
			active[u.KnowledgeID] = true
		}
		filtered = append(filtered, u)
	}
	if len(active) > 0 && hasRetract {
		return nil, nil, errors.New("wiki_first_add_mixed_retraction_deferred")
	}
	return filtered, active, nil
}

func markWikiFirstAdd(page *types.WikiPage, ids map[string]bool) error {
	if len(ids) == 0 {
		return nil
	}
	meta := map[string]json.RawMessage{}
	if len(page.PageMetadata) > 0 {
		if err := json.Unmarshal(page.PageMetadata, &meta); err != nil {
			return err
		}
	}
	if meta == nil {
		meta = map[string]json.RawMessage{}
	}
	seen := map[string]bool{}
	if b := meta["feishu_first_add"]; len(b) > 0 {
		if err := json.Unmarshal(b, &seen); err != nil {
			return err
		}
	}
	for id := range ids {
		seen[id] = true
	}
	b, err := json.Marshal(seen)
	if err != nil {
		return err
	}
	meta["feishu_first_add"] = b
	b, err = json.Marshal(meta)
	if err != nil {
		return err
	}
	page.PageMetadata = types.JSON(b)
	return nil
}

func preserveWikiFirstAddBody(previous, generated string) string {
	if strings.TrimSpace(previous) == "" {
		return generated
	}
	return previous + "\n\n---\n\n" + generated
}

type (
	wikiPostErrorsKey       struct{}
	wikiFirstAddFinalizeKey struct{}
	wikiPostErrors          struct {
		mu     sync.Mutex
		failed bool
	}
)

func withWikiPostErrors(ctx context.Context) (context.Context, *wikiPostErrors) {
	result := &wikiPostErrors{}
	return context.WithValue(ctx, wikiPostErrorsKey{}, result), result
}

func recordWikiPostError(ctx context.Context, err error) {
	if err == nil {
		return
	}
	if state, ok := ctx.Value(wikiPostErrorsKey{}).(*wikiPostErrors); ok {
		state.mu.Lock()
		state.failed = true
		state.mu.Unlock()
	}
}
func (s *wikiPostErrors) hasFailure() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.failed }
func (s *wikiIngestService) isMVPWikiKnowledge(ctx context.Context, id string) (bool, error) {
	if s.knowledgeSvc == nil {
		return false, nil
	}
	k, err := s.knowledgeSvc.GetKnowledgeByIDOnly(ctx, id)
	if err != nil {
		return false, err
	}
	var meta map[string]any
	return k != nil && k.Channel == "feishu_personal" && json.Unmarshal(k.Metadata, &meta) == nil &&
		meta["feishu_mvp_attempt"] != "", nil
}

func (s *wikiIngestService) hasFirstAddPages(ctx context.Context, kb string, slugs []string) (bool, error) {
	for _, slug := range slugs {
		page, err := s.wikiService.GetPageBySlug(ctx, kb, slug)
		if err != nil {
			return false, err
		}
		if page != nil && strings.Contains(string(page.PageMetadata), `"feishu_first_add"`) {
			return true, nil
		}
	}
	return false, nil
}
