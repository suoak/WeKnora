package service

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
)

// A canonical KB viewer who is a live member of the owning workspace can see
// file catalog metadata, even while processing/source-content access is closed.
// This is not a source proof and never grants detail, file, chunk or model access.
func (p *PersonalRecovery) exactPageMetadata(
	ctx context.Context, tenant uint64, kb string, ids []string,
) ([]*types.Knowledge, error) {
	caller := types.CallerFromContext(ctx)
	principal, ok := ctx.Value(types.PrincipalContextKey).(types.Principal)
	effective, valid := types.TenantIDFromContext(ctx)
	if !ok || principal.Type != types.PrincipalWebUser || principal.ID != caller.UserID ||
		caller.UserID == "" || caller.TenantID != tenant || !valid || effective != tenant {
		return nil, access.ErrForbidden
	}
	owner, err := p.loadAuthorizationKB(ctx, kb)
	if err != nil || owner.TenantID != tenant {
		return nil, access.ErrForbidden
	}
	if _, err := access.ResolveKB(ctx, access.KBRequest{Caller: caller}, owner,
		types.OrgRoleViewer, p.shares, nil); err != nil {
		return nil, err
	}
	memberCheck := func() error {
		var member bool
		if err := p.service.db.WithContext(ctx).Raw(`SELECT EXISTS(
SELECT 1 FROM tenant_members m JOIN users u ON u.id=m.user_id
WHERE m.user_id=? AND m.tenant_id=? AND m.role=? AND m.status='active'
AND m.deleted_at IS NULL AND u.is_active AND u.deleted_at IS NULL)`,
			caller.UserID, tenant, caller.Role).Scan(&member).Error; err != nil {
			return err
		}
		if !member {
			return access.ErrForbidden
		}
		return nil
	}
	if err := memberCheck(); err != nil {
		return nil, err
	}
	allowed, protected, err := access.GuardDocumentReadQuery(ctx, tenant, kb)
	if err != nil {
		return nil, err
	}
	if protected {
		set := make(map[string]bool, len(allowed))
		for _, id := range allowed {
			set[id] = true
		}
		for _, id := range ids {
			if !set[id] {
				return nil, access.ErrForbidden
			}
		}
	}
	type entry struct {
		ID, KnowledgeBaseID, FileName, FileType, Type, FolderPath string
		ParseStatus, SummaryStatus, EnableStatus, Channel         string
		ExecutionError                                            string
		TenantID                                                  uint64
		FileSize                                                  int64
		CreatedAt, UpdatedAt                                      time.Time
	}
	var entries []entry
	if len(ids) > 0 {
		// Scan a dedicated DTO. Do not SELECT *, instantiate a content entity,
		// or retrieve protected text and discard it after a failing read hook.
		err := p.service.db.WithContext(ctx).Table("knowledges k").Select(`
k.id,k.tenant_id,k.knowledge_base_id,k.file_name,k.file_type,k.type,k.folder_path,
k.file_size,k.parse_status,k.summary_status,k.enable_status,k.channel,k.created_at,k.updated_at,
CASE WHEN a.status='needs_attention' AND a.owner_nonce='' AND a.lease_until IS NULL
 THEN a.last_error_category ELSE '' END AS execution_error`).
			Joins("LEFT JOIN feishu_personal_resources r ON r.logical_knowledge_id=k.id").
			Joins("LEFT JOIN feishu_personal_attempts a ON a.id=r.current_attempt_id AND a.caller_tenant_id=k.tenant_id").
			Where("k.id IN ? AND k.tenant_id=? AND k.knowledge_base_id=? AND k.deleted_at IS NULL", ids, tenant, kb).
			Scan(&entries).Error
		if err != nil {
			return nil, err
		}
	}
	if len(entries) != len(ids) {
		return nil, errPersonalExactDocument
	}
	byID := make(map[string]*types.Knowledge, len(entries))
	for _, e := range entries {
		if _, duplicate := byID[e.ID]; duplicate {
			return nil, errPersonalExactDocument
		}
		row := &types.Knowledge{
			ID: e.ID, TenantID: e.TenantID, KnowledgeBaseID: e.KnowledgeBaseID,
			FileName: e.FileName, FileType: e.FileType, Type: e.Type, FolderPath: e.FolderPath,
			FileSize: e.FileSize, ParseStatus: e.ParseStatus, SummaryStatus: e.SummaryStatus,
			EnableStatus: e.EnableStatus, Channel: e.Channel, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
			MetadataOnly: true,
		}
		if e.ExecutionError == "exact_document_execution_requires_review" {
			// This category is written only by the fenced termination of an
			// actual Claim. Never label an unclaimed pending operation failed.
			row.ParseStatus = types.ParseStatusFailed
			row.ErrorMessage = "Processing stopped; original file and unpublished output are retained for review."
		}
		byID[e.ID] = row
	}
	rows := make([]*types.Knowledge, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, byID[id])
	}
	owner, err = p.loadAuthorizationKB(ctx, kb)
	if err != nil || owner.TenantID != tenant {
		return nil, access.ErrForbidden
	}
	if _, err := access.ResolveKB(ctx, access.KBRequest{Caller: caller}, owner,
		types.OrgRoleViewer, p.shares, nil); err != nil {
		return nil, err
	}
	if err := memberCheck(); err != nil {
		return nil, err
	}
	return rows, nil
}

