package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource"
	feishucore "github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/infrastructure/chunker"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
)

func safeLegacyIncrementalEnabled() bool {
	v, e := strconv.ParseBool(os.Getenv("FEISHU_SAFE_INCREMENTAL_ENABLED"))
	return e == nil && v
}
func legacyWikiDrive(kind string) bool {
	return kind == "feishu" || kind == "feishu_drive" || kind == "lark" || kind == "lark_drive"
}

func legacyFetchNeedsHold(err error) bool {
	// A known availability fault is not evidence of withdrawn access. An
	// authentication/permission/not-found or unclassified result is uncertain,
	// never a permanent-delete assertion.
	return !errors.Is(err, feishucore.ErrRateLimited) && !errors.Is(err, feishucore.ErrTransient) &&
		!errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled)
}

func legacyItemNeedsHold(item *types.FetchedItem) bool {
	category := item.Metadata["feishu_error_category"]
	return category != "rate_limit" && category != "transient"
}

// Only server-owned sync executions can change persistent source quarantine.
func (s *DataSourceService) legacySourceUncertain(ctx context.Context, ds *types.DataSource, uncertain bool) error {
	if !safeLegacyIncrementalEnabled() || !legacyWikiDrive(ds.Type) {
		return nil
	}
	store, err := repository.FeishuLegacyStoreFor(s.knowledgeService.GetRepository())
	if err != nil {
		return err
	}
	return store.SetSourceUncertain(context.WithoutCancel(ctx), ds, uncertain)
}

func (s *DataSourceService) legacyMissingItem(ctx context.Context, ds *types.DataSource, item *types.FetchedItem) error {
	store, err := repository.FeishuLegacyStoreFor(s.knowledgeService.GetRepository())
	if err != nil {
		return err
	}
	// Listing absence is not a trustworthy permanent-delete event, including
	// when SyncDeletions is false. Preserve the old version privately, deny
	// reads now, and require reconciliation instead of acknowledging a cursor.
	_, err = store.RestrictSource(context.WithoutCancel(ctx), ds, item.ExternalID, false)
	if err != nil {
		return err
	}
	return types.ErrLegacySourceRestricted
}

func (s *knowledgeService) restrictLegacyUnverifiedChildren(ctx context.Context, ds *types.DataSource, item *types.FetchedItem, store *repository.FeishuLegacyStore) error {
	children, err := s.repo.FindByMetadataKeyPrefix(ctx, ds.TenantID, ds.KnowledgeBaseID, "external_id", types.SubtreeChildPrefix(item.ExternalID))
	if err != nil {
		return err
	}
	held := false
	for _, child := range children {
		meta := child.GetMetadata()
		if meta["datasource_id"] != ds.ID {
			continue
		}
		if item.ReplacesSubtree && slices.Contains(item.SubtreeKeep, meta["external_id"]) {
			continue
		}
		if _, err := store.RestrictSource(ctx, ds, meta["external_id"], false); err != nil {
			return err
		}
		held = true
	}
	if held {
		return types.ErrLegacySourceRestricted
	}
	return nil
}

