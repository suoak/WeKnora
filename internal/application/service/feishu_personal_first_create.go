package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
)

func (p *PersonalRecovery) processFirstCreateManifest(
	ctx context.Context,
	payload types.DocumentProcessPayload,
) error {
	m, r, a, err := p.repo.Load(ctx, payload.PersonalAttemptID)
	if err != nil {
		return err
	}
	if !applicationPayloadMatches(m, payload) || payload.KnowledgeID != a.CandidateVersionID ||
		payload.CandidateVersionID != a.CandidateVersionID ||
		payload.DispatchID != "personal-"+a.ID ||
		payload.TenantID != a.CallerTenantID ||
		payload.KnowledgeBaseID != m.KBID {
		return repository.ErrPersonalIdentity
	}
	ks := p.service.knowledge.(*knowledgeService)
	dispatch := ks.repo.(interfaces.KnowledgeDispatchRepository)
	return dispatch.WithKnowledgeDispatchLock(ctx, r.LogicalKnowledgeID, func(ctx context.Context) error {
		m, _, a, err := p.repo.Load(ctx, a.ID)
		if err != nil {
			return err
		}
		if _, err = p.authorize(ctx, m, a); err != nil {
			return err
		}
		if m.KnowledgeID != "" {
			return nil
		}
		if a.Status != "awaiting_index_isolation" {
			if err = p.processPrivateManifest(ctx, payload); err != nil {
				return err
			}
			m, _, a, err = p.repo.Load(ctx, a.ID)
			if err != nil {
				return err
			}
		}
		if a.Status != "awaiting_index_isolation" {
			return nil
		}
		if a.RetryAfter != nil && a.RetryAfter.After(time.Now()) {
			return nil
		}
		return p.bridgeFirstCreate(ctx, m, a)
	})
}

