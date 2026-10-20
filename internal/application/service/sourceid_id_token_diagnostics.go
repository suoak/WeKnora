package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

func sourceIDIDTokenFailureDiagnostic(ctx context.Context, err error) {
	entry, ok := ctx.Value(sourceIDDiagnosticKey{}).(sourceIDDiagnosticContext)
	if !ok {
		return
	}
	data, _ := json.Marshal(struct {
		Event      string   `json:"event"`
		Trace      string   `json:"trace"`
		Categories []string `json:"categories"`
		AtHash     string   `json:"at_hash_validation"`
	}{"sourceid_id_token_failure", entry.ID, sourceIDIDTokenFailureCategories(err), "NOT_IMPLEMENTED"})
	sourceIDEmitTokenDiagnostic(ctx, data)
}

var (
	errOIDCJWKSFetch    = errors.New("oidc jwks fetch failed")
	errOIDCKeySelection = errors.New("oidc signing key selection failed")
)

// Only code-owned enums leave the verifier. Neither error text nor token claims
// are logged. Multiple claim failures can be joined by the JWT validator.
func sourceIDIDTokenFailureCategories(err error) []string {
	if err == nil {
		return nil
	}
	result := []string{}
	for _, item := range []struct {
		err  error
		name string
	}{
		{errOIDCJWKSFetch, "jwks_fetch"},
		{errOIDCKeySelection, "key_selection"},
		{jwt.ErrTokenMalformed, "parse"},
		{jwt.ErrTokenInvalidIssuer, "issuer"},
		{jwt.ErrTokenInvalidAudience, "audience"},
		{jwt.ErrTokenExpired, "expired"},
		{jwt.ErrTokenUsedBeforeIssued, "iat"},
		{jwt.ErrTokenNotValidYet, "nbf"},
		{jwt.ErrTokenRequiredClaimMissing, "required_claim"},
		{jwt.ErrInvalidType, "claim_type"},
	} {
		if errors.Is(err, item.err) {
			result = append(result, item.name)
		}
	}
	if errors.Is(err, jwt.ErrTokenSignatureInvalid) {
		if strings.Contains(err.Error(), "signing method ") && strings.Contains(err.Error(), " is invalid") {
			result = append(result, "algorithm")
		} else {
			result = append(result, "signature")
		}
	}
	if len(result) == 0 {
		switch err.Error() {
		case "id_token missing issued-at":
			result = append(result, "iat_missing")
		case "id_token authorized party mismatch":
			result = append(result, "authorized_party")
		case "id_token missing sub claim":
			result = append(result, "subject_missing")
		case "OIDC ID Token nonce mismatch":
			result = append(result, "nonce")
		case "missing nonce":
			result = append(result, "nonce_missing")
		default:
			result = append(result, "unknown")
		}
	}
	return result
}
