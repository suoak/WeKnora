package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrPersonalContentConflict = errors.New("idempotency_key already used with different content")

// PersonalContent has no dependency on the public search directory. Every
// admission reloads the credential, membership, private owner and editor share.
type PersonalContent struct {
	DB        *gorm.DB
	Keys      interfaces.UserMCPAPIKeyRepository
	Members   interfaces.TenantMemberService
	KBs       interfaces.KnowledgeBaseRepository
	Shares    interfaces.KBShareService
	Tenants   interfaces.TenantService
	Knowledge interfaces.KnowledgeService
	Employees access.EnterpriseEmployeeVerifier
	Audit     interfaces.AuditLogService
}

func NewPersonalContent(db *gorm.DB, keys interfaces.TenantAPIKeyRepository, members interfaces.TenantMemberService, kbs interfaces.KnowledgeBaseRepository, shares interfaces.KBShareService, tenants interfaces.TenantService, knowledge interfaces.KnowledgeService, portal *PortalMCPKnowledge, audit interfaces.AuditLogService) *PersonalContent {
	live, ok := keys.(interfaces.UserMCPAPIKeyRepository)
	if !ok || portal == nil || portal.Resolver == nil {
		return nil
	}
	return &PersonalContent{DB: db, Keys: live, Members: members, KBs: kbs, Shares: shares, Tenants: tenants, Knowledge: knowledge, Employees: portal.Resolver.Employees, Audit: audit}
}

type WritableKnowledgeBase struct {
	ID           string   `json:"knowledge_base_id"`
	Name         string   `json:"knowledge_base_name"`
	Space        string   `json:"space_name"`
	Permission   string   `json:"permission"`
	ContentTypes []string `json:"supported_content_types"`
}

