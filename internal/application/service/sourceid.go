package service

import (
	"context"
	"errors"
	"time"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/google/uuid"
)

type oidcContextKey string

const oidcNonceKey oidcContextKey = "oidc_nonce"
const oidcVerifierKey oidcContextKey = "oidc_verifier"
const sourceIDSessionKey oidcContextKey = "sourceid_session"
const sourceIDPreviousKey oidcContextKey = "sourceid_previous_refresh"
const sourceIDPreparedKey oidcContextKey = "sourceid_prepared"

func (s *userService) sourceIDStore() (interfaces.SourceIDRepository, error) {
	r, ok := s.userRepo.(interfaces.SourceIDRepository)
	if !ok {
		return nil, errors.New("sourceid repository unavailable")
	}
	return r, nil
}

// PrepareOIDCCallback consumes durable state before code exchange. The verifier
// is never embedded in the authorization URL or the signed (readable) state.
func (s *userService) PrepareOIDCCallback(ctx context.Context, state string) (context.Context, error) {
	p, err := secutils.VerifyOIDCState(state)
	if err != nil {
		return ctx, err
	}
	ctx = context.WithValue(ctx, oidcNonceKey, p.Nonce)
	if s.config == nil || s.config.OIDCAuth == nil || !s.config.OIDCAuth.SourceIDEnable {
		return ctx, nil
	}
	r, err := s.sourceIDStore()
	if err != nil {
		return ctx, err
	}
	transaction, err := r.ConsumeOIDCTransaction(ctx, apprepo.SourceIDHash(state))
	if err != nil {
		return ctx, err
	}
	if transaction.Nonce != p.Nonce {
		return ctx, errors.New("oidc transaction mismatch")
	}
	ctx, err = prepareSourceIDReauthentication(ctx, s.config.OIDCAuth, transaction.Result)
	if err != nil {
		return ctx, err
	}
	ctx = context.WithValue(ctx, sourceIDPreparedKey, true)
	return context.WithValue(ctx, oidcVerifierKey, transaction.Verifier), nil
}

// Only explicit, independently confirmed claim paths are accepted. No employee
// attribute is used as a substitute for an external session identifier.
func sourceIDClaim(claims map[string]interface{}, path string) string {
	return strictSourceIDClaim(claims, path)
}

