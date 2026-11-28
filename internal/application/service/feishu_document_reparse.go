package service

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/feishusynccontrol"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

type documentReparseRunKey struct{}

func (s *DataSourceService) documentReparseAuthority(
	ctx context.Context, ds *types.DataSource, actor string,
) error {
	if s.feishuIdentity == nil || s.feishuIdentity.db == nil {
		return apperrors.NewServiceUnavailableError("Document reparse authorization is unavailable")
	}
	authority, err := feishusynccontrol.SourceManagementFromDatabase(
		ctx,
		s.feishuIdentity.db,
		ds.ID,
		actor,
	)
	if err != nil {
		if errors.Is(err, feishusynccontrol.ErrRecoveryRejected) {
			return apperrors.NewForbiddenError("Document reparse permission denied")
		}
		return apperrors.NewServiceUnavailableError("Document reparse authorization is unavailable")
	}
	if authority.TenantID != ds.TenantID || authority.KBID != ds.KnowledgeBaseID ||
		authority.AccessState != "active" || !legacyWikiDrive(ds.Type) || !safeLegacyIncrementalEnabled() ||
		(ds.Status != types.DataSourceStatusActive && ds.Status != types.DataSourceStatusError) {
		return apperrors.NewConflictError(
			"The source must be active and verified before document reparsing",
		)
	}
	config, err := ds.ParseConfig()
	if err != nil {
		return err
	}
	ref, err := types.ParseFeishuSourceReference(config)
	if err != nil {
		return err
	}
	if ref != nil && ref.OwnerUserID != actor {
		return apperrors.NewForbiddenError("Document reparse requires the source owner")
	}
	return nil
}

// RequestDocumentReparse accepts one selected document without altering its published version.
func (s *DataSourceService) RequestDocumentReparse(
	ctx context.Context, source, kid string, overrides *types.KnowledgeProcessOverrides,
) (*types.FeishuDocumentReparseStatus, error) {
	caller := types.CallerFromContext(ctx)
	principal, valid := ctx.Value(types.PrincipalContextKey).(types.Principal)
	if !valid || principal.Type != types.PrincipalWebUser || principal.ID == "" ||
		principal.ID != caller.UserID {
		return nil, apperrors.NewForbiddenError("Document reparse requires an authenticated editor")
	}
	ds, err := s.GetDataSource(ctx, source)
	if err != nil || ds == nil || ds.TenantID != caller.TenantID {
		return nil, apperrors.NewForbiddenError("Document reparse permission denied")
	}
	if err := s.documentReparseAuthority(ctx, ds, caller.UserID); err != nil {
		return nil, err
	}
	connector, err := s.connectorRegistry.Get(ds.Type)
	if err != nil {
		return nil, err
	}
	if _, ok := connector.(datasource.DocumentConnector); !ok {
		return nil, apperrors.NewServiceUnavailableError(
			"The source does not support document reparsing",
		)
	}
	store, err := repository.FeishuLegacyStoreFor(s.knowledgeService.GetRepository())
	if err != nil {
		return nil, err
	}
	request, err := store.SourceReparseTarget(ctx, ds, kid)
	if err != nil {
		return nil, err
	}
	config, err := ds.ParseConfig()
	if err != nil || config == nil || !slices.Contains(config.ResourceIDs, request.RootID) {
		return nil, apperrors.NewConflictError(
			"Document binding is outside the configured source selection",
		)
	}
	kb, err := s.kbService.GetKnowledgeBaseByID(
		types.WithLegacyKBAdminMetadata(ctx, ds.KnowledgeBaseID),
		ds.KnowledgeBaseID,
	)
	if err != nil || kb == nil || kb.TenantID != ds.TenantID {
		return nil, apperrors.NewForbiddenError("Document reparse permission denied")
	}
	if err := validateSourceDocumentReparseConfig(ctx, kb, request.FileType, overrides); err != nil {
		return nil, err
	}
	request.Initiator = types.TaskInitiatorFromContext(ctx)
	if request.Initiator.UserID != caller.UserID {
		return nil, apperrors.NewForbiddenError(
			"Document reparse initiator does not match the authenticated editor",
		)
	}
	request.ProcessConfig, request.KBConfigHash = overrides, repository.LegacyKBConfigHash(kb)
	status, err := store.QueueSourceDocumentReparse(ctx, ds, request)
	if err != nil {
		return nil, err
	}
	// A delivery failure cannot lose the accepted SQL intent. The independent
	// recovery sweep re-admits it through this same normal source queue.
	if _, err := s.ManualSync(context.WithValue(ctx, documentReparseRunKey{}, true), ds.ID); err != nil {
		logger.Warnf(
			ctx,
			"Document reparse wake-up deferred source=%s request=%s",
			ds.ID,
			status.ID,
		)
	}
	return status, nil
}