// The manifest download receipt remains distinct from this public admission.
// FileService is outside the transaction; ambiguous/unreferenced saved blobs
// are retained for ownership reconciliation, never deleted speculatively.
func (p *PersonalRecovery) bridgeFirstCreate(
	ctx context.Context,
	m *types.FeishuPersonalImport,
	a *types.FeishuPersonalAttempt,
) error {
	ks := p.service.knowledge.(*knowledgeService)
	remote, err := p.authorize(ctx, m, a)
	if err != nil {
		return err
	}
	manifest, err := p.store.read(a)
	if err != nil {
		return err
	}
	if manifest.Hash != a.ManifestHash || manifest.Size != a.ManifestSize ||
		manifest.Name != a.ManifestName {
		return repository.ErrPersonalIdentity
	}
	private, err := p.store.path(a.ManifestKey)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(private, "content"))
	if err != nil {
		return err
	}
	form, f, err := feishuFile(data, manifest.Name)
	if err != nil {
		return err
	}
	defer func(cleanup func() error) { _ = cleanup() }(form.RemoveAll)
	kb, err := ks.kbService.GetKnowledgeBaseByID(remote, m.KBID)
	if err != nil {
		return err
	}
	if kb.Type == types.KnowledgeBaseTypeFAQ || !isValidFileType(manifest.Name) {
		return ErrInvalidFileType
	}
	if _, err = resolveFileImportProcessConfig(remote, kb, getFileType(manifest.Name), nil, nil); err != nil {
		return err
	}
	fileSvc := ks.resolveFileService(remote, kb)
	if fileSvc == nil {
		return errors.New("personal_first_create_file_service_unavailable")
	}
	hash, err := calculateFileHash(f)
	if err != nil {
		return err
	}
	owner, err := p.repo.ClaimFirstCreate(ctx, a.ID, p.lease)
	if err != nil {
		return err
	}
	path, err := fileSvc.SaveFile(remote, f, kb.TenantID, a.LogicalKnowledgeID)
	if err != nil {
		return p.restoreBridge(ctx, owner, err)
	}
	meta := map[string]string{
		"feishu_auth_mode":       types.FeishuAuthMode(m.AuthMode),
		"feishu_auth_principal":  applicationPrincipalFingerprint(m.ApplicationID, a.AppConfigVersion),
		"source_url":             m.URL,
		"original_url":           m.URL,
		"feishu_resource_type":   m.ResourceType,
		"feishu_document_token":  m.DocumentToken,
		"feishu_resource_id":     m.ResourceType + ":" + m.DocumentToken,
		"feishu_imported_by":     m.UserID,
		"feishu_open_id":         m.OpenID,
		"feishu_source_modified": a.Revision,
		"feishu_coverage": "exported_content; embedded objects and attachments require " +
			"completeness review",
		"feishu_mvp_attempt":     a.ID,
		"feishu_manifest_sha256": manifest.Hash,
		"datasource_id":          "personal:" + m.ID,
		"external_id":            m.ResourceType + ":" + m.DocumentToken,
	}
	if types.FeishuAuthMode(m.AuthMode) == types.FeishuApplicationGrant {
		verified, err := applicationVerifiedTextHash(remote, data, manifest.Name)
		if err != nil {
			return p.restoreBridge(ctx, owner, err)
		}
		meta["feishu_verified_text_sha256"] = verified
		meta["feishu_coverage"] = "docx_text_and_tables_verified; images_attachments_embeds_not_open"
		if m.ResourceType == "sheet" || m.ResourceType == "bitable" {
			meta["feishu_coverage"] = "structured_api_snapshot_v1; full_read_compare; media_rejected"
			if len(manifest.Assets) > 0 {
				meta["feishu_coverage"] = "structured_api_snapshot_v1; full_read_compare; " +
					"attachments_persisted_and_parsed"
			}
		}
	}
	b, _ := json.Marshal(meta)
	k := &types.Knowledge{
		ID:               a.LogicalKnowledgeID,
		TenantID:         kb.TenantID,
		KnowledgeBaseID:  m.KBID,
		Type:             "file",
		Channel:          "feishu_personal",
		Source:           m.URL,
		Title:            manifest.Name,
		FileName:         manifest.Name,
		FileType:         getFileType(manifest.Name),
		FilePath:         path,
		FileSize:         manifest.Size,
		FileHash:         hash,
		EmbeddingModelID: kb.EmbeddingModelID,
		ParseStatus:      types.ParseStatusPending,
		EnableStatus:     "disabled",
		Metadata:         types.JSON(b),
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	if types.FeishuAuthMode(m.AuthMode) == types.FeishuApplicationGrant {
		// The parse/index pipeline must use the same parser as the verified export.
		// Preserve the KB's configuration and pin only this document.
		engine, fileType := "anydoc", "docx"
		if m.ResourceType == "sheet" || m.ResourceType == "bitable" {
			engine, fileType = "builtin", "md"
		}
		if err := k.SetProcessOverrides(
			&types.KnowledgeProcessOverrides{
				ParserEngineRules: []types.ParserEngineRule{
					{
						FileTypes: []string{
							fileType,
						},
						Engine: engine,
					},
				},
			},
		); err != nil {
			return p.restoreBridge(ctx, owner, err)
		}
	}
	op, err := ks.dispatchIntent(remote, k)
	if err != nil {
		return p.restoreBridge(ctx, owner, err)
	}
	var envelope types.KnowledgeDispatch
	if err = json.Unmarshal(op.Payload, &envelope); err != nil {
		return p.restoreBridge(ctx, owner, err)
	}
	envelope.PayloadVersion = publicPayloadVersion(m)
	if types.FeishuAuthMode(m.AuthMode) == types.FeishuApplicationGrant {
		op.TaskType = types.TypeFeishuApplicationProcess
		envelope.Task.FeishuAuthMode = m.AuthMode
		envelope.Task.FeishuApplicationID = m.ApplicationID
	}
	envelope.AttemptID = a.ID
	envelope.Revision = a.Revision
	envelope.Task.PersonalAttemptID = a.ID
	envelope.Task.CandidateVersionID = a.CandidateVersionID
	envelope.Task.FirstCreate = true
	op.Payload, err = json.Marshal(envelope)
	if err != nil {
		return p.restoreBridge(ctx, owner, err)
	}
	err = p.mutate(ctx, owner, 0, func(tx *gorm.DB, current *types.FeishuPersonalAttempt) error {
		if current.ManifestHash != manifest.Hash || current.LogicalKnowledgeID != k.ID ||
			current.CandidateVersionID == k.ID {
			return repository.ErrPersonalIdentity
		}
		if err := tx.Create(k).Error; err != nil {
			return err
		}
		if err := tx.Create(op).Error; err != nil {
			return err
		}
		bound := tx.Model(&types.FeishuPersonalImport{}).
			Where("id = ? AND knowledge_id = ?", m.ID, "").
			Updates(map[string]any{
				"knowledge_id":    k.ID,
				"source_modified": a.Revision,
				"coverage":        meta["feishu_coverage"],
				"status":          "index_pending",
			})
		if bound.Error != nil {
			return bound.Error
		}
		if bound.RowsAffected != 1 {
			return repository.ErrPersonalIdentity
		}
		current.Status = "index_pending"
		current.LeaseUntil = nil
		current.OwnerNonce = ""
		return nil
	})
	if err != nil {
		// COMMIT may have succeeded. Only an exact immutable ownership match
		// permits adopting the original Knowledge; a not-found is not cleanup.
		read, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		var saved types.Knowledge
		var pending types.TaskPendingOp
		if p.service.db.WithContext(read).
			Where("id = ? AND file_path = ? AND file_hash = ?", k.ID, path, hash).
			First(&saved).
			Error != nil ||
			p.service.db.WithContext(read).
				Where("scope = ? AND scope_id = ? AND dedup_key = ?", types.KnowledgeDispatchScope, k.ID, op.DedupKey).
				First(&pending).
				Error != nil {
			return p.restoreBridge(read, owner, err)
		}
		op = &pending
	}
	// Admission remains durable even when this best-effort wake-up fails.
	_ = ks.publishKnowledgeDispatch(ctx, op)
	return nil
}

