package service

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
)

type sourceIDLocalStore interface {
	GetSourceIDLocalSession(context.Context, string, string) (*types.SourceIDLocalSession, error)
	CreateSourceIDLocalSession(context.Context, *types.SourceIDSession, *types.SourceIDLocalSession) error
	UpdateSourceIDLocalAdmission(context.Context, string, string, types.SourceIDAdmissionDecision) error
}

type sourceIDEnterpriseStore interface {
	ResolveSourceIDEnterpriseEmailIdentity(context.Context, string, string, string, string, string, string, time.Duration) (*types.User, error)
	CreateSourceIDEnterpriseLocalSession(context.Context, *types.SourceIDSession, *types.SourceIDLocalSession, string, string, time.Duration) error
	CheckSourceIDEnterpriseOwnership(context.Context, string, string, string, string, time.Duration) error
}

type sourceIDAuthorizationProofKey struct{}
type sourceIDAuthorizationProof struct {
	Issuer, Subject      string
	At                   time.Time
	PromptLoginRequested bool
}

// First-release adapter: only consumes a fully verified, single-use OIDC
// transaction. It cannot infer eligibility from an arbitrary Token HTTP 200.
type sourceIDAuthorizationAdmissionResolver struct{ cfg *config.OIDCAuthConfig }

// Approved ordinary SSO accepts a verified OIDC transaction as identity proof.
// It makes no claim that an existing SID SSO session rechecked employment.
type sourceIDSSOAdmissionResolver struct{ cfg *config.OIDCAuthConfig }

func (r sourceIDSSOAdmissionResolver) Resolve(ctx context.Context, request types.SourceIDAdmissionRequest) (types.SourceIDAdmissionDecision, error) {
	unknown := types.SourceIDAdmissionDecision{Outcome: types.SourceIDAdmissionUnknown}
	if ctx.Err() != nil || !r.cfg.SourceIDSSOPolicyConfirmed || request.Stage != "login" || !request.AuthorizationVerified ||
		request.Issuer != r.cfg.IssuerURL || request.Subject == "" || request.AuthorizationVerifiedAt.IsZero() {
		return unknown, nil
	}
	p, err := r.cfg.SourceIDLocalPolicy()
	if err != nil {
		return unknown, err
	}
	return types.SourceIDAdmissionDecision{Outcome: types.SourceIDAdmissionAllow, Source: "oidc_sso_approved", ContractRef: r.cfg.SourceIDAdmissionPolicyRef,
		CheckedAt: request.AuthorizationVerifiedAt, ValidUntil: request.AuthorizationVerifiedAt.Add(p.AdmissionMaxAge)}, nil
}

func (r sourceIDAuthorizationAdmissionResolver) Resolve(ctx context.Context, request types.SourceIDAdmissionRequest) (types.SourceIDAdmissionDecision, error) {
	decision := types.SourceIDAdmissionDecision{Outcome: types.SourceIDAdmissionUnknown}
	if ctx.Err() != nil || !r.cfg.SourceIDAuthorizationRecheckConfirmed || request.Stage != "login" ||
		!r.cfg.SourceIDForceReauthPromptLogin || !request.PromptLoginRequested ||
		!request.AuthorizationVerified || request.Issuer != r.cfg.IssuerURL || request.Subject == "" || request.AuthorizationVerifiedAt.IsZero() {
		return decision, nil
	}
	policy, err := r.cfg.SourceIDLocalPolicy()
	if err != nil {
		return decision, err
	}
	return types.SourceIDAdmissionDecision{Outcome: types.SourceIDAdmissionAllow, Source: "authorization", ContractRef: r.cfg.SourceIDAdmissionPolicyRef,
		CheckedAt: request.AuthorizationVerifiedAt, ValidUntil: request.AuthorizationVerifiedAt.Add(policy.AdmissionMaxAge)}, nil
}

// Authorization admission is gated on a verified OIDC transaction and the
// separately confirmed SID SSO-recheck/freshness contract. Other strategies
// currently have synthetic adapters only.
func (s *userService) resolveSourceIDAdmission(ctx context.Context, cfg *config.OIDCAuthConfig, request types.SourceIDAdmissionRequest) (types.SourceIDAdmissionDecision, error) {
	var decision types.SourceIDAdmissionDecision
	p, err := cfg.SourceIDLocalPolicy()
	if err != nil {
		return decision, err
	}
	resolver := s.employeeAdmission
	if cfg.SourceIDAdmissionMode == "oidc_sso_approved" {
		resolver = sourceIDSSOAdmissionResolver{cfg}
	}
	if resolver == nil && cfg.SourceIDAdmissionMode == "authorization" && cfg.SourceIDAuthorizationRecheckConfirmed {
		resolver = sourceIDAuthorizationAdmissionResolver{cfg}
	}
	if resolver == nil {
		return decision, errors.New("sourceid admission contract pending")
	}
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	decision, err = resolver.Resolve(callCtx, request)
	if err != nil || callCtx.Err() != nil {
		return types.SourceIDAdmissionDecision{}, errors.New("sourceid admission unavailable")
	}
	if decision.Outcome == types.SourceIDAdmissionDeny {
		return decision, errors.New("sourceid admission denied")
	}
	now := time.Now()
	if decision.Outcome != types.SourceIDAdmissionAllow || decision.Source != cfg.SourceIDAdmissionMode ||
		decision.ContractRef != cfg.SourceIDAdmissionPolicyRef || decision.CheckedAt.IsZero() || decision.CheckedAt.After(now) ||
		!decision.ValidUntil.After(now) || decision.ValidUntil.Before(decision.CheckedAt) {
		return types.SourceIDAdmissionDecision{}, errors.New("sourceid admission unknown")
	}
	limit := decision.CheckedAt.Add(p.AdmissionMaxAge)
	if decision.ValidUntil.After(limit) {
		decision.ValidUntil = limit
	}
	if !decision.ValidUntil.After(now) {
		return types.SourceIDAdmissionDecision{}, errors.New("sourceid admission expired")
	}
	if decision.Source == "authorization" && decision.RecheckSupported {
		return types.SourceIDAdmissionDecision{}, errors.New("sourceid authorization admission cannot recheck")
	}
	if request.Stage != "login" && !decision.RecheckSupported {
		return types.SourceIDAdmissionDecision{}, errors.New("sourceid admission reauthentication required")
	}
	return decision, nil
}

