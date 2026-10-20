package mcp

import (
	"context"
	"errors"
	"strings"

	"github.com/mark3labs/mcp-go/client/transport"
)

// OAuthSafeError deliberately does not retain provider bodies or SQL errors.
// Code identifies the failed operation; ProviderCode is an allowlisted class.
// Only cancellation/deadline sentinels are retained for lifecycle decisions.
type OAuthSafeError struct {
	Code         string
	ProviderCode string
	cause        error
}

func (e *OAuthSafeError) Error() string {
	if e.ProviderCode != "" {
		return e.Code + ": " + e.ProviderCode
	}
	return e.Code
}
func (e *OAuthSafeError) Unwrap() error { return e.cause }
func safeOAuthError(code string, err error) error {
	if err == nil {
		return nil
	}
	e := &OAuthSafeError{Code: code}
	var provider transport.OAuthError
	if errors.As(err, &provider) {
		e.ProviderCode = safeOAuthProviderCode(provider.ErrorCode)
	}
	var wire *transport.Error
	if errors.As(err, &wire) && wire.Err != nil {
		for _, reason := range []string{"Invalid session ID", "No active connection"} {
			if strings.Contains(wire.Err.Error(), reason) {
				e.cause = &transport.Error{Err: errors.New(reason)}
				break
			}
		}
	}
	if errors.Is(err, transport.ErrSessionTerminated) {
		e.cause = &transport.Error{Err: transport.ErrSessionTerminated}
	}
	if errors.Is(err, context.Canceled) {
		e.cause = context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		e.cause = context.DeadlineExceeded
	}
	return e
}

func safeOAuthProviderCode(code string) string {
	switch code {
	case "invalid_request",
		"invalid_client",
		"invalid_grant",
		"unauthorized_client",
		"unsupported_grant_type",
		"invalid_scope",
		"access_denied",
		"server_error",
		"temporarily_unavailable",
		"invalid_token",
		"expired_token",
		"bad_refresh_token":
		return code
	default:
		return "provider_error"
	}
}

// SafeOAuthErrorMessage is the final API/log boundary, including errors from
// state storage and service repositories. Raw causes are never formatted.
func SafeOAuthErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	var safe *OAuthSafeError
	if errors.As(err, &safe) {
		return safe.Error()
	}
	return "mcp_oauth_operation_failed"
}
