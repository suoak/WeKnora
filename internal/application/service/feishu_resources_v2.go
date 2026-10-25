package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
)

func connectionID(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ResolveCredentialReference loads only the current user's grant. No request
// accepts owner/open_id/epoch fields, and user failures never select app tokens.
func (s *FeishuPersonalService) ResolveCredentialReference(
	ctx context.Context,
	mode, id string,
) (context.Context, *core.Client, types.FeishuCredentialRef, error) {
	var ref types.FeishuCredentialRef
	user, ok := types.UserIDFromContext(ctx)
	if !ok {
		return ctx, nil, ref, access.ErrUnauthorized
	}
	cfg, err := s.app(ctx)
	if err != nil {
		return ctx, nil, ref, err
	}
	if cfg.Capabilities == nil || !cfg.modeEnabled(mode) {
		return ctx, nil, ref, types.ErrFeishuIdentityDisabled
	}
	ref = types.FeishuCredentialRef{
		Version:       1,
		Kind:          mode,
		AppID:         cfg.AppID,
		ConfigVersion: cfg.identityVersion(mode),
	}
	client := s.client(&core.Config{AppID: cfg.AppID, AppSecret: cfg.Secret})
	if mode == types.FeishuUserOAuth {
		var c types.FeishuConnection
		var epoch types.FeishuPersonalGrantEpoch
		if s.db.WithContext(ctx).
			Where("user_id = ? AND app_id = ?", user, cfg.AppID).
			First(&c).
			Error != nil ||
			s.db.WithContext(ctx).Where("user_id = ?", user).First(&epoch).Error != nil {
			return ctx, nil, ref, ErrFeishuReconnect
		}
		ref.OwnerID, ref.OpenID, ref.GrantEpoch = user, c.OpenID, epoch.Epoch
		ref.ConnectionID = connectionID(mode, user, cfg.AppID, c.OpenID, epoch.Epoch)
	} else {
		if !applicationGrantEnabled() || !applicationUserAllowed(types.IsSystemAdminFromContext(ctx)) {
			return ctx, nil, ref, ErrFeishuApplicationRestricted
		}
		ref.ConnectionID = connectionID(mode, cfg.AppID, cfg.identityVersion(mode))
	}
	if id != "" && id != ref.ConnectionID {
		return ctx, nil, ref, access.ErrForbidden
	}
	if err := ref.Validate(user); err != nil {
		return ctx, nil, ref, err
	}
	validate := func(call context.Context) error {
		current, e := s.app(call)
		if e != nil || current.identityVersion(mode) != ref.ConfigVersion || current.AppID != ref.AppID ||
			!current.modeEnabled(mode) {
			return ErrFeishuReconnect
		}
		if mode == types.FeishuUserOAuth {
			var n int64
			if e = s.db.
				WithContext(call).
				Model(&types.FeishuPersonalGrantEpoch{}).
				Where("user_id = ? AND epoch = ?",
					user,
					ref.GrantEpoch).
				Count(&n).Error; e != nil {
				return e
			}
			if n != 1 {
				return ErrFeishuReconnect
			}
		}
		return nil
	}
	provider := func(call context.Context) (string, error) {
		if e := validate(call); e != nil {
			return "", e
		}
		if mode == types.FeishuUserOAuth {
			return s.UserToken(call, user, ref.AppID, ref.OpenID)
		}
		return client.GetTenantAccessToken(call)
	}
	if mode == types.FeishuUserOAuth {
		ctx = core.WithUserIdentity(ctx, provider)
	} else {
		ctx = core.WithApplicationIdentityValidation(ctx, provider, validate)
	}
	return ctx, client, ref, nil
}

// FeishuResourceQuery selects one identity-bound resource page.
type FeishuResourceQuery struct {
	AuthMode     string
	ConnectionID string
	Kind         string
	Parent       string
	Space        string
	Cursor       string
	Size         int
}
type feishuResourceCursor struct {
	User           string
	Connection     string
	Kind           string
	Parent         string
	Space          string
	Page           string
	Expires        time.Time
	ResolvedParent string
	ResolvedSpace  string
}

// FeishuResourcePage returns a bounded page and an encrypted continuation cursor.
type FeishuResourcePage struct {
	Items    []core.ResourceEntry `json:"items"`
	HasMore  bool                 `json:"has_more"`
	Cursor   string               `json:"cursor,omitempty"`
	Coverage string               `json:"coverage"`
}

// Resources browses only the selected identity's accessible Drive or Wiki page.
func (s *FeishuPersonalService) Resources(
	ctx context.Context,
	q FeishuResourceQuery,
) (FeishuResourcePage, error) {
	out := FeishuResourcePage{
		Items:    []core.ResourceEntry{},
		Coverage: "current_identity_one_level; shared_with_me_is_not_exhaustive",
	}
	remote, client, ref, err := s.ResolveCredentialReference(ctx, q.AuthMode, q.ConnectionID)
	if err != nil {
		return out, err
	}
	user, _ := types.UserIDFromContext(ctx)
	page := ""
	var previous feishuResourceCursor
	if len(q.Cursor) > 8192 {
		return out, errors.New("invalid_resource_cursor")
	}
	if q.Cursor != "" {
		var c feishuResourceCursor
		if openFeishu(q.Cursor, &c) != nil || c.User != user || c.Connection != ref.ConnectionID ||
			c.Kind != q.Kind ||
			c.Parent != q.Parent ||
			c.Space != q.Space ||
			!c.Expires.After(time.Now()) {
			return out, errors.New("invalid_resource_cursor")
		}
		page = c.Page
		previous = c
	}
	if q.Size == 0 {
		q.Size = 50
	}
	remote, cancel := context.WithTimeout(remote, 30*time.Second)
	defer cancel()
	result, err := client.ReadResourcePage(remote, q.Kind, q.Parent, q.Space, page, q.Size)
	if err != nil {
		return out, err
	}
	if page != "" && (previous.ResolvedParent != result.ResolvedParent ||
		previous.ResolvedSpace != result.ResolvedSpace) {
		return out, errors.New("resource_parent_changed_restart_browse")
	}
	out.Items, out.HasMore = result.Items, result.HasMore
	if result.HasMore {
		out.Cursor, err = sealFeishu(
			feishuResourceCursor{
				User:           user,
				Connection:     ref.ConnectionID,
				Kind:           q.Kind,
				Parent:         q.Parent,
				Space:          q.Space,
				Page:           result.NextPage,
				Expires:        time.Now().Add(10 * time.Minute),
				ResolvedParent: result.ResolvedParent,
				ResolvedSpace:  result.ResolvedSpace,
			},
		)
	}
	return out, err
}

// Connections returns the caller's available identities without bearer tokens.
func (s *FeishuPersonalService) Connections(ctx context.Context) ([]map[string]any, error) {
	user, ok := types.UserIDFromContext(ctx)
	if !ok {
		return nil, access.ErrUnauthorized
	}
	cfg, err := s.app(ctx)
	if err != nil {
		return nil, err
	}
	rows := []map[string]any{}
	if cfg.Capabilities == nil {
		return rows, nil
	}
	if cfg.modeEnabled(types.FeishuUserOAuth) {
		row := map[string]any{"kind": types.FeishuUserOAuth, "connected": false}
		var c types.FeishuConnection
		var epoch types.FeishuPersonalGrantEpoch
		if s.db.WithContext(ctx).
			Where("user_id = ? AND app_id = ?", user, cfg.AppID).
			First(&c).
			Error == nil &&
			s.db.WithContext(ctx).Where("user_id = ?", user).First(&epoch).Error == nil {
			row["id"] = connectionID(types.FeishuUserOAuth, user, cfg.AppID, c.OpenID, epoch.Epoch)
			row["connected"] = c.RefreshExpiresAt.After(time.Now())
		}
		rows = append(rows, row)
	}
	if cfg.modeEnabled(types.FeishuApplicationGrant) && applicationGrantEnabled() &&
		applicationUserAllowed(types.IsSystemAdminFromContext(ctx)) {
		rows = append(
			rows,
			map[string]any{
				"kind": types.FeishuApplicationGrant,
				"id": connectionID(
					types.FeishuApplicationGrant,
					cfg.AppID,
					cfg.identityVersion(types.FeishuApplicationGrant),
				),
				"connected": true,
			},
		)
	}
	return rows, nil
}
