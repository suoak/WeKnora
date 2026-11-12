package service

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
)

func (s *knowledgeService) beginLegacySourceIdentity(
	ctx context.Context, kb *types.KnowledgeBase, store *repository.FeishuLegacyStore,
	ds *types.DataSource, item *types.FetchedItem,
) (*types.FeishuLegacyResource, *types.FeishuLegacyAttempt, error) {
	r, a, err := s.beginLegacySourceIdentityStrict(ctx, kb, store, ds, item)
	var fence *repository.LegacyFenceError
	if os.Getenv("KNOWHUB_FEISHU_LATEST_CONTENT") != "true" || item.CurrentSnapshot == nil ||
		!errors.As(
			err,
			&fence,
		) || (fence.Reason != "source_revision_content_conflict" && fence.
		Reason != "source_identity_legacy_proof_required") {
		return r, a, err
	}
	// Capture before the network observation; Begin checks this exact database
	// generation under its write lock. A concurrent publication invalidates it.
	baseline, err := store.LatestSnapshotBaseline(ctx, ds, item.ExternalID)
	if err != nil {
		return nil, nil, err
	}
	fresh, err := item.CurrentSnapshot(ctx)
	if err != nil {
		return nil, nil, err
	}
	proof, err := baseline.ProveCurrent(item, fresh)
	if err != nil {
		return nil, nil, err
	}
	var identity *types.FeishuLegacySourceIdentity
	if legacyDOCXExport(item) {
		identity, err = repository.LegacyDOCXSourceIdentity(item.Content, item.FileName, item.Title)
	} else if legacyXLSXExport(item) {
		identity, err = repository.LegacyXLSXSourceIdentity(item.Content, item.FileName, item.Title)
	}
	if err != nil {
		return nil, nil, err
	}
	return store.BeginLatestSourceSnapshot(ctx, ds, item,
		repository.LegacySourceHash(item.Content, item.FileName, item.Title), identity, proof)
}

func (s *knowledgeService) beginLegacySourceIdentityStrict(
	ctx context.Context, kb *types.KnowledgeBase, store *repository.FeishuLegacyStore,
	ds *types.DataSource, item *types.FetchedItem,
) (*types.FeishuLegacyResource, *types.FeishuLegacyAttempt, error) {
	raw := repository.LegacySourceHash(item.Content, item.FileName, item.Title)
	xlsx := legacyXLSXExport(item)
	if !legacyDOCXExport(item) && !xlsx {
		return store.Begin(ctx, ds, item, raw)
	}
	identify := repository.LegacyDOCXSourceIdentity
	if xlsx {
		identify = repository.LegacyXLSXSourceIdentity
	}
	identity, err := identify(item.Content, item.FileName, item.Title)
	if err != nil {
		return nil, nil, err
	}
	r, a, err := store.BeginWithSourceIdentity(ctx, ds, item, raw, identity, nil)
	var fence *repository.LegacyFenceError
	if !errors.As(err, &fence) || fence.Reason != "source_identity_legacy_proof_required" {
		return r, a, err
	}
	if os.Getenv("KNOWHUB_FEISHU_LATEST_CONTENT") == "true" && item.CurrentSnapshot != nil {
		// Current full-source proof supersedes an unavailable historical
		// archive. It does not certify the old bytes or publish them again.
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
	prove := baseline.ProveDOCX
	if xlsx {
		prove = baseline.ProveXLSX
	}
	proof, err := prove(content)
	if err != nil {
		return nil, nil, err
	}
	return store.BeginWithSourceIdentity(ctx, ds, item, raw, identity, proof)
}

func legacyDOCXExport(item *types.FetchedItem) bool {
	return item != nil && getFileType(item.FileName) == "docx" &&
		item.Metadata["obj_type"] == "docx" && item.Metadata["attachment"] != "true"
}

func legacyXLSXExport(item *types.FetchedItem) bool {
	return item != nil && getFileType(item.FileName) == "xlsx" &&
		(item.Metadata["obj_type"] == "sheet" || item.Metadata["obj_type"] == "bitable") &&
		item.Metadata["attachment"] != "true"
}
