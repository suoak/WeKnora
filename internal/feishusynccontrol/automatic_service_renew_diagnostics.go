package feishusynccontrol

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"gorm.io/gorm"
)

// Provider/SQL errors can carry URLs, parameters or secrets. Only fixed labels
// are retained, including when unknown errors recur without a successful Step.
func systemServiceCanaryClass(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canary_context_canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "canary_context_deadline"
	case errors.Is(err, gorm.ErrRecordNotFound):
		return "canary_record_not_found"
	case errors.Is(err, errCanaryEvidenceLimit):
		return "canary_evidence_limit"
	case errors.Is(err, ErrNotReady), errors.Is(err, ErrRecoveryRejected):
		return "canary_evidence_not_ready"
	}
	var state interface{ SQLState() string }
	if errors.As(err, &state) {
		switch state.SQLState() {
		case "40001", "40P01":
			return "canary_sql_concurrency"
		case "57014":
			return "canary_sql_canceled"
		case "08006", "57P01":
			return "canary_sql_connection"
		case "23503", "23505", "23514":
			return "canary_sql_integrity"
		case "42P01", "42703":
			return "canary_sql_schema"
		default:
			return "canary_sql_query"
		}
	}
	var syntax *json.SyntaxError
	var value *json.UnmarshalTypeError
	if errors.As(err, &syntax) || errors.As(err, &value) {
		return "canary_receipt_decode"
	}
	if label := systemServiceRenewClass(err); label != "renew_redis_read_error" {
		return "canary_" + strings.TrimPrefix(label, "renew_")
	}
	return "canary_internal_error"
}