func validateSourceDocumentReparseConfig(
	ctx context.Context,
	kb *types.KnowledgeBase,
	fileType string,
	overrides *types.KnowledgeProcessOverrides,
) error {
	if overrides != nil && (overrides.SummaryEnabled != nil || overrides.GraphEnabled != nil ||
		overrides.QuestionGenerationConfig != nil || overrides.ASRConfig != nil || overrides.ExtractConfig != nil ||
		overrides.ImageAttrsEnabled != nil || overrides.ImageActions != nil) {
		return apperrors.NewBadRequestError(
			"Source document reparsing supports parser, chunking and VLM settings",
		)
	}
	_, err := resolveFileImportProcessConfig(ctx, kb, fileType, overrides, nil)
	return err
}

// DocumentReparseStatus exposes no connector identity, content, or process configuration.
func (s *DataSourceService) DocumentReparseStatus(
	ctx context.Context, source, kid string,
) (*types.FeishuDocumentReparseStatus, error) {
	ds, err := s.GetDataSource(ctx, source)
	if err != nil || ds == nil || ds.TenantID != types.CallerFromContext(ctx).TenantID {
		return nil, apperrors.NewForbiddenError("Document reparse permission denied")
	}
	if s.feishuIdentity == nil || s.feishuIdentity.db == nil {
		return nil, apperrors.NewServiceUnavailableError(
			"Document reparse authorization is unavailable",
		)
	}
	if _, err := feishusynccontrol.SourceManagementFromDatabase(ctx, s.feishuIdentity.db, ds.ID,
		types.CallerFromContext(ctx).UserID); err != nil {
		return nil, apperrors.NewForbiddenError("Document reparse permission denied")
	}
	store, err := repository.FeishuLegacyStoreFor(s.knowledgeService.GetRepository())
	if err != nil {
		return nil, err
	}
	return store.SourceDocumentReparseStatus(ctx, ds, kid)
}

func (s *DataSourceService) drainSourceDocumentReparse(
	ctx context.Context, ds *types.DataSource, config *types.DataSourceConfig,
	tagIDs []string, result *types.SyncResult,
) (bool, error) {
	if !legacyWikiDrive(ds.Type) || !safeLegacyIncrementalEnabled() || s.feishuIdentity == nil {
		return false, nil
	}
	if config == nil {
		return false, errors.New("source document reparse configuration unavailable")
	}
	connector, err := s.connectorRegistry.Get(ds.Type)
	if err != nil {
		return false, err
	}
	fetcher, ok := connector.(datasource.DocumentConnector)
	if !ok {
		return false, nil
	}
	store, err := repository.FeishuLegacyStoreFor(s.knowledgeService.GetRepository())
	if err != nil {
		return false, err
	}
	work, request, err := store.ClaimSourceDocumentReparse(ctx, ds)
	if err != nil || request == nil {
		return false, err
	}
	err = s.documentReparseAuthority(work, ds, request.Initiator.UserID)
	if err == nil {
		var kb *types.KnowledgeBase
		kb, err = s.kbService.GetKnowledgeBaseByID(
			types.WithLegacyKBAdminMetadata(work, ds.KnowledgeBaseID),
			ds.KnowledgeBaseID,
		)
		if err == nil && (kb == nil || kb.TenantID != ds.TenantID ||
			request.KBConfigHash != repository.LegacyKBConfigHash(kb)) {
			err = &repository.LegacyFenceError{Reason: "kb_configuration_changed"}
		}
	}
	var item *types.FetchedItem
	if err == nil {
		item, err = fetcher.FetchDocument(work, config, request.RootID, request.ExternalID)
		if err != nil {
			failed := &types.FetchedItem{
				ExternalID: request.ExternalID, SourceResourceID: request.RootID,
				Metadata: core.FeishuErrorItemMeta(
					err,
					map[string]string{"failure_stage": "fetch_document"},
				),
			}
			if legacyItemNeedsHold(failed) {
				if restrictErr := s.restrictFailedFeishuItem(work, ds, failed); restrictErr != nil {
					return true, restrictErr
				}
			}
		}
	}
	if err == nil {
		_, err = s.ingestItem(withKBActivitySuppressed(work), ds, item, tagIDs)
	}
	if result != nil {
		result.Total++
	}
	if err == nil {
		if result != nil {
			result.Updated++
		}
		return true, nil
	}
	if checkErr := s.checkSyncExecution(work); checkErr != nil {
		return true, checkErr
	}
	if checkErr := feishusafety.Check(work); checkErr != nil {
		return true, checkErr
	}
	var progress *repository.FeishuEmbeddingProgress
	if errors.As(err, &progress) {
		return true, store.YieldSourceDocumentReparse(work, ds, progress)
	}
	code, _, permanent := classifyKnowledgeParseFailure(err)
	if local := feishuDocumentFailureCode(ds, &types.FetchedItem{ExternalID: request.ExternalID}, err); local != "" {
		code = local
	}
	switch code {
	case "safe_staging_parser_resource_limit", "safe_staging_parser_semantic_row_too_large",
		"safe_staging_invalid_xlsx_package", "safe_staging_chunk_count_out_of_bounds",
		"safe_staging_encrypted_file", "safe_staging_file_format_mismatch", "safe_staging_source_address_blocked":
		permanent = true
	}
	var fence *repository.LegacyFenceError
	if errors.As(err, &fence) {
		code, permanent = fence.Reason, true
	}
	if app, ok := apperrors.IsAppError(err); ok && app.HTTPCode < 500 {
		code, permanent = "source_permission_or_configuration_changed", true
	}
	if errors.Is(err, core.ErrPermission) || errors.Is(err, core.ErrNotFound) {
		code, permanent = "source_document_access_unavailable", true
	}
	failure := repository.WithSourceDocumentReparseFailure(work, err, errors.Is(err, feishusafety.ErrBudgetBusy))
	if err := store.FinishSourceDocumentReparse(failure, ds, code, !permanent,
		errors.Is(err, repository.ErrLegacyVersionBusy)); err != nil {
		return true, err
	}
	if result != nil {
		result.Failed++
		recordSyncError(result, types.SyncItemError{
			Code: code, Message: "Document reparse failed; inspect source access and processing limits",
		})
	}
	return true, nil
}

