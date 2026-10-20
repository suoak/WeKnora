package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
)

type sourceIDVerifiedEmailStore interface {
	ResolveSourceIDVerifiedEmailSession(
		context.Context,
		types.SourceIDVerifiedEmailLogin,
		*types.SourceIDSession,
		*types.SourceIDLocalSession,
		func(context.Context, interfaces.UserRepository) (*types.User, error),
	) (*types.User, bool, error)
	SourceIDSessionLinkPolicy(context.Context, string) (*types.SourceIDLocalLinkPolicy, error)
	CheckSourceIDVerifiedEmailSession(
		context.Context,
		string,
		string,
		string,
		string,
		string,
		string,
		string,
	) error
}

// Called only after durable state/code, ID Token and UserInfo validation and
// ordinary SSO admission. Binding + policy audit + Session commit atomically.
func (s *userService) loginSourceIDVerifiedEmail(
	ctx context.Context,
	cfg *config.OIDCAuthConfig,
	info *types.OIDCUserInfo,
	employee string,
	admission types.SourceIDAdmissionDecision,
	now time.Time,
) (*types.User, context.Context, error) {
	p, err := cfg.SourceIDLocalPolicy()
	if err != nil {
		return nil, ctx, err
	}
	r, ok := s.userRepo.(sourceIDVerifiedEmailStore)
	if !ok {
		return nil, ctx, errors.New("sourceid verified email repository unavailable")
	}
	email := strings.ToLower(strings.TrimSpace(sourceIDStringClaim(info.Claims, "attributes.RJEMAIL")))
	session := &types.SourceIDSession{
		ID:        uuid.NewString(),
		Issuer:    cfg.IssuerURL,
		ExpiresAt: now.Add(p.AbsoluteTTL),
		CreatedAt: now,
	}
	if admission.ValidUntil.Before(session.ExpiresAt) {
		session.ExpiresAt = admission.ValidUntil
	}
	local := &types.SourceIDLocalSession{
		ID:                  session.ID,
		Subject:             info.Subject,
		PolicyRef:           cfg.SourceIDLocalPolicyRef,
		AdmissionPolicyRef:  cfg.SourceIDAdmissionPolicyRef,
		AdmissionValidUntil: admission.ValidUntil,
		AdmissionSource:     admission.Source,
		AdmissionCheckedAt:  admission.CheckedAt,
		LastSeenAt:          now,
		AccessSeconds:       int64(p.AccessTTL / time.Second),
		IdleSeconds:         int64(p.IdleTTL / time.Second),
	}
	q := types.SourceIDVerifiedEmailLogin{
		Issuer:           cfg.IssuerURL,
		Subject:          info.Subject,
		EmployeeNo:       employee,
		Email:            email,
		PolicyRef:        cfg.SourceIDVerifiedEmailPolicyRef,
		BindingPolicyRef: cfg.SourceIDAutoBindingPolicyRef,
		JITEnabled:       cfg.SourceIDJITEnable,
		JITPolicyRef:     cfg.SourceIDJITPolicyRef,
		JITDomains:       cfg.SourceIDJITDomains(),
	}
	user, created, err := r.ResolveSourceIDVerifiedEmailSession(
		ctx,
		q,
		session,
		local,
		s.sourceIDJITProvisioner(email),
	)
	if err != nil {
		return nil, ctx, err
	}
	store, err := s.sourceIDStore()
	if err != nil {
		return nil, ctx, err
	}
	if err = store.CheckSourceIDSession(ctx, session.ID, user.ID); err != nil {
		_ = store.RevokeSourceIDSession(ctx, session.ID, user.ID)
		return nil, ctx, err
	}
	ctx = context.WithValue(ctx, sourceIDJITCreatedKey{}, created)
	return user, context.WithValue(ctx, sourceIDSessionKey, session.ID), nil
}
