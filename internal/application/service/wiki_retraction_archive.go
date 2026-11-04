package service

import (
	"encoding/json"

	"github.com/Tencent/WeKnora/internal/types"
)

const wikiRetractionArchiveKey = "wiki_retraction_archive"

func markWikiRetractionArchive(page *types.WikiPage) error {
	meta := map[string]json.RawMessage{}
	if len(page.PageMetadata) > 0 {
		if err := json.Unmarshal(page.PageMetadata, &meta); err != nil {
			return err
		}
	}
	if meta == nil {
		meta = map[string]json.RawMessage{}
	}
	meta[wikiRetractionArchiveKey] = json.RawMessage(`true`)
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	page.PageMetadata, page.Status = types.JSON(data), types.WikiPageStatusArchived
	return nil
}

func restoreWikiRetractionArchive(page *types.WikiPage) error {
	if page.Status != types.WikiPageStatusArchived || page.LastEditSource != types.WikiEditSourcePipeline {
		return nil
	}
	meta := map[string]json.RawMessage{}
	if len(page.PageMetadata) > 0 {
		if err := json.Unmarshal(page.PageMetadata, &meta); err != nil {
			return err
		}
	}
	var automatic bool
	if data := meta[wikiRetractionArchiveKey]; len(data) > 0 {
		if err := json.Unmarshal(data, &automatic); err != nil {
			return err
		}
	}
	if !automatic {
		return nil
	}
	delete(meta, wikiRetractionArchiveKey)
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	page.PageMetadata, page.Status = types.JSON(data), types.WikiPageStatusDraft
	return nil
}
