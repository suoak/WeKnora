package core

import (
	"net/http"
	"testing"
)

func TestExport9499RequiresEndpointAndExactThrottlingMessage(t *testing.T) {
	for _, tc := range []struct {
		operation, message string
		want               ErrorCategory
	}{
		{"create_export_task", "too many request", ErrorCategoryRateLimit},
		{"get_export_task_status", " Too Many Requests. ", ErrorCategoryRateLimit},
		{"create_export_task", "Invalid parameter type in json: id", ErrorCategoryPermanent},
		{"create_export_task", "unknown error", ErrorCategoryPermanent},
		{"create_export_task", "too many requests but permission denied", ErrorCategoryPermanent},
		{"resolve_personal_resource", "too many request", ErrorCategoryPermanent},
	} {
		t.Run(tc.operation+"/"+tc.message, func(t *testing.T) {
			got, reason := classifyOperationAPIError(tc.operation, http.StatusBadRequest, 9499, tc.message)
			if got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
			if got == ErrorCategoryRateLimit && reason != "export_throttled" {
				t.Fatal("lost fixed provider reason")
			}
		})
	}
}
