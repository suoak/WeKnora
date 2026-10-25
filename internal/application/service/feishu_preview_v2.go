package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrFeishuSharingApproval = errors.New("feishu_sharing_approval_required")

type FeishuPreviewItem struct {
	URL           string `json:"url"`
	ResourceType  string `json:"resource_type,omitempty"`
	Revision      string `json:"revision,omitempty"`
	Status        string `json:"status"`
	ErrorCategory string `json:"error_category,omitempty"`
	Canonical     string `json:"-"`
	ResourceToken string `json:"-"`
}

func feishuV2Error(err error) string {
	var api *core.APIError
	if errors.As(err, &api) && api.Code == 99991672 {
		return "api_scope_missing"
	}
	switch {
	case errors.Is(err, ErrFeishuSharingApproval):
		return "sharing_approval_required"
	case errors.Is(err, ErrFeishuReconnect), errors.Is(err, core.ErrAuth):
		return "reconnect_required"
	case errors.Is(err, core.ErrPermission), errors.Is(err, core.ErrNotFound):
		return "source_permission_required"
	case errors.Is(err, core.ErrRateLimited), errors.Is(err, core.ErrTransient), errors.Is(err, context.DeadlineExceeded):
		return "temporary_failure"
	case errors.Is(err, access.ErrForbidden):
		return "kb_permission_required"
	case errors.Is(err, core.ErrUnsupportedPersonalResource):
		return "unsupported_resource"
	default:
		return "operation_failed"
	}
}

func (s *FeishuPersonalService) Preview(ctx context.Context, kbID, mode, id string, links []string) ([]FeishuPreviewItem, error) {
	if len(links) < 1 || len(links) > 100 {
		return nil, errors.New("feishu_batch_size_invalid")
	}
	kb, err := s.kbs.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return nil, err
	}
	if err = access.RequireKBWrite(ctx, kb); err != nil {
		return nil, err
	}
	remote, client, ref, err := s.ResolveCredentialReference(ctx, mode, id)
	if err != nil {
		return nil, err
	}
	remote, cancel := context.WithTimeout(remote, 30*time.Second)
	defer cancel()
	user, _ := types.UserIDFromContext(ctx)
	seen := map[string]bool{}
	out := make([]FeishuPreviewItem, 0, len(links))
	for _, link := range links {
		item := FeishuPreviewItem{URL: strings.TrimSpace(link), Status: "failed"}
		r, readErr := core.ParsePersonalResourceURL(link)
		if readErr == nil {
			if mode == types.FeishuUserOAuth {
				r, readErr = client.ResolvePersonalResource(remote, r)
			} else {
				r, readErr = client.ResolveApplicationResource(applicationTableContext(remote), r)
			}
		}
		if readErr != nil {
			item.ErrorCategory = feishuV2Error(readErr)
			out = append(out, item)
			continue
		}
		item.URL, item.ResourceType, item.Revision, item.ResourceToken = r.URL, r.Type, r.Modified, r.Token
		item.Canonical = connectionID(ref.AppID, r.Type, r.Token)
		if seen[item.Canonical] {
			item.Status = "duplicate"
			out = append(out, item)
			continue
		}
		seen[item.Canonical] = true
		item.Status = "metadata_readable_export_pending"
		if mode == types.FeishuUserOAuth {
			m := types.FeishuPersonalImport{UserID: user, KBID: kbID, AppID: ref.AppID, AuthMode: mode, ResourceType: r.Type, DocumentToken: r.Token}
			cfg, loadErr := s.app(ctx)
			if loadErr != nil {
				return nil, loadErr
			}
			if e := s.checkSharingApproval(s.db.WithContext(ctx), cfg, &m, r.Modified); e != nil {
				item.Status = "blocked"
				item.ErrorCategory = feishuV2Error(e)
			}
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *FeishuPersonalService) checkSharingApproval(tx *gorm.DB, cfg *FeishuAppConfig, m *types.FeishuPersonalImport, revision string) error {
	if cfg.Capabilities == nil || types.FeishuAuthMode(m.AuthMode) != types.FeishuUserOAuth || m.DocumentToken == "" {
		return nil
	}
	if types.PrivateKBGuardsEnabled(tx) {
		owner, err := types.PrivateKBOwner(tx, m.KBID)
		if err != nil {
			return err
		}
		if owner == m.UserID {
			return nil
		}
	}
	var approval types.FeishuSharingApproval
	err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("owner_id = ? AND kb_id = ? AND app_id = ? AND resource_type = ? AND resource_token = ? AND revision = ? AND config_version = ? AND state = ? AND expires_at > ?", m.UserID, m.KBID, m.AppID, m.ResourceType, m.DocumentToken, revision, cfg.Version, "approved", time.Now()).First(&approval).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrFeishuSharingApproval
		}
		return err
	}
	return nil
}

// ApproveSharing is an explicit, audited grant to the destination KB's current
// readers. It requires both the source user's identity and KB owner/admin
// authority; a reader can request collaboration but cannot grant for others.
func (s *FeishuPersonalService) ApproveSharing(ctx context.Context, kbID, id, link, reason string, confirmed bool) (*types.FeishuSharingApproval, error) {
	user, ok := types.UserIDFromContext(ctx)
	if !ok {
		return nil, access.ErrUnauthorized
	}
	if !confirmed || strings.TrimSpace(reason) == "" || len(reason) > 512 {
		return nil, errors.New("explicit_sharing_confirmation_required")
	}
	kb, err := s.kbs.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return nil, err
	}
	if err = access.RequireKBWrite(ctx, kb); err != nil {
		return nil, err
	}
	caller := types.CallerFromContext(ctx)
	if caller.TenantID != kb.TenantID || kb.CreatorID != user && !caller.Role.HasPermission(types.TenantRoleAdmin) {
		return nil, access.ErrForbidden
	}
	remote, client, ref, err := s.ResolveCredentialReference(ctx, types.FeishuUserOAuth, id)
	if err != nil {
		return nil, err
	}
	remote, cancel := context.WithTimeout(remote, 30*time.Second)
	defer cancel()
	r, err := core.ParsePersonalResourceURL(link)
	if err != nil {
		return nil, err
	}
	r, err = client.ResolvePersonalResource(remote, r)
	if err != nil {
		return nil, err
	}
	if r.Modified == "" {
		return nil, errors.New("source_revision_required")
	}
	approval := &types.FeishuSharingApproval{ID: uuid.NewString(), OwnerID: user, KBID: kbID, AppID: ref.AppID, ResourceType: r.Type, ResourceToken: r.Token, Revision: r.Modified, ConfigVersion: ref.ConfigVersion, State: "approved", ApprovedBy: user, Reason: reason, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(24 * time.Hour)}
	if err = s.db.WithContext(ctx).Create(approval).Error; err != nil {
		return nil, err
	}
	return approval, nil
}