func (s *userService) loginSourceID(ctx context.Context, cfg *config.OIDCAuthConfig, info *types.OIDCUserInfo) (*types.User, context.Context, error) {
	if cfg.SourceIDProtocolOnly || !cfg.SourceIDContractConfirmed {
		return nil, ctx, errors.New("sourceid identity contract pending")
	}
	mode := cfg.EffectiveSourceIDSessionMode()
	if cfg.EffectiveSourceIDAccountScope() == "enterprise_all" && (mode != "local_approved" || cfg.SourceIDAdmissionMode != "oidc_sso_approved") {
		return nil, ctx, errors.New("sourceid enterprise local policy required")
	}
	if cfg.SourceIDAdmissionMode == "oidc_sso_approved" && mode != "local_approved" {
		return nil, ctx, errors.New("sourceid sso local session required")
	}
	if mode != "external" && mode != "local_approved" {
		return nil, ctx, errors.New("sourceid session mode invalid")
	}
	if cfg.SourceIDEmployeeClaim == "" || (mode == "external" && (cfg.SourceIDSessionClaim == "" || cfg.SourceIDStatusClaim == "" || cfg.SourceIDAllowedStatus == "")) {
		return nil, ctx, errors.New("sourceid claim contract unconfirmed")
	}
	if cfg.SourceIDEmailBindingEnable && (!cfg.SourceIDEmailContractConfirmed || cfg.SourceIDEmployeeClaim != "attributes.RJGH" || cfg.SourceIDClaimsSource != "userinfo") {
		return nil, ctx, errors.New("sourceid email identity contract pending")
	}
	if cfg.SourceIDAutoBindingEnable && (!cfg.SourceIDEmailBindingEnable || cfg.SourceIDAutoBindingPolicyRef == "") {
		return nil, ctx, errors.New("sourceid automatic binding policy pending")
	}
	employee := sourceIDStringClaim(info.Claims, cfg.SourceIDEmployeeClaim)
	index := sourceIDStringClaim(info.Claims, cfg.SourceIDSessionClaim)
	status := sourceIDClaim(info.Claims, cfg.SourceIDStatusClaim)
	if employee == "" || info.Subject == "" || (mode == "external" && index == "") {
		return nil, ctx, errors.New("sourceid employee or session missing")
	}
	if mode == "external" && status != cfg.SourceIDAllowedStatus {
		return nil, ctx, errors.New("sourceid employee disabled")
	}
	now := time.Now()
	var admission types.SourceIDAdmissionDecision
	if mode == "local_approved" {
		var err error
		proof, _ := ctx.Value(sourceIDAuthorizationProofKey{}).(sourceIDAuthorizationProof)
		admission, err = s.resolveSourceIDAdmission(ctx, cfg, types.SourceIDAdmissionRequest{
			Issuer: cfg.IssuerURL, Subject: info.Subject, EmployeeNo: employee, Stage: "login", VerifiedClaims: info.Claims,
			AuthorizationVerified: proof.Issuer == cfg.IssuerURL && proof.Subject == info.Subject, AuthorizationVerifiedAt: proof.At,
			PromptLoginRequested: proof.PromptLoginRequested,
		})
		if err != nil {
			return nil, ctx, err
		}
	}
	r, err := s.sourceIDStore()
	if err != nil {
		return nil, ctx, err
	}
	var user *types.User
	if mode == "local_approved" && cfg.SourceIDAdmissionMode == "oidc_sso_approved" {
		if cfg.EffectiveSourceIDAccountScope() == "enterprise_all" {
			binding, ok := r.(sourceIDEnterpriseStore)
			if !ok {
				return nil, ctx, errors.New("sourceid enterprise repository unavailable")
			}
			maxAge, policyErr := cfg.SourceIDEnterpriseFreshness()
			if policyErr != nil {
				return nil, ctx, policyErr
			}
			user, err = binding.ResolveSourceIDEnterpriseEmailIdentity(ctx, cfg.IssuerURL, info.Subject, employee, sourceIDStringClaim(info.Claims, "attributes.RJEMAIL"), cfg.SourceIDAutoBindingPolicyRef, cfg.SourceIDEnterprisePolicyRef, maxAge)
		} else {
			accounts, accountErr := cfg.SourceIDPilotAccounts()
			if accountErr != nil {
				return nil, ctx, accountErr
			}
			binding, ok := r.(interface {
				ResolveSourceIDPilotEmailIdentity(context.Context, string, string, string, string, string, map[string]types.SourceIDPilotAccount) (*types.User, error)
			})
			if !ok {
				return nil, ctx, errors.New("sourceid pilot repository unavailable")
			}
			user, err = binding.ResolveSourceIDPilotEmailIdentity(ctx, cfg.IssuerURL, info.Subject, employee, sourceIDStringClaim(info.Claims, "attributes.RJEMAIL"), cfg.SourceIDAutoBindingPolicyRef, accounts)
		}
	} else if cfg.SourceIDEmailBindingEnable {
		if cfg.SourceIDAutoBindingEnable {
			binding, ok := r.(interface {
				ResolveSourceIDAutoEmailIdentity(context.Context, string, string, string, string, string) (*types.User, error)
			})
			if !ok {
				return nil, ctx, errors.New("sourceid binding repository unavailable")
			}
			user, err = binding.ResolveSourceIDAutoEmailIdentity(ctx, cfg.IssuerURL, info.Subject, employee, sourceIDStringClaim(info.Claims, "attributes.RJEMAIL"), cfg.SourceIDAutoBindingPolicyRef)
		} else {
			binding, ok := r.(interface {
				ResolveSourceIDEmailIdentity(context.Context, string, string, string, string) (*types.User, error)
			})
			if !ok {
				return nil, ctx, errors.New("sourceid binding repository unavailable")
			}
			user, err = binding.ResolveSourceIDEmailIdentity(ctx, cfg.IssuerURL, info.Subject, employee, sourceIDStringClaim(info.Claims, "attributes.RJEMAIL"))
		}
	} else {
		user, err = r.ResolveSourceIDIdentity(ctx, cfg.IssuerURL, info.Subject, employee)
	}
	if err != nil {
		return nil, ctx, err
	}
	session := &types.SourceIDSession{ID: uuid.NewString(), Issuer: cfg.IssuerURL, UserID: user.ID, SessionIndexHash: apprepo.SourceIDHash(cfg.IssuerURL + "\x00" + index), ExpiresAt: time.Now().Add(7 * 24 * time.Hour), CreatedAt: time.Now()}
	if mode == "local_approved" {
		policy, err := cfg.SourceIDLocalPolicy()
		if err != nil {
			return nil, ctx, err
		}
		store, ok := r.(sourceIDLocalStore)
		if !ok {
			return nil, ctx, errors.New("sourceid local repository unavailable")
		}
		session.SessionIndexHash = ""
		session.ExpiresAt = now.Add(policy.AbsoluteTTL)
		if !admission.RecheckSupported && admission.ValidUntil.Before(session.ExpiresAt) {
			session.ExpiresAt = admission.ValidUntil
		}
		local := &types.SourceIDLocalSession{ID: session.ID, Subject: info.Subject, PolicyRef: cfg.SourceIDLocalPolicyRef,
			AdmissionPolicyRef: cfg.SourceIDAdmissionPolicyRef, AdmissionValidUntil: admission.ValidUntil, LastSeenAt: now,
			AdmissionSource: admission.Source, AdmissionCheckedAt: admission.CheckedAt, RecheckSupported: admission.RecheckSupported,
			IdleSeconds: int64(policy.IdleTTL / time.Second), AccessSeconds: int64(policy.AccessTTL / time.Second), RefreshEnabled: cfg.SourceIDLocalRefreshEnable}
		if cfg.SourceIDAdmissionMode == "oidc_sso_approved" && cfg.EffectiveSourceIDAccountScope() == "enterprise_all" {
			enterprise, ok := r.(sourceIDEnterpriseStore)
			if !ok {
				return nil, ctx, errors.New("sourceid enterprise repository unavailable")
			}
			maxAge, policyErr := cfg.SourceIDEnterpriseFreshness()
			if policyErr != nil {
				return nil, ctx, policyErr
			}
			err = enterprise.CreateSourceIDEnterpriseLocalSession(ctx, session, local, employee, cfg.SourceIDEnterprisePolicyRef, maxAge)
		} else {
			err = store.CreateSourceIDLocalSession(ctx, session, local)
		}
		if err != nil {
			return nil, ctx, err
		}
	} else if cfg.SourceIDEmailBindingEnable {
		binding, ok := r.(interface {
			CreateSourceIDReviewedSession(context.Context, *types.SourceIDSession) error
		})
		if !ok {
			return nil, ctx, errors.New("sourceid session repository unavailable")
		}
		err = binding.CreateSourceIDReviewedSession(ctx, session)
	} else {
		err = r.CreateSourceIDSession(ctx, session)
	}
	if err != nil {
		return nil, ctx, err
	}
	if err := r.CheckSourceIDSession(ctx, session.ID, user.ID); err != nil {
		return nil, ctx, err
	}
	return user, context.WithValue(ctx, sourceIDSessionKey, session.ID), nil
}

