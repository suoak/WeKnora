package api

import (
	"context"
	"errors"
	"fmt"
	"github.com/Tencent/WeKnora/internal/types"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestB2NormalizeStreamFailure(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want types.StreamFailureCode
	}{
		{fmt.Errorf("TOKEN_SQL_BEARER_CANARY: %w", context.Canceled), types.StreamFailureCanceled},
		{context.DeadlineExceeded, types.StreamFailureDeadline},
		{&HTTPError{StatusCode: 429, Body: "TOKEN_CANARY", Header: http.Header{"Retry-After": []string{"100"}}}, types.StreamFailureRateLimited},
		{&HTTPError{StatusCode: 503, Body: "SQL_CANARY"}, types.StreamFailureServerUnavailable},
		{&HTTPError{StatusCode: 400, Body: "502 rate limit context_length_exceeded"}, types.StreamFailureRequestRejected},
		{&TransportError{Err: io.ErrUnexpectedEOF}, types.StreamFailureTransportInterrupted},
		{errors.New("rate limit 502 context_length_exceeded TOKEN_CANARY"), types.StreamFailureUnknown},
	} {
		e := NormalizeStreamFailure(context.Background(), tc.err)
		if e.Code() != tc.want || errors.Unwrap(e) != nil {
			t.Fatalf("safe class=%s", e.CodeName())
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if NormalizeStreamFailure(ctx, &HTTPError{StatusCode: 503}).Code() != types.StreamFailureCanceled {
		t.Fatal("parent priority")
	}
}
func TestB2ProviderStructuredMapping(t *testing.T) {
	for _, tc := range []struct {
		p      string
		code   any
		kind   string
		status int
		name   string
		want   types.StreamFailureCode
	}{
		{"openai-responses", "server_error", "", 0, "", types.StreamFailureServerUnavailable},
		{"openai-completions", "context_length_exceeded", "invalid_request_error", 0, "", types.StreamFailureContextOverflow},
		{"openai-responses", "context_length_exceeded", "", 0, "", types.StreamFailureUnknown},
		{"openai-completions", map[string]any{"message": "rate limit"}, "", 0, "", types.StreamFailureUnknown},
		{"anthropic-messages", nil, "overloaded_error", 0, "", types.StreamFailureServerUnavailable},
		{"anthropic-messages", nil, "request_too_large", 0, "", types.StreamFailureRequestRejected},
		{"google-genai", nil, "", 429, "RESOURCE_EXHAUSTED", types.StreamFailureRateLimited},
		{"google-genai", nil, "", 429, "INVALID_ARGUMENT", types.StreamFailureUnknown},
	} {
		if e := ProviderStreamFailure(tc.p, tc.code, tc.kind, tc.status, tc.name); e.Code() != tc.want {
			t.Fatalf("profile %s class %s", tc.p, e.CodeName())
		}
	}
	e := NormalizeProviderStreamFailure(context.Background(), "openai-completions", &HTTPError{StatusCode: 401, Body: `{"error":{"code":"context_length_exceeded"}}`})
	if e.Code() != types.StreamFailureRequestRejected {
		t.Fatal("auth bypass")
	}
	e = NormalizeProviderStreamFailure(context.Background(), "openai-completions", &HTTPError{StatusCode: 400, Body: `{"error":{"code":"server_error"}}`})
	if e.Code() != types.StreamFailureRequestRejected {
		t.Fatal("status contradiction")
	}
	e = NormalizeStreamFailure(context.Background(), &HTTPError{StatusCode: 429, Header: http.Header{"Retry-After": []string{"100"}}})
	if e.RetryAfter() != 30*time.Second {
		t.Fatal("retry cap")
	}
}
