package api

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// NormalizeStreamFailure drops all original causes before logging or emitting.
func NormalizeStreamFailure(ctx context.Context, err error) *types.StreamFailure {
	if ctx != nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	if errors.Is(err, context.Canceled) {
		return types.NewStreamFailure(types.StreamFailureCanceled, 0, 0)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return types.NewStreamFailure(types.StreamFailureDeadline, 0, 0)
	}
	var safe *types.StreamFailure
	if errors.As(err, &safe) {
		return types.NewStreamFailure(safe.Code(), safe.Status(), safe.RetryAfter())
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return failureForStatus(httpErr.StatusCode, httpErr.RetryAfter())
	}
	var cert x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	var host x509.HostnameError
	if errors.As(err, &cert) || errors.As(err, &invalid) || errors.As(err, &host) {
		return types.NewStreamFailure(types.StreamFailureRequestRejected, 0, 0)
	}
	var transport *TransportError
	var network net.Error
	if errors.As(err, &transport) || errors.Is(err, io.ErrUnexpectedEOF) || errors.As(err, &network) && network.Timeout() {
		return types.NewStreamFailure(types.StreamFailureTransportInterrupted, 0, 0)
	}
	return types.NewStreamFailure(types.StreamFailureUnknown, 0, 0)
}
func failureForStatus(status int, wait time.Duration) *types.StreamFailure {
	code := types.StreamFailureUnknown
	switch {
	case status == 408:
		code = types.StreamFailureDeadline
	case status == 429:
		code = types.StreamFailureRateLimited
	case status >= 500 && status <= 599:
		code = types.StreamFailureServerUnavailable
	case status >= 400 && status <= 499:
		code = types.StreamFailureRequestRejected
	}
	return types.NewStreamFailure(code, status, wait)
}

// ProviderStreamFailure only reads the selected protocol's exact typed fields.
// code objects, free text and contradictory fields never grant recovery.
func ProviderStreamFailure(profile string, code any, kind string, status int, statusName string) *types.StreamFailure {
	result := types.StreamFailureUnknown
	value, isString := code.(string)
	switch profile {
	case "openai-completions", "openai-responses":
		if code != nil && !isString {
			break
		}
		switch value {
		case "rate_limit_exceeded":
			result = types.StreamFailureRateLimited
		case "server_error":
			result = types.StreamFailureServerUnavailable
		case "insufficient_quota":
			result = types.StreamFailureRequestRejected
		case "context_length_exceeded":
			// This exact error.code is supported by the Completions profile only.
			// Responses' documented ResponseError does not promise this code.
			if profile == "openai-completions" {
				result = types.StreamFailureContextOverflow
			}
		default:
			if value == "" && (kind == "invalid_request_error" || kind == "authentication_error" || kind == "permission_error") {
				result = types.StreamFailureRequestRejected
			}
		}
	case "anthropic-messages":
		switch kind {
		case "rate_limit_error":
			result = types.StreamFailureRateLimited
		case "api_error", "overloaded_error":
			result = types.StreamFailureServerUnavailable
		case "invalid_request_error", "authentication_error", "permission_error", "request_too_large", "not_found_error":
			result = types.StreamFailureRequestRejected
		}
	case "google-genai":
		if status <= 0 {
			break
		}
		expected := map[int]string{400: "INVALID_ARGUMENT", 401: "UNAUTHENTICATED", 403: "PERMISSION_DENIED", 404: "NOT_FOUND", 408: "DEADLINE_EXCEEDED", 429: "RESOURCE_EXHAUSTED", 500: "INTERNAL", 502: "BAD_GATEWAY", 503: "UNAVAILABLE", 504: "DEADLINE_EXCEEDED"}
		name, ok := expected[status]
		if !ok || statusName != "" && statusName != name {
			break
		}
		return failureForStatus(status, 0)
	}
	return types.NewStreamFailure(result, 0, 0)
}

// NormalizeProviderStreamFailure interprets only a bounded JSON error envelope,
// then discards HTTP body/headers. It does not change Endpoint.Do or Chat.
func NormalizeProviderStreamFailure(ctx context.Context, profile string, err error) *types.StreamFailure {
	safe := NormalizeStreamFailure(ctx, err)
	if safe.Code() == types.StreamFailureCanceled || safe.Code() == types.StreamFailureDeadline {
		return safe
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		return safe
	}
	if httpErr.StatusCode == 401 || httpErr.StatusCode == 403 {
		return safe
	}
	var env struct {
		Error *struct {
			Code   any    `json:"code"`
			Type   string `json:"type"`
			Status string `json:"status"`
		} `json:"error"`
	}
	if len(httpErr.Body) > 1<<20 || json.Unmarshal([]byte(httpErr.Body), &env) != nil || env.Error == nil {
		return safe
	}
	structured := ProviderStreamFailure(profile, env.Error.Code, env.Error.Type, 0, env.Error.Status)
	if structured.Code() == types.StreamFailureRequestRejected {
		return types.NewStreamFailure(structured.Code(), httpErr.StatusCode, 0)
	}
	if httpErr.StatusCode == 400 && structured.Code() == types.StreamFailureContextOverflow {
		return types.NewStreamFailure(structured.Code(), 400, 0)
	}
	// HTTP status is authoritative: a body cannot turn a 400 into retryable 503.
	return safe
}