// Batch selectors and status polling need catalog metadata, not a body grant.
func (s *knowledgeService) exactPublicMetadataBatch(
	ctx context.Context, tenant uint64, ids []string,
) ([]*types.Knowledge, bool, error) {
	if !exactPersonalRecoveryEnabled() || s.DocumentACLReadEnabled() || len(ids) == 0 {
		return nil, false, nil
	}
	p, ok := s.personalRecovery.(*PersonalRecovery)
	principal, web := ctx.Value(types.PrincipalContextKey).(types.Principal)
	caller := types.CallerFromContext(ctx)
	if !ok || !web || principal.Type != types.PrincipalWebUser || principal.ID != caller.UserID ||
		caller.UserID == "" || caller.TenantID != tenant {
		return nil, false, nil
	}
	unique := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !seen[id] {
			unique = append(unique, id)
			seen[id] = true
		}
	}
	var headers []struct{ ID, KnowledgeBaseID string }
	if err := p.service.db.WithContext(ctx).Table("knowledges").Select("id,knowledge_base_id").
		Where("id IN ? AND tenant_id=? AND deleted_at IS NULL", unique, tenant).Scan(&headers).Error; err != nil {
		return nil, true, err
	}
	if len(headers) != len(unique) {
		return nil, false, nil // Missing/foreign IDs retain the existing strict path.
	}
	groups := make(map[string][]string)
	var kbs []string
	for _, header := range headers {
		if _, exists := groups[header.KnowledgeBaseID]; !exists {
			kbs = append(kbs, header.KnowledgeBaseID)
		}
		groups[header.KnowledgeBaseID] = append(groups[header.KnowledgeBaseID], header.ID)
	}
	var count int64
	if err := p.service.db.WithContext(ctx).Table("task_pending_ops").Where(
		"scope=? AND tenant_id=? AND payload->>'knowledge_base_id' IN ?",
		types.PersonalExactReceiptScope, tenant, kbs).Count(&count).Error; err != nil {
		return nil, true, err
	}
	if count == 0 {
		return nil, false, nil
	}
	if len(unique) > 256 {
		return nil, true, errPersonalExactDocument
	}
	byID := make(map[string]*types.Knowledge, len(unique))
	for kb, group := range groups {
		rows, err := p.exactPageMetadata(ctx, tenant, kb, group)
		if err != nil {
			return nil, true, err
		}
		for _, row := range rows {
			byID[row.ID] = row
		}
	}
	rows := make([]*types.Knowledge, 0, len(unique))
	for _, id := range unique {
		rows = append(rows, byID[id])
	}
	return rows, true, nil
}
