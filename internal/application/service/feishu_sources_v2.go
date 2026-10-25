package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// EnableImportSource enrolls only an already indexed V2 import in the existing
// safe datasource engine. It preserves the Knowledge ID and index rows. The
// temporary paused state prevents a scheduler racing the metadata handoff.
func (s *FeishuPersonalService) EnableImportSource(ctx context.Context, kbID, importID, schedule string) (*types.DataSource, error) {
	user, ok := types.UserIDFromContext(ctx)
	if !ok {
		return nil, access.ErrUnauthorized
	}
	if s.dataSources == nil || s.recovery == nil || !safeLegacyIncrementalEnabled() || !applicationGrantEngineAvailable() {
		return nil, ErrFeishuStagingRequired
	}
	if schedule == "" {
		schedule = "*/15 * * * *"
	}
	if err := datasource.ValidateSyncSchedule(schedule); err != nil {
		return nil, err
	}
	kb, err := s.kbs.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return nil, err
	}
	if err = access.RequireKBWrite(ctx, kb); err != nil {
		return nil, err
	}
	var m types.FeishuPersonalImport
	if err = s.db.WithContext(ctx).Where("id = ? AND user_id = ? AND kb_id = ?", importID, user, kbID).First(&m).Error; err != nil {
		return nil, access.ErrNotFound
	}
	if m.Status == "paused" || m.Status == "hold" || m.Status == "revoked" {
		return nil, access.ErrForbidden
	}
	var existing types.DataSource
	existingErr := s.db.WithContext(ctx).Where("id = ?", m.ID).First(&existing).Error
	if existingErr == nil {
		if existing.KnowledgeBaseID != kbID || existing.TenantID != kb.TenantID {
			return nil, access.ErrForbidden
		}
		if err := checkFeishuSourceOwner(ctx, &existing); err != nil {
			return nil, err
		}
		if m.Status == "source_managed" {
			return &existing, nil // Never auto-resume an existing paused/held source.
		}
		if existing.LegacyAccessState == "hold" || existing.LegacyAccessState == "revoked" || existing.Status != types.DataSourceStatusPaused {
			return nil, access.ErrForbidden
		}
	}
	if existingErr != nil && !errors.Is(existingErr, gorm.ErrRecordNotFound) {
		return nil, existingErr
	}
	rows, err := s.decorateFeishuImports(ctx, []types.FeishuPersonalImport{m}, nil)
	if err != nil {
		return nil, err
	}
	m = rows[0]
	if m.KnowledgeID == "" || m.IndexStatus != "verified" || m.Status != "synced" && m.Status != "completed" {
		return nil, errors.New("indexed_import_required")
	}
	_, _, credential, err := s.ResolveCredentialReference(ctx, types.FeishuAuthMode(m.AuthMode), "")
	if err != nil {
		return nil, err
	}

	ref := types.FeishuSourceReference{Version: 1, OwnerUserID: user, Kind: credential.Kind, ConnectionID: credential.ConnectionID, AppID: credential.AppID, IdentityVersion: credential.ConfigVersion}
	config := &types.DataSourceConfig{Credentials: map[string]interface{}{}, ResourceIDs: []string{"links"}, Settings: map[string]interface{}{"credential_reference": ref, "links": []string{m.URL}}}
	blob, err := config.ToJSON()
	if err != nil {
		return nil, err
	}
	ds := &types.DataSource{ID: m.ID, TenantID: kb.TenantID, KnowledgeBaseID: kbID, Name: "Feishu " + m.ResourceType, Type: types.ConnectorTypeFeishu, Config: blob, SyncSchedule: schedule, SyncMode: types.SyncModeIncremental, Status: types.DataSourceStatusPaused, ConflictStrategy: types.ConflictStrategyOverwrite, SyncDeletions: false}
	if m.SourceModified == "" {
		return nil, errors.New("indexed_revision_required")
	}
	// Seed only this newly created source from the verified first-import
	// revision. Existing source cursors are never cleared or overwritten.
	ds.LastSyncCursor, err = (core.ReferenceLinkOps{}).EncodeCursor(map[string]map[string]string{"links": {m.ResourceType + ":" + m.DocumentToken: m.SourceModified}}, time.Now()).ToJSON()
	if err != nil {
		return nil, err
	}
	if existingErr == nil {
		ds = &existing // Retry the fenced handoff without changing its cursor.
	} else {
		ds, err = s.dataSources.CreateDataSource(ctx, ds)
		if err != nil {
			return nil, err
		}
	}
	var k types.Knowledge
	if err = s.db.WithContext(ctx).Where("id = ? AND knowledge_base_id = ?", m.KnowledgeID, kbID).First(&k).Error; err != nil {
		return nil, err
	}
	var metadata map[string]any
	if json.Unmarshal(k.Metadata, &metadata) != nil || metadata["feishu_imported_by"] != user || metadata["external_id"] != m.ResourceType+":"+m.DocumentToken {
		return nil, access.ErrForbidden
	}
	if old := metadata["datasource_id"]; old != "personal:"+m.ID && old != m.ID {
		return nil, errors.New("source_binding_conflict")
	}
	metadata["datasource_id"] = m.ID
	updated, _ := json.Marshal(metadata)
	if err = s.recovery.repo.HandoffSource(ctx, m.ID, user, func(tx *gorm.DB, current *types.FeishuPersonalImport) error {
		if current.KnowledgeID != k.ID || current.SourceModified != m.SourceModified {
			return errors.New("source_revision_changed")
		}
		// "synced" is a computed view; the persisted first-create state is
		// "indexed". Recheck the real receipt and Wiki completion in the
		// handoff transaction instead of expecting the display status in SQL.
		transactionService := *s
		transactionService.db = tx
		transactionRecovery := *s.recovery
		transactionRecovery.service = &transactionService
		verified := []types.FeishuPersonalImport{*current}
		if e := transactionRecovery.firstCreateStatuses(tx.Statement.Context, verified); e != nil {
			return e
		}
		if verified[0].IndexStatus != "verified" || verified[0].Status != "synced" {
			return errors.New("indexed_import_required")
		}
		change := tx.Model(&types.Knowledge{}).Where("id = ? AND file_hash = ? AND metadata = ?", k.ID, k.FileHash, k.Metadata).Update("metadata", types.JSON(updated))
		if change.Error != nil {
			return change.Error
		}
		if change.RowsAffected != 1 {
			return errors.New("source_binding_changed")
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err = s.dataSources.ResumeDataSource(ctx, ds.ID); err != nil {
		return nil, err
	}
	return s.dataSources.GetDataSource(ctx, ds.ID)
}

func (s *FeishuPersonalService) ImportSourceAction(ctx context.Context, kbID, id, action string) error {
	user, ok := types.UserIDFromContext(ctx)
	if !ok {
		return access.ErrUnauthorized
	}
	if s.dataSources == nil {
		return ErrFeishuStagingRequired
	}
	kb, err := s.kbs.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return err
	}
	if err = access.RequireKBWrite(ctx, kb); err != nil {
		return err
	}
	var m types.FeishuPersonalImport
	if err = s.db.WithContext(ctx).Where("id = ? AND user_id = ? AND kb_id = ? AND status = ?", id, user, kbID, "source_managed").First(&m).Error; err != nil {
		return access.ErrNotFound
	}
	ds, err := s.dataSources.GetDataSource(ctx, id)
	if err != nil {
		return err
	}
	if ds.KnowledgeBaseID != kbID || ds.TenantID != kb.TenantID {
		return access.ErrForbidden
	}
	if err = checkFeishuSourceOwner(ctx, ds); err != nil {
		return err
	}
	if action == "pause" {
		return s.dataSources.PauseDataSource(ctx, id)
	}
	if ds.LegacyAccessState == "hold" || ds.LegacyAccessState == "revoked" {
		return access.ErrForbidden
	}
	switch action {
	case "sync":
		if ds.Status == types.DataSourceStatusPaused {
			return errors.New("source_paused")
		}
		_, err = s.dataSources.ManualSync(ctx, id)
		return err
	case "resume":
		if impl, ok := s.dataSources.(*DataSourceService); ok {
			if _, err = impl.bindFeishuSource(ctx, ds); err != nil {
				return err
			}
		}
		return s.dataSources.ResumeDataSource(ctx, id)
	case "rebind":
		config, e := ds.ParseConfig()
		if e != nil {
			return e
		}
		old, e := types.ParseFeishuSourceReference(config)
		if e != nil || old == nil {
			return types.ErrFeishuIdentityReference
		}
		_, _, ref, e := s.ResolveCredentialReference(ctx, old.Kind, "")
		if e != nil {
			return e
		}
		if ref.AppID != old.AppID {
			return types.ErrFeishuIdentityReference
		}
		config.Settings["credential_reference"] = types.FeishuSourceReference{Version: 1, OwnerUserID: user, Kind: ref.Kind, ConnectionID: ref.ConnectionID, AppID: ref.AppID, IdentityVersion: ref.ConfigVersion}
		ds.Config, e = config.ToJSON()
		if e != nil {
			return e
		}
		_, e = s.dataSources.UpdateDataSource(ctx, ds)
		return e // Rebinding never resumes the source.
	default:
		return errors.New("invalid_source_action")
	}
}