// RecoverSourceDocumentReparses rechecks live source authority for accepted intents.
// It restores only the original initiator; no JWT or web principal is fabricated.
func (s *DataSourceService) RecoverSourceDocumentReparses(ctx context.Context) error {
	if !safeLegacyIncrementalEnabled() || s.feishuIdentity == nil {
		return nil
	}
	store, err := repository.FeishuLegacyStoreFor(s.knowledgeService.GetRepository())
	if err != nil {
		return err
	}
	requests, err := store.PendingSourceDocumentReparses(ctx)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, request := range requests {
		if seen[request.DataSourceID] {
			continue
		}
		seen[request.DataSourceID] = true
		work, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		work = types.WithExecutionTenant(request.Initiator.Apply(work), request.TenantID)
		ds, readErr := s.GetDataSource(work, request.DataSourceID)
		missing := errors.Is(readErr, gorm.ErrRecordNotFound)
		if missing || (readErr == nil && ds != nil && ds.TenantID != request.TenantID) {
			_ = store.RejectSourceDocumentReparse(work, &request, "source_unavailable")
			cancel()
			continue
		}
		if readErr == nil && ds != nil {
			authorityErr := s.documentReparseAuthority(work, ds, request.Initiator.UserID)
			if app, ok := apperrors.IsAppError(authorityErr); ok && app.HTTPCode < 500 {
				_ = store.RejectSourceDocumentReparse(
					work,
					&request,
					"source_permission_or_configuration_changed",
				)
				cancel()
				continue
			}
			if repository.LegacySourceReparseConfigHash(ds) != request.ConfigHash {
				_ = store.RejectSourceDocumentReparse(
					work,
					&request,
					"source_configuration_changed",
				)
				cancel()
				continue
			}
		}
		var wakeErr error
		if readErr != nil || ds == nil {
			wakeErr = readErr
			if wakeErr == nil {
				wakeErr = errors.New("document reparse source unavailable")
			}
		} else {
			var delivery context.Context
			delivery, wakeErr = store.PrepareSourceDocumentReparseDelivery(work, ds, &request)
			if wakeErr == nil {
				_, wakeErr = s.ManualSync(
					context.WithValue(delivery, documentReparseRunKey{}, true),
					request.DataSourceID,
				)
			}
		}
		cancel()
		if wakeErr != nil {
			logger.Warnf(ctx, "Document reparse recovery deferred source=%s", request.DataSourceID)
			deferCtx, deferCancel := context.WithTimeout(ctx, 200*time.Millisecond)
			_ = store.DeferSourceDocumentReparse(deferCtx, &request)
			deferCancel()
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return nil
}
