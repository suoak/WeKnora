package service

import (
	"context"
	"errors"
	"io"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
)

func (s *knowledgeService) beginLegacySourceIdentity(
	ctx context.Context, kb *types.KnowledgeBase, store *repository.FeishuLegacyStore,
	ds *types.DataSource, item *types.FetchedItem,
) (*types.FeishuLegacyResource, *types.FeishuLegacyAttempt, error) {
	raw := repository.LegacySourceHash(item.Content, item.FileName, item.Title)
	if !legacyDOCXExport(item) {
		return store.Begin(ctx, ds, item, raw)
	}
	identity, err := repository.LegacyDOCXSourceIdentity(item.Content, item.FileName, item.Title)
	if err != nil {
		return nil, nil, err
	}
	r, a, err := store.BeginWithSourceIdentity(ctx, ds, item, raw, identity, nil)
	var fence *repository.LegacyFenceError
	if !errors.As(err, &fence) || fence.Reason != "source_identity_legacy_proof_required" {
		return r, a, err
	}
	baseline, err := store.SourceIdentityBaseline(ctx, ds, item.ExternalID)
	if err != nil {
		return nil, nil, err
	}
	path := baseline.FilePath()
	if path == "" || !s.legacyPreviousFileIsControlled(ctx, kb, path) {
		return nil, nil, ErrFeishuStagingRequired
	}
	svc := s.resolveFileServiceForPath(ctx, kb, path)
	if svc == nil {
		return nil, nil, ErrFeishuStagingRequired
	}
	archive, err := svc.GetFile(ctx, path)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = archive.Close() }()
	const maxArchiveBytes = 64 << 20
	content, err := io.ReadAll(io.LimitReader(archive, maxArchiveBytes+1))
	if err != nil {
		return nil, nil, err
	}
	if len(content) > maxArchiveBytes {
		return nil, nil, ErrFeishuStagingRequired
	}
	proof, err := baseline.ProveDOCX(content)
	if err != nil {
		return nil, nil, err
	}
	return store.BeginWithSourceIdentity(ctx, ds, item, raw, identity, proof)
}

func legacyDOCXExport(item *types.FetchedItem) bool {
	return item != nil && getFileType(item.FileName) == "docx" &&
		item.Metadata["obj_type"] == "docx" && item.Metadata["attachment"] != "true"
}