func (p *PersonalRecovery) restoreBridge(
	ctx context.Context,
	owner *types.FeishuPersonalAttempt,
	cause error,
) error {
	read, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err := p.mutate(read, owner, 0, func(tx *gorm.DB, a *types.FeishuPersonalAttempt) error {
		a.Status = "awaiting_index_isolation"
		a.LeaseUntil = nil
		a.OwnerNonce = ""
		a.LastErrorCategory = "first_create_commit_or_file_unconfirmed"
		a.RetryCount++
		next := time.Now().Add(time.Duration(1<<min(a.RetryCount, 8)) * time.Second)
		a.RetryAfter = &next
		if a.RetryCount >= 8 {
			a.Status = "needs_attention"
			if err := tx.Model(
				&types.FeishuPersonalImport{},
			).
				Where(
					"id = ?",
					a.ImportID,
				).
				Update(
					"status",
					"needs_attention",
				).
				Error; err != nil {
				return err
			}
			return tx.Model(&types.TaskPendingOp{}).
				Where("scope = ? AND scope_id = ?", types.KnowledgeDispatchScope, a.CandidateVersionID).
				Update("op", "attention").
				Error
		}
		return nil
	})
	return errors.Join(cause, err)
}

func (p *PersonalRecovery) firstCreateDispatchable(
	ctx context.Context,
	intent types.KnowledgeDispatch,
) (bool, error) {
	m, r, a, err := p.repo.Load(ctx, intent.AttemptID)
	if err != nil {
		return false, err
	}
	t := intent.Task
	if !p.firstCreateOnly || intent.PayloadVersion != publicPayloadVersion(m) ||
		!applicationPayloadMatches(m, t) ||
		!t.FirstCreate ||
		t.PersonalAttemptID != a.ID ||
		t.CandidateVersionID != a.CandidateVersionID ||
		t.KnowledgeID != r.LogicalKnowledgeID ||
		m.KnowledgeID != r.LogicalKnowledgeID ||
		t.DispatchID != "document-"+r.LogicalKnowledgeID ||
		t.KnowledgeBaseID != m.KBID ||
		intent.Revision != a.Revision {
		return false, repository.ErrPersonalIdentity
	}
	ctx, err = p.privateWorkerContext(ctx, m, a)
	if err != nil {
		return false, err
	}
	var k types.Knowledge
	if err = p.service.db.WithContext(
		ctx,
	).
		Where(
			"id = ? AND knowledge_base_id = ? AND tenant_id = ?",
			t.KnowledgeID,
			t.KnowledgeBaseID,
			t.TenantID,
		).
		First(
			&k,
		).
		Error; err != nil {
		return false, err
	}
	var meta map[string]any
	if json.Unmarshal(k.Metadata, &meta) != nil || meta["feishu_mvp_attempt"] != a.ID ||
		meta["feishu_manifest_sha256"] != a.ManifestHash ||
		k.FilePath != t.FilePath ||
		k.FileName != t.FileName ||
		a.Status == "obsolete" ||
		a.Status == "needs_attention" {
		return false, repository.ErrPersonalIdentity
	}
	_, err = p.authorize(ctx, m, a)
	return err == nil, err
}

