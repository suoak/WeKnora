package core

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ErrorCategory is the stable machine-readable classification assigned to a
// failed Feishu request before it leaves the HTTP client layer.
type ErrorCategory string

const (
	ErrorCategoryRateLimit  ErrorCategory = "rate_limit"
	ErrorCategoryAuth       ErrorCategory = "auth"
	ErrorCategoryPermission ErrorCategory = "permission"
	ErrorCategoryNotFound   ErrorCategory = "not_found"
	ErrorCategoryTransient  ErrorCategory = "transient"
	ErrorCategoryPermanent  ErrorCategory = "permanent"
	ErrorCategoryUnknownAPI ErrorCategory = "unknown_api"
)

var (
	ErrRateLimited = errors.New("feishu rate limited")
	ErrAuth        = errors.New("feishu authentication failed")
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
	if e.Message != "" {
		parts = append(parts, "msg="+e.Message)
	}
	return strings.Join(parts, " ")
}

func (e *APIError) Unwrap() error { return e.Cause }

func (e *APIError) Is(target error) bool {
	if e == nil {
		return false
	}
	switch target {
	case ErrRateLimited:
		return e.Category == ErrorCategoryRateLimit
	case ErrAuth:
		return e.Category == ErrorCategoryAuth
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
	knownRateLimitCodes  = map[int]struct{}{99991400: {}}
	knownAuthCodes       = map[int]struct{}{99991663: {}}
	knownPermissionCodes = map[int]struct{}{99991672: {}}
	knownNotFoundCodes   = map[int]struct{}{1663: {}}
)

func classifyAPIError(httpStatus, code int) ErrorCategory {
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
