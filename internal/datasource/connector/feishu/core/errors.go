package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// ErrorCategory is the stable machine-readable classification assigned to a
// failed Feishu request before it leaves the HTTP client layer.
type ErrorCategory string

// Stable Feishu API error categories used by retry and presentation policy.
const (
	ErrorCategoryRateLimit  ErrorCategory = "rate_limit"
	ErrorCategoryAuth       ErrorCategory = "auth"
	ErrorCategoryAppScope   ErrorCategory = "app_scope_missing"
	ErrorCategoryUserScope  ErrorCategory = "user_scope_missing"
	ErrorCategoryPermission ErrorCategory = "permission"
	ErrorCategoryNotFound   ErrorCategory = "not_found"
	ErrorCategoryTransient  ErrorCategory = "transient"
	ErrorCategoryPermanent  ErrorCategory = "permanent"
	ErrorCategoryUnknownAPI ErrorCategory = "unknown_api"
)

// Sentinel errors support errors.Is checks without exposing response bodies.
var (
	ErrRateLimited = errors.New("feishu rate limited")
	ErrAuth        = errors.New("feishu authentication failed")
	ErrAppScope    = errors.New("feishu application scope missing")
	ErrUserScope   = errors.New("feishu user scope missing")
	ErrPermission  = errors.New("feishu permission denied")
	ErrNotFound    = errors.New("feishu resource not found")
	ErrTransient   = errors.New("feishu transient failure")
	ErrPermanent   = errors.New("feishu permanent failure")
	ErrUnknownAPI  = errors.New("feishu unclassified api failure")
)

// APIError preserves the structured fields needed by retry policy, logging and
// UI-safe error mapping. Body and credentials are deliberately not retained.
type APIError struct {
	Category   ErrorCategory
	Code       int
	Message    string
	HTTPStatus int
	Operation  string
	RequestID  string
	Scopes     []string
	RetryAfter time.Duration
	Cause      error
}

func (e *APIError) Error() string {
	if e == nil {
		return "<nil>"
	}
	parts := []string{"feishu api request failed", "category=" + string(e.Category)}
	if e.Operation != "" {
		parts = append(parts, "operation="+e.Operation)
	}
	if e.Code != 0 {
		parts = append(parts, fmt.Sprintf("code=%d", e.Code))
	}
	if e.HTTPStatus != 0 {
		parts = append(parts, fmt.Sprintf("status=%d", e.HTTPStatus))
	}
	if e.RequestID != "" {
		parts = append(parts, "request_id_hash="+tokenFingerprint(e.RequestID)[:12])
	}
	var timeout net.Error
	if errors.Is(e.Cause, context.DeadlineExceeded) ||
		(errors.As(e.Cause, &timeout) && timeout.Timeout()) {
		parts = append(parts, "cause=timeout")
	}
	return strings.Join(parts, " ")
}

func (e *APIError) Unwrap() error { return e.Cause }

// Is matches APIError categories to their stable sentinel errors.
func (e *APIError) Is(target error) bool {
	if e == nil {
		return false
	}
	switch target {
	case ErrRateLimited:
		return e.Category == ErrorCategoryRateLimit
	case ErrAuth:
		return e.Category == ErrorCategoryAuth
	case ErrAppScope:
		return e.Category == ErrorCategoryAppScope
	case ErrUserScope:
		return e.Category == ErrorCategoryUserScope
	case ErrPermission:
		return e.Category == ErrorCategoryPermission
	case ErrNotFound:
		return e.Category == ErrorCategoryNotFound
	case ErrTransient:
		return e.Category == ErrorCategoryTransient
	case ErrPermanent:
		return e.Category == ErrorCategoryPermanent
	case ErrUnknownAPI:
		return e.Category == ErrorCategoryUnknownAPI
	default:
		return false
	}
}

// Only codes already evidenced by this connector's API fixtures are listed.
// Unknown application codes stay UnknownAPI rather than being guessed.
var (
	knownRateLimitCodes = map[int]struct{}{99991400: {}}
	knownAuthCodes      = map[int]struct{}{
		99991661: {},
		99991663: {},
		99991668: {},
		99991671: {},
		99991677: {},
	}
	knownPermissionCodes = map[int]struct{}{131006: {}, 1770032: {}, 1069902: {}}
	knownNotFoundCodes   = map[int]struct{}{1663: {}}
)

func classifyAPIError(httpStatus, code int) ErrorCategory {
	// These are scope errors even when the provider uses HTTP 400/403. They
	// must never become a document-owner export refusal or trigger a token swap.
	if code == 99991672 {
		return ErrorCategoryAppScope
	}
	if code == 99991676 || code == 99991679 {
		return ErrorCategoryUserScope
	}
	if _, ok := knownRateLimitCodes[code]; ok || httpStatus == http.StatusTooManyRequests {
		return ErrorCategoryRateLimit
	}
	if _, ok := knownAuthCodes[code]; ok || httpStatus == http.StatusUnauthorized {
		return ErrorCategoryAuth
	}
	if _, ok := knownPermissionCodes[code]; ok || httpStatus == http.StatusForbidden {
		return ErrorCategoryPermission
	}
	if _, ok := knownNotFoundCodes[code]; ok || httpStatus == http.StatusNotFound {
		return ErrorCategoryNotFound
	}
	if httpStatus >= 500 && httpStatus <= 599 {
		return ErrorCategoryTransient
	}
	if httpStatus >= 400 && httpStatus <= 499 {
		return ErrorCategoryPermanent
	}
	if code != 0 {
		return ErrorCategoryUnknownAPI
	}
	return ErrorCategoryPermanent
}

func asAPIError(err error) (*APIError, bool) {
	var apiErr *APIError
	ok := errors.As(err, &apiErr)
	return apiErr, ok
}

func isRetryableAPIError(err error) bool {
	return errors.Is(err, ErrRateLimited) || errors.Is(err, ErrTransient)
}
