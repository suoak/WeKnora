package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
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
	Category       ErrorCategory
	Code           int
	Message        string
	HTTPStatus     int
	Operation      string
	RequestID      string
	Scopes         []string
	RetryAfter     time.Duration
	ProviderReason string
	Cause          error
}

// Export 9499 is overloaded. Only the CLI-evidenced exact rate-limit messages
// at export task endpoints change its category; its raw message is not logged.
func classifyOperationAPIError(operation string, status, code int, message string) (ErrorCategory, string) {
	if code == 9499 && (operation == "create_export_task" || operation == "get_export_task_status") {
		normalized := strings.Trim(strings.ToLower(strings.TrimSpace(message)), " .")
		if normalized == "too many request" || normalized == "too many requests" {
			return ErrorCategoryRateLimit, "export_throttled"
		}
		return classifyAPIError(status, code), "export_9499_unclassified"
	}
	return classifyAPIError(status, code), ""
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
	if e.Cause != nil {
		parts = append(parts, "transport="+TransportErrorCategory(e.Cause))
	}
	return strings.Join(parts, " ")
}

// TransportErrorCategory returns a finite diagnostic label, never a URL, token,
// response or underlying error message. It does not grant retries or admission.
func TransportErrorCategory(err error) string {
	if err == nil {
		return ""
	}
	var network net.Error
	var dns *net.DNSError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "truncated"
	case errors.Is(err, io.EOF):
		return "eof"
	case errors.Is(err, syscall.ECONNRESET):
		return "connection_reset"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection_refused"
	case errors.Is(err, syscall.EPIPE):
		return "broken_pipe"
	case errors.As(err, &dns):
		return "dns"
	case errors.As(err, &network) && network.Timeout():
		return "timeout"
	}
	// net/http's bundled HTTP/2 error types are internal; these fixed markers
	// are diagnostic only, and never enable an alternate content path.
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "goaway"):
		return "http2_goaway"
	case strings.Contains(message, "stream error"):
		return "http2_stream"
	case strings.Contains(message, "client connection lost"):
		return "http2_connection_lost"
	case strings.Contains(message, "certificate") || strings.Contains(message, "x509"):
		return "tls_certificate"
	case strings.Contains(message, "ssrf") || strings.Contains(message, "blocked"):
		return "security_rejected"
	default:
		return "unclassified"
	}
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