func (p *PersonalRecovery) processFirstCreateDocument(
	ctx context.Context,
	payload types.DocumentProcessPayload,
) error {
	ks := p.service.knowledge.(*knowledgeService)
	dispatch := ks.repo.(interfaces.KnowledgeDispatchRepository)
	return dispatch.WithKnowledgeDispatchLock(ctx, payload.KnowledgeID, func(ctx context.Context) error {
		op, err := dispatch.GetKnowledgeDispatch(ctx, payload.TenantID, payload.KnowledgeID)
		if err != nil {
			return err
		}
		var intent types.KnowledgeDispatch
		if err = json.Unmarshal(op.Payload, &intent); err != nil {
			return err
		}
		exact, _ := json.Marshal(intent.Task)
		supplied, _ := json.Marshal(payload)
		if string(exact) != string(supplied) {
			return repository.ErrPersonalIdentity
		}
		if op.Op == "completed" || op.Op == "obsolete" {
			return nil
		}
		ready, err := p.firstCreateDispatchable(ctx, intent)
		if err != nil || !ready {
			return err
		}
		if op.Op == "processed" {
			return ks.publishDurablePostProcess(ctx, op, payload)
		}
		if op.Op == "attention" {
			return ErrFeishuDispatchUnconfirmed
		}
		m, _, a, err := p.repo.Load(ctx, payload.PersonalAttemptID)
		if err != nil {
			return err
		}
		k, err := ks.repo.GetKnowledgeByID(ctx, payload.TenantID, payload.KnowledgeID)
		if err != nil {
			return err
		}
		chunks, err := ks.chunkRepo.ListAllChunksByKnowledgeID(ctx, k.TenantID, k.ID)
		if err != nil {
			return err
		}
		if len(chunks) > 0 || k.ParseStatus == types.ParseStatusCompleted ||
			k.ParseStatus == types.ParseStatusFinalizing {
			// Unknown partial/final results are not permission to run the
			// ordinary destructive cleanup again. Reconciler must read receipts.
			_ = dispatch.SetKnowledgeDispatchState(ctx, op.ID, "attention")
			return p.repo.Invalidate(ctx, a.ID, "partial_or_unknown_index_requires_review")
		}
		var indexRows int64
		if err = p.service.db.WithContext(
			ctx,
		).
			Table(
				"embeddings",
			).
			Where(
				"knowledge_id = ?",

				k.ID,
			).
			Count(
				&indexRows,
			).
			Error; err != nil {
			return err
		}
		if indexRows > 0 {
			_ = dispatch.SetKnowledgeDispatchState(ctx, op.ID, "attention")
			return p.repo.Invalidate(ctx, a.ID, "orphan_or_unknown_index_requires_review")
		}
		work, err := p.authorize(ctx, m, a)
		if err != nil {
			return err
		}
		owner, err := p.repo.Claim(ctx, a.ID, p.lease)
		if err != nil {
			return err
		}
		if err = dispatch.SetKnowledgeDispatchState(ctx, op.ID, "processing"); err != nil {
			return err
		}
		work, cancel := context.WithCancel(work)
		done, stopped := make(chan struct{}), make(chan struct{})
		go func() {
			defer close(stopped)
			ticker := time.NewTicker(p.lease / 3)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-work.Done():
					return
				case <-ticker.C:
					if _, e := p.authorize(work, m, a); e != nil {
						cancel()
						return
					}
					if e := p.mutate(work, owner, p.lease, nil); e != nil {
						cancel()
						return
					}
				}
			}
		}()
		defer func() { close(done); cancel(); <-stopped }()
		b, _ := json.Marshal(payload)
		processingContext := context.WithValue(work, knowledgeDispatchContextKey{}, op)
		processingContext = context.WithValue(processingContext, personalFirstCreateContextKey{}, true)
		err = ks.processDocument(processingContext, asynq.NewTask(types.TypeDocumentProcess, b))
		read, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		current, readErr := dispatch.GetKnowledgeDispatch(read, k.TenantID, k.ID)
		if err == nil && readErr == nil && current.Op == "processed" {
			receiptErr := p.recordFirstCreateIndex(read, m, owner)
			if receiptErr == nil {
				return nil
			}
			return errors.Join(receiptErr, p.repo.Invalidate(read, a.ID, "pg_index_receipt_unconfirmed"))
		}
		_ = dispatch.SetKnowledgeDispatchState(read, op.ID, "attention")
		return errors.Join(err, readErr, p.repo.Invalidate(read, a.ID, "parse_or_index_result_unconfirmed"))
	})
}

type personalFirstCreateContextKey struct{}

func isPersonalFirstCreate(ctx context.Context) bool {
	v, _ := ctx.Value(personalFirstCreateContextKey{}).(bool)
	return v
}

func firstCreateRecoveryPayload(
	m *types.FeishuPersonalImport,
	a *types.FeishuPersonalAttempt,
) types.DocumentProcessPayload {
	payload := types.DocumentProcessPayload{
		PersonalAttemptID:  a.ID,
		CandidateVersionID: a.CandidateVersionID,
		KnowledgeID:        a.CandidateVersionID,
		KnowledgeBaseID:    m.KBID,
		TenantID:           a.CallerTenantID,
		DispatchID:         "personal-" + a.ID,
		Attempt:            1,
	}
	if types.FeishuAuthMode(m.AuthMode) == types.FeishuApplicationGrant {
		payload.FeishuAuthMode = m.AuthMode
		payload.FeishuApplicationID = m.ApplicationID
	}
	return payload
}
