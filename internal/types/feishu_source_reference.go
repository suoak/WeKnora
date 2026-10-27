package types

import (
	"context"
	"encoding/json"
	"errors"

	"gorm.io/gorm"
)

// FeishuSourceReference persists an owned identity reference without tokens.
type FeishuSourceReference struct {
	Version         int    `json:"version"`
	OwnerUserID     string `json:"owner_user_id"`
	Kind            string `json:"kind"`
	ConnectionID    string `json:"connection_id"`
	AppID           string `json:"app_id"`
	IdentityVersion string `json:"identity_version"`
}

// ParseFeishuSourceReference validates an optional server-owned connector identity.
func ParseFeishuSourceReference(config *DataSourceConfig) (*FeishuSourceReference, error) {
	if config == nil || config.Settings == nil || config.Settings["credential_reference"] == nil {
		return nil, nil
	}
	b, err := json.Marshal(config.Settings["credential_reference"])
	if err != nil {
		return nil, err
	}
	var ref FeishuSourceReference
	if json.Unmarshal(b, &ref) != nil || ref.Version != 1 || ref.OwnerUserID == "" ||
		ref.ConnectionID == "" ||
		ref.AppID == "" ||
		ref.IdentityVersion == "" ||
		ref.Kind != FeishuUserOAuth && ref.Kind != FeishuApplicationGrant {
		return nil, errors.New("invalid_feishu_source_reference")
	}
	return &ref, nil
}

type (
	feishuSourceCommitGuardKey struct{}
	// FeishuSourceCommitGuard checks local identity and approval state under locks.
	FeishuSourceCommitGuard func(*gorm.DB) error
)

type (
	feishuSourceReadGuardKey struct{}
	// FeishuSourceReadGuard checks remote access and the expected document revision.
	FeishuSourceReadGuard func(context.Context) error
)

// WithFeishuSourceReadGuard attaches a remote check outside publication transactions. The commit guard
// separately fences local identity and sharing state under database locks.
func WithFeishuSourceReadGuard(ctx context.Context, guard FeishuSourceReadGuard) context.Context {
	return context.WithValue(ctx, feishuSourceReadGuardKey{}, guard)
}

// CheckFeishuSourceRead revalidates a bound source before model work or publication.
func CheckFeishuSourceRead(ctx context.Context) error {
	if guard, ok := ctx.Value(feishuSourceReadGuardKey{}).(FeishuSourceReadGuard); ok && guard != nil {
		return guard(ctx)
	}
	return nil
}

// WithFeishuSourceCommitGuard attaches the local publication fence.
func WithFeishuSourceCommitGuard(ctx context.Context, guard FeishuSourceCommitGuard) context.Context {
	return context.WithValue(ctx, feishuSourceCommitGuardKey{}, guard)
}

// FeishuSourceGuardFromContext returns the bound local publication fence.
func FeishuSourceGuardFromContext(ctx context.Context) (FeishuSourceCommitGuard, bool) {
	guard, ok := ctx.Value(feishuSourceCommitGuardKey{}).(FeishuSourceCommitGuard)
	return guard, ok && guard != nil
}