func (s *PersonalContent) credential(ctx context.Context) (*types.TenantAPIKey, error) {
	scope, ok := types.TenantAPIKeyScopeFromContext(ctx)
	caller := types.CallerFromContext(ctx)
	p, human := ctx.Value(types.PrincipalContextKey).(types.Principal)
	if s == nil || s.DB == nil || s.Keys == nil || s.Members == nil || s.Employees == nil || !ok || scope.ScopeType != types.APIKeyScopeUserMCP || scope.FullAccess || !human || p.Type != types.PrincipalWebUser || p.ID == "" || caller.UserID != p.ID || !scope.HasCapability(types.APIKeyCapabilityAddContent) {
		return nil, access.ErrForbidden
	}
	key, err := s.Keys.GetUserMCPAPIKey(ctx, p.ID, scope.KeyID)
	if err != nil {
		return nil, err
	}
	if key == nil || !key.IsUserMCP() || key.FullAccess || key.OwnerUserID == nil || *key.OwnerUserID != p.ID || key.RevokedAt != nil || key.ExpiresAt != nil && !key.ExpiresAt.After(time.Now()) || !(types.TenantAPIKeyScope{Capabilities: key.Capabilities}).HasCapability(types.APIKeyCapabilityAddContent) {
		return nil, access.ErrForbidden
	}
	eligible, err := s.Employees.IsEnterpriseEmployee(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	if !eligible {
		return nil, access.ErrForbidden
	}
	return key, nil
}

func (s *PersonalContent) authorize(ctx context.Context, id string) (context.Context, *types.KnowledgeBase, error) {
	key, err := s.credential(ctx)
	if err != nil {
		return ctx, nil, err
	}
	// KB repository hooks enforce the persisted private-owner marker.
	kb, err := s.KBs.GetKnowledgeBaseByID(ctx, id)
	if err != nil || kb == nil {
		return ctx, nil, access.ErrForbidden
	}
	tenant, err := s.Tenants.GetTenantByID(ctx, kb.TenantID)
	if err != nil || tenant == nil || tenant.Status != "active" {
		return ctx, nil, access.ErrForbidden
	}
	members, err := s.Members.ListByUser(ctx, *key.OwnerUserID)
	if err != nil {
		return ctx, nil, err
	}
	base := &access.LiveDocumentBaseVerifier{Members: s.Members, Keys: s.Keys}
	for _, member := range members {
		if member == nil || member.UserID != *key.OwnerUserID || member.Status != types.TenantMemberStatusActive || member.DeletedAt.Valid || !member.Role.IsValid() {
			continue
		}
		// This existing helper resolves dynamic_all to current membership, or the
		// exact persisted scope for fixed Keys. No public reader is involved.
		fresh, e := base.PrivateWorkspaceContext(ctx, member.TenantID)
		if e != nil {
			continue
		}
		scope, _ := types.TenantAPIKeyScopeFromContext(fresh)
		scope.Capabilities = key.Capabilities
		fresh = types.WithTenantAPIKeyScope(fresh, scope)
		if !scope.AllowsKnowledgeBase(kb.ID) {
			continue
		}
		if access.CheckKBContentCreate(fresh, kb, s.Shares) != nil {
			continue
		}
		grant, e := access.ResolveKB(fresh, access.KBRequest{Caller: types.CallerFromContext(fresh)}, kb, types.OrgRoleEditor, s.Shares, nil)
		if e != nil {
			continue
		}
		fresh = context.WithValue(grant.Context(fresh), types.TenantInfoContextKey, tenant)
		if access.RequireKBContentCreate(fresh, kb) != nil {
			continue
		}
		return fresh, kb, nil
	}
	return ctx, nil, access.ErrForbidden
}

func (s *PersonalContent) ListWritable(ctx context.Context) ([]WritableKnowledgeBase, error) {
	if _, err := s.credential(ctx); err != nil {
		return nil, err
	}
	// Metadata candidates are independent of publication/search eligibility.
	var ids []string
	if err := s.DB.WithContext(ctx).Table("knowledge_bases").Where("deleted_at IS NULL AND is_temporary=false AND type=?", types.KnowledgeBaseTypeDocument).Order("id").Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	rows := []WritableKnowledgeBase{}
	for _, id := range ids {
		scoped, kb, err := s.authorize(ctx, id)
		if err != nil {
			continue
		}
		tenant, _ := types.TenantInfoFromContext(scoped)
		rows = append(rows, WritableKnowledgeBase{ID: kb.ID, Name: kb.Name, Space: tenant.Name, Permission: "editor", ContentTypes: []string{"text/markdown", "text/plain"}})
	}
	return rows, nil
}

type PersonalContentRequest struct {
	KBID           string `json:"knowledge_base_id"`
	Title          string `json:"title"`
	Content        string `json:"content"`
	Publish        bool   `json:"publish"`
	IdempotencyKey string `json:"-"`
}

func (s *PersonalContent) Add(ctx context.Context, req PersonalContentRequest) (created *types.Knowledge, err error) {
	caller := types.CallerFromContext(ctx)
	scope, _ := types.TenantAPIKeyScopeFromContext(ctx)
	// Audit uses only identifiers and a bounded outcome. No title/body/secret.
	defer func() {
		if s == nil || s.Audit == nil {
			return
		}
		outcome := types.AuditOutcomeAccepted
		doc := ""
		tenant := caller.TenantID
		if err != nil {
			outcome = types.AuditOutcomeDenied
		}
		if created != nil {
			doc = created.ID
			tenant = created.TenantID
			if created.ParseStatus == "failed" {
				outcome = types.AuditOutcomeFailed
			}
		}
		details, _ := json.Marshal(map[string]any{"key_id": scope.KeyID})
		_ = s.Audit.Log(context.WithoutCancel(ctx), &types.AuditLog{TenantID: tenant, ActorUserID: caller.UserID, Action: "mcp.content_added", ScopeType: "knowledge_base", ScopeID: req.KBID, TargetType: "knowledge", TargetID: doc, Outcome: outcome, Details: types.JSON(details)})
	}()
	if len(req.IdempotencyKey) < 8 || len(req.IdempotencyKey) > 128 || strings.TrimSpace(req.Title) == "" || len(req.Title) > 512 || strings.TrimSpace(req.Content) == "" || len(req.Content) > 512<<10 {
		return nil, errors.New("invalid title, content or idempotency_key (8..128 characters)")
	}
	// Embedded file references can cause parser fetches or claim existing stored
	// resources. Text admission does not establish authorization to those files.
	lower := strings.ToLower(req.Content)
	if strings.Contains(lower, "![") || strings.Contains(lower, "<img") || strings.Contains(lower, "<iframe") || strings.Contains(lower, "file://") || strings.Contains(lower, "resource://") || strings.Contains(lower, "/files/") {
		return nil, errors.New("embedded resources require the controlled upload workflow")
	}
	scoped, kb, err := s.authorize(ctx, req.KBID)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(req)
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("knowhub:personal-content:%d:%s", scope.KeyID, req.IdempotencyKey))).String()
	existing := func() (*types.Knowledge, error) {
		// A receipt is not a document read: never load title/body, sources or
		// files, and never require a pending document to pass publication ACLs.
		var receipt struct {
			ID                                                      string
			TenantID                                                uint64
			KnowledgeBaseID, ParseStatus, ErrorMessage, RequestHash string
		}
		fingerprint := "metadata->>'creation_request_hash'"
		if s.DB.Dialector.Name() == "sqlite" {
			fingerprint = "json_extract(metadata, '$.creation_request_hash')"
		}
		e := s.DB.WithContext(scoped).Table("knowledges").Select("id, tenant_id, knowledge_base_id, parse_status, error_message, "+fingerprint+" AS request_hash").Where("id=? AND deleted_at IS NULL", id).Take(&receipt).Error
		if e != nil {
			return nil, e
		}
		if receipt.RequestHash != hash || receipt.KnowledgeBaseID != kb.ID || receipt.TenantID != kb.TenantID {
			return nil, ErrPersonalContentConflict
		}
		return &types.Knowledge{ID: receipt.ID, TenantID: receipt.TenantID, KnowledgeBaseID: receipt.KnowledgeBaseID, ParseStatus: receipt.ParseStatus, ErrorMessage: receipt.ErrorMessage}, nil
	}
	if row, e := existing(); row != nil || errors.Is(e, ErrPersonalContentConflict) {
		return row, e
	}
	status := types.ManualKnowledgeStatusDraft
	if req.Publish {
		status = types.ManualKnowledgeStatusPublish
	}
	created, err = s.Knowledge.CreateKnowledgeFromManual(scoped, kb.ID, &types.ManualKnowledgePayload{Title: req.Title, Content: req.Content, Status: status, Channel: types.ChannelAPI, CreationID: id, CreationRequestHash: hash}, types.ChannelAPI)
	if err != nil {
		// A primary-key race, timeout or post-insert error must not make another
		// document. Only the INSERT winner enqueues parsing in the existing service.
		if row, e := existing(); row != nil || errors.Is(e, ErrPersonalContentConflict) {
			return row, e
		}
	}
	return created, err
}
