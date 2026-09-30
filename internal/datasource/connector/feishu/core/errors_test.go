package core

import (
	"errors"
	"net/http"
	"testing"
)

func TestRateLimitErrorClassification(t *testing.T) {
	err := &APIError{
		Category:   classifyAPIError(http.StatusOK, 99991400),
		Code:       99991400,
		HTTPStatus: http.StatusOK,
		Operation:  "create_export_task",
	}
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("errors.Is(%v, ErrRateLimited) = false", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 99991400 {
		t.Fatalf("errors.As APIError = %#v, want code 99991400", apiErr)
	}
}

func TestAPIErrorClassificationKnownCodesAndHTTPStatuses(t *testing.T) {
	tests := []struct {
		name   string
		status int
		code   int
		want   error
	}{
		{name: "http rate limit", status: http.StatusTooManyRequests, want: ErrRateLimited},
		{name: "known auth", status: http.StatusOK, code: 99991663, want: ErrAuth},
		{name: "http auth", status: http.StatusUnauthorized, want: ErrAuth},
		{name: "known permission", status: http.StatusOK, code: 99991672, want: ErrPermission},
		{name: "http permission", status: http.StatusForbidden, want: ErrPermission},
		{name: "known not found", status: http.StatusOK, code: 1663, want: ErrNotFound},
		{name: "http not found", status: http.StatusNotFound, want: ErrNotFound},
		{name: "server failure", status: http.StatusBadGateway, want: ErrTransient},
		{name: "malformed request", status: http.StatusBadRequest, want: ErrPermanent},
		{name: "unknown application code", status: http.StatusOK, code: 987654, want: ErrUnknownAPI},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &APIError{Category: classifyAPIError(tt.status, tt.code), Code: tt.code, HTTPStatus: tt.status}
			if !errors.Is(err, tt.want) {
				t.Fatalf("category=%q does not match %v", err.Category, tt.want)
			}
		})
	}
}