func (s *userService) recheckSourceIDAdmission(ctx context.Context, sessionID, userID string) error {
	local, err := s.sourceIDLocalLimits(ctx, sessionID, userID)
	if err != nil || local == nil {
		return err
	}
	if !local.RecheckSupported {
		return nil
	} // Existing snapshot is strictly bounded; never extended.
	decision, err := s.resolveSourceIDAdmission(ctx, s.config.OIDCAuth, types.SourceIDAdmissionRequest{
		Issuer: local.Issuer, Subject: local.Subject, EmployeeNo: local.EmployeeNo, Stage: "refresh",
	})
	if err != nil {
		store, storeErr := s.sourceIDStore()
		if storeErr != nil {
			return storeErr
		}
		if revokeErr := store.RevokeSourceIDSession(ctx, sessionID, userID); revokeErr != nil {
			return revokeErr
		}
		return err
	}
	return s.userRepo.(sourceIDLocalStore).UpdateSourceIDLocalAdmission(ctx, sessionID, userID, decision)
}

func (s *userService) sourceIDLocalLimits(ctx context.Context, sessionID, userID string) (*types.SourceIDLocalSession, error) {
	store, ok := s.userRepo.(sourceIDLocalStore)
	if !ok {
		return nil, nil
	} // Existing external-only repository implementations.
	local, err := store.GetSourceIDLocalSession(ctx, sessionID, userID)
	if err != nil || local == nil {
		return local, err
	}
	if local.Revoked || !local.AbsoluteExpiresAt.After(time.Now()) || !local.LastSeenAt.Add(time.Duration(local.IdleSeconds)*time.Second).After(time.Now()) {
		return nil, errors.New("sourceid local session expired")
	}
	if s.config == nil || s.config.OIDCAuth == nil {
		return nil, errors.New("sourceid local policy unavailable")
	}
	cfg := s.config.OIDCAuth
	if _, err := cfg.SourceIDLocalPolicy(); err != nil {
		return nil, err
	}
	if cfg.IssuerURL != local.Issuer || cfg.SourceIDLocalPolicyRef != local.PolicyRef || cfg.SourceIDAdmissionPolicyRef != local.AdmissionPolicyRef || cfg.SourceIDAdmissionMode != local.AdmissionSource {
		return nil, errors.New("sourceid local policy changed; reauthentication required")
	}
	if cfg.SourceIDAdmissionMode == "oidc_sso_approved" {
		if cfg.EffectiveSourceIDAccountScope() == "enterprise_all" {
			enterprise, ok := store.(sourceIDEnterpriseStore)
			if !ok {
				return nil, errors.New("sourceid enterprise repository unavailable")
			}
			maxAge, policyErr := cfg.SourceIDEnterpriseFreshness()
			if policyErr != nil {
				return nil, policyErr
			}
			if err := enterprise.CheckSourceIDEnterpriseOwnership(ctx, local.Issuer, local.EmployeeNo, userID, cfg.SourceIDEnterprisePolicyRef, maxAge); err != nil {
				return nil, err
			}
		} else {
			accounts, err := cfg.SourceIDPilotAccounts()
			if err != nil || accounts[userID].Email == "" || accounts[userID].EmployeeNo != local.EmployeeNo {
				return nil, errors.New("sourceid pilot account unavailable")
			}
		}
	}
	return local, nil
}

func (s *userService) revokeSourceIDRefreshReplay(ctx context.Context, record *types.AuthToken) error {
	if record == nil || record.SessionID == "" || record.TokenType != "refresh_token" {
		return nil
	}
	local, err := s.sourceIDLocalLimits(ctx, record.SessionID, record.UserID)
	if err != nil {
		return err
	}
	if local == nil {
		return nil
	}
	store, err := s.sourceIDStore()
	if err != nil {
		return err
	}
	return store.RevokeSourceIDSession(ctx, record.SessionID, record.UserID)
}