// StageFeishuLegacyItem is synchronous at the cursor boundary, but its private
// database artifacts survive retries/crashes. It does not call ReparseKnowledge
// or dispatch the unsafe stable-KID delete/recreate pipeline.
func (s *knowledgeService) StageFeishuLegacyItem(ctx context.Context, ds *types.DataSource, item *types.FetchedItem, tagIDs []string) (receipt *types.FeishuLegacyReceipt, err error) {
	if !safeLegacyIncrementalEnabled() || !legacyWikiDrive(ds.Type) || len(item.Content) == 0 || item.IsDeleted || item.ExternalID == "" {
		return nil, ErrFeishuStagingRequired
	}
	if datasource.LegacyFeishuSyncSuspended(ds.Type) {
		return nil, datasource.ErrLegacyFeishuSyncSuspended
	}
	if s.config == nil || strings.TrimSpace(os.Getenv("RETRIEVE_DRIVER")) != "postgres" {
		return nil, ErrFeishuStagingRequired
	}
	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, ds.KnowledgeBaseID)
	if err != nil {
		return nil, err
	}
	if kb.TenantID != ds.TenantID || kb.Type != types.KnowledgeBaseTypeDocument || kb.VectorStoreID != nil || !kb.NeedsEmbeddingModel() {
		return nil, ErrFeishuStagingRequired
	}
	store, err := repository.FeishuLegacyStoreFor(s.repo)
	if err != nil {
		return nil, err
	}
	fileSvc, storageID, storageStamp, err := s.legacyCandidateStorage(ctx, kb, store)
	if err != nil {
		if errors.Is(err, ErrFeishuStagingRequired) {
			if denyErr := store.SetSourceUncertain(ctx, ds, true); denyErr != nil {
				return nil, errors.Join(err, denyErr)
			}
		}
		return nil, err
	}
	// Old attachment rows cannot outlive a now-missing/unverifiable parent
	// dependency. Quarantine, never invoke the old physical subtree sweep.
	if err := s.restrictLegacyUnverifiedChildren(ctx, ds, item, store); err != nil {
		return nil, err
	}
	// Root traversal location is not document content. A move between roots
	// already authorized by this same datasource must not force re-embedding.
	// Datasource configuration and the source-owned identity stay fenced.
	sourceBytes, err := json.Marshal([]any{item.Content, item.FileName, item.Title})
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(sourceBytes)
	resource, attempt, err := store.Begin(ctx, ds, item, hex.EncodeToString(h[:]))
	if err != nil {
		return nil, err
	}
	if attempt.Status == "published" {
		receipt, err = store.Receipt(ctx, resource, attempt)
		if err != nil {
			return nil, err
		}
		return receipt, types.NewDuplicateFileError(&types.Knowledge{ID: resource.KnowledgeID})
	}
	defer func() {
		if err != nil {
			clean, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_ = store.Abandon(clean, attempt)
		}
	}()
	fileType := getFileType(item.FileName)
	if !isValidFileType(item.FileName) {
		return nil, ErrInvalidFileType
	}
	var overrides *types.KnowledgeProcessOverrides
	meta := map[string]string{}
	existing, loadErr := s.repo.GetKnowledgeByID(ctx, ds.TenantID, resource.KnowledgeID)
	if resource.State == "hold" && len(resource.QuarantineSnapshot) > 2 {
		var private struct {
			Knowledge types.Knowledge `json:"knowledge"`
		}
		if e := json.Unmarshal(resource.QuarantineSnapshot, &private); e != nil {
			return nil, e
		}
		if private.Knowledge.ID == resource.KnowledgeID {
			existing = &private.Knowledge
			loadErr = nil
		}
	}
	if loadErr == nil {
		if !s.legacyPreviousFileIsControlled(ctx, kb, existing.FilePath) {
			if denyErr := store.SetSourceUncertain(ctx, ds, true); denyErr != nil {
				return nil, denyErr
			}
			return nil, ErrFeishuStagingRequired
		}
		overrides, err = existing.ProcessOverrides()
		if err != nil {
			return nil, err
		}
		for k, v := range existing.GetMetadata() {
			meta[k] = v
		}
	} else if !errors.Is(loadErr, repository.ErrKnowledgeNotFound) {
		return nil, loadErr
	}
	eff, err := resolveFileImportProcessConfig(ctx, kb, fileType, overrides, nil)
	if err != nil {
		return nil, err
	}
	// Parent/child and asynchronous media/enrichment require a versioned
	// derivative contract; unsupported configurations remain explicitly closed.
	if eff.ChunkingConfig.EnableParentChild {
		return nil, ErrFeishuStagingRequired
	}
	engine := eff.ChunkingConfig.ResolveParserEngine(fileType)
	parserOverrides := MergeParserEngineOverrides(s.getParserEngineOverridesFromContext(ctx), nil)
	if overrides != nil {
		parserOverrides = MergeParserEngineOverrides(parserOverrides, overrides.ParserEngineOverrides)
	}
	applyParserRuleOverrides(parserOverrides, eff.ChunkingConfig, fileType)
	if err = validateParserEngineOverrideURLs(parserOverrides); err != nil {
		return nil, err
	}
	reader := s.resolveDocReader(ctx, engine, fileType, false, parserOverrides)
	if reader == nil {
		return nil, errors.New("Feishu candidate parser unavailable")
	}
	if err = store.CheckOwner(ctx, ds, attempt); err != nil {
		return nil, err
	}
	parsed, err := s.callDocReaderWithTimeout(ctx, reader, &types.ReadRequest{FileContent: item.Content, FileName: item.FileName, FileType: fileType, Title: item.Title, ParserEngine: engine, ParserEngineOverrides: parserOverrides, RequestID: attempt.ID, ParserSemanticChunkMaxChars: types.DefaultParserSemanticChunkMaxChars})
	if err != nil {
		return nil, err
	}
	if parsed == nil || parsed.Error != "" || parsed.IsAudio || len(parsed.ImageRefs) > 0 || strings.TrimSpace(parsed.MarkdownContent) == "" {
		return nil, ErrFeishuStagingRequired
	}
	sanitizeReadResult(parsed)
	// Reuse the production parser span validation and segment boundaries.
	segments, segmented, err := materializeParserDefinedSegments(parsed, eff.ChunkingConfig.ParserSemanticChunkMaxChars)
	if err != nil {
		return nil, err
	}
	var parts []types.ParsedChunk
	if segmented {
		parts, _, err = splitMaterializedParserSegments(segments, buildSplitterConfigFromChunking(eff.ChunkingConfig), false, chunker.SplitterConfig{}, chunker.SplitterConfig{})
		if err != nil {
			return nil, err
		}
	} else {
		var controlled bool
		parts, controlled, err = materializeParserDefinedChunks(parsed, eff.ChunkingConfig.ParserSemanticChunkMaxChars)
		if err != nil {
			return nil, err
		}
		if !controlled {
			for _, p := range chunker.Split(parsed.MarkdownContent, buildSplitterConfigFromChunking(eff.ChunkingConfig)) {
				parts = append(parts, types.ParsedChunk{Content: p.Content, ContextHeader: p.ContextHeader, Seq: p.Seq, Start: p.Start, End: p.End})
			}
		}
	}
	if len(parts) == 0 || len(parts) > 10000 {
		return nil, ErrFeishuStagingRequired
	}
	chunks := make([]*types.Chunk, len(parts))
	headers := map[string]string{}
	texts := make([]string, len(parts))
	for i, p := range parts {
		chunks[i] = &types.Chunk{ID: uuid.NewString(), TenantID: ds.TenantID, KnowledgeID: resource.KnowledgeID, KnowledgeBaseID: kb.ID, Content: p.Content, ChunkIndex: i, ChunkType: types.ChunkTypeText, IsEnabled: true, IndexStatus: "ready", Status: int(types.ChunkStatusIndexed), StartAt: p.Start, EndAt: p.End, Metadata: types.JSON(`{}`), RelationChunks: types.JSON(`[]`), IndirectRelationChunks: types.JSON(`[]`)}
		if len(tagIDs) == 1 {
			chunks[i].TagID = tagIDs[0]
		}
		chunks[i].ContextHeader = p.ContextHeader
		chunks[i].SourceLocators = p.SourceLocators
		headers[chunks[i].ID] = p.ContextHeader
		texts[i] = item.Title + "\n\n" + chunks[i].EmbeddingContent()
	}
	for i, c := range chunks {
		if i > 0 {
			c.PreChunkID = chunks[i-1].ID
		}
		if i+1 < len(chunks) {
			c.NextChunkID = chunks[i+1].ID
		}
	}
	stamp, err := store.ModelStamp(ctx, kb.EmbeddingModelID)
	if err != nil {
		return nil, err
	}
	model, err := s.modelService.GetEmbeddingModelForTenant(ctx, kb.EmbeddingModelID, ds.TenantID)
	if err != nil {
		return nil, err
	}
	if err = store.CheckOwner(ctx, ds, attempt); err != nil {
		return nil, err
	}
	vectors, err := model.BatchEmbed(ctx, texts)
	if err != nil {
		return nil, err
	}
	form, file, err := feishuFile(item.Content, item.FileName)
	if err != nil {
		return nil, err
	}
	defer form.RemoveAll()
	// Candidate path is disjoint even when logical KID/filename are identical.
	path, err := fileSvc.SaveFile(ctx, file, ds.TenantID, attempt.ID)
	if err != nil {
		return nil, err
	}
	md5hash, err := calculateFileHash(file)
	if err != nil {
		return nil, err
	}
	for k, v := range item.Metadata {
		meta[k] = v
	}
	meta["datasource_id"] = ds.ID
	meta["external_id"] = item.ExternalID
	meta["source_resource_id"] = item.SourceResourceID
	meta["source_updated_at"] = item.UpdatedAt.UTC().Format(time.RFC3339Nano)
	meta["feishu_legacy_attempt"] = attempt.ID
	b, err := json.Marshal(meta)
	if err != nil {
		return nil, err
	}
	k := types.Knowledge{ID: resource.KnowledgeID, TenantID: ds.TenantID, KnowledgeBaseID: kb.ID, Type: "file", Title: item.Title, Source: item.URL, Channel: ds.Type, FileName: item.FileName, FileType: fileType, FileSize: int64(len(item.Content)), FileHash: md5hash, FilePath: path, EmbeddingModelID: kb.EmbeddingModelID, Metadata: types.JSON(b)}
	m := types.FeishuLegacyManifest{Knowledge: k, Chunks: chunks, ChunkHeaders: headers, EmbeddingModelID: kb.EmbeddingModelID, KBConfigSHA256: repository.LegacyKBConfigHash(kb), ModelStamp: stamp, Dimension: model.GetDimensions()}
	m.StorageBackendID = storageID
	m.StorageStamp = storageStamp
	if err = store.Prepare(ctx, ds, attempt, m, vectors); err != nil {
		return nil, err
	}
	if _, err = store.Publish(ctx, ds, attempt); err != nil {
		return nil, err
	}
	// Only a durable, re-read, currently active receipt admits the Sync Cursor.
	return store.Receipt(ctx, resource, attempt)
}
