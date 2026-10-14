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
	if cfg.SourceIDEmployeeClaim == "" || cfg.SourceIDSessionClaim == "" || cfg.SourceIDStatusClaim == "" || cfg.SourceIDAllowedStatus == "" {
		return nil, ctx, errors.New("sourceid claim contract unconfirmed")
	}
	employee := sourceIDStringClaim(info.Claims, cfg.SourceIDEmployeeClaim)
	index := sourceIDStringClaim(info.Claims, cfg.SourceIDSessionClaim)
	status := sourceIDClaim(info.Claims, cfg.SourceIDStatusClaim)
	if employee == "" || index == "" {
		return nil, ctx, errors.New("sourceid employee or session missing")
	}
	if status != cfg.SourceIDAllowedStatus {
		return nil, ctx, errors.New("sourceid employee disabled")
	}
	r, err := s.sourceIDStore()
	if err != nil {
		return nil, ctx, err
	}
	user, err := r.ResolveSourceIDIdentity(ctx, cfg.IssuerURL, info.Subject, employee)
	if err != nil {
		return nil, ctx, err
	}
	session := &types.SourceIDSession{ID: uuid.NewString(), Issuer: cfg.IssuerURL, UserID: user.ID, SessionIndexHash: apprepo.SourceIDHash(cfg.IssuerURL + "\x00" + index), ExpiresAt: time.Now().Add(7 * 24 * time.Hour), CreatedAt: time.Now()}
	if err := r.CreateSourceIDSession(ctx, session); err != nil {
		return nil, ctx, err
	}
	if err := r.CheckSourceIDSession(ctx, session.ID, user.ID); err != nil {
		return nil, ctx, err
	}
	return user, context.WithValue(ctx, sourceIDSessionKey, session.ID), nil
}

func (s *userService) CheckSourceIDTokenSession(ctx context.Context, token *types.AuthToken) error {
	if token.SessionID == "" {
		return nil
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
	token, err := s.tokenRepo.GetTokenByValue(ctx, access)
	if err != nil {
		return ctx, err
	}
	if token == nil || token.IsRevoked || token.TokenType != "access_token" {
		return ctx, errors.New("invalid access token")
	}
	if err := s.CheckSourceIDTokenSession(ctx, token); err != nil {
		return ctx, err
	}
	if token.SessionID != "" {
		ctx = context.WithValue(ctx, sourceIDSessionKey, token.SessionID)
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