func (s *userService) CheckSourceIDTokenSession(ctx context.Context, token *types.AuthToken) error {
	if token == nil {
		return errors.New("sourceid token missing")
	}
	if token.SessionID == "" {
		return nil
	}
	if _, err := s.sourceIDLocalLimits(ctx, token.SessionID, token.UserID); err != nil {
		return err
	}
	r, err := s.sourceIDStore()
	if err != nil {
		return err
	}
	return r.CheckSourceIDSession(ctx, token.SessionID, token.UserID)
}

// Switches inherit the authenticated access token's session, never a caller's
// unrelated refresh token. This works even when no refresh token is submitted.
func (s *userService) SourceIDSwitchContext(ctx context.Context, access string) (context.Context, error) {
	if _, _, err := s.ValidateToken(ctx, access); err != nil {
		return ctx, err
	}
	token, err := s.tokenRepo.GetTokenByValue(ctx, access)
	if err != nil {
		return ctx, err
	}
	if token == nil || token.IsRevoked || token.TokenType != "access_token" || !token.ExpiresAt.After(time.Now()) {
		return ctx, errors.New("invalid access token")
	}
	if err := s.CheckSourceIDTokenSession(ctx, token); err != nil {
		return ctx, err
	}
	if token.SessionID != "" {
		ctx = context.WithValue(ctx, sourceIDSessionKey, token.SessionID)
		ctx = context.WithValue(ctx, types.SourceIDTokenOperationKey{}, "switch")
	}
	return ctx, nil
}

func (s *userService) SourceIDBackchannelLogout(ctx context.Context, issuer, index string) error {
	if s.config == nil || s.config.OIDCAuth == nil || !s.config.OIDCAuth.Enable || !s.config.OIDCAuth.SourceIDEnable || !s.config.OIDCAuth.SourceIDLogoutEnable {
		return errors.New("sourceid logout disabled")
	}
	if issuer == "" || issuer != s.config.OIDCAuth.IssuerURL {
		return errors.New("sourceid issuer mismatch")
	}
	r, err := s.sourceIDStore()
	if err != nil {
		return err
	}
	return r.RevokeSourceIDIndex(ctx, issuer, index)
}

func (s *userService) SaveOIDCLoginResult(ctx context.Context, result string) (string, error) {
	r, err := s.sourceIDStore()
	if err != nil {
		return "", err
	}
	key, err := generateRandomString(32)
	if err != nil {
		return "", err
	}
	err = r.SaveOIDCTransaction(ctx, &types.OIDCTransaction{Key: apprepo.SourceIDHash("result:" + key), Result: result, ExpiresAt: time.Now().Add(time.Minute)})
	return key, err
}

func (s *userService) ConsumeOIDCLoginResult(ctx context.Context, key string) (string, error) {
	r, err := s.sourceIDStore()
	if err != nil {
		return "", err
	}
	t, err := r.ConsumeOIDCTransaction(ctx, apprepo.SourceIDHash("result:"+key))
	if err != nil {
		return "", err
	}
	return t.Result, nil
}
