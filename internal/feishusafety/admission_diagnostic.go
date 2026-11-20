package feishusafety

import (
	"context"
	"errors"
	"net"
	"strings"
)

// Persist only finite, content-free categories alongside the original stop.
// Rotated application logs must not erase the distinction between a rejected
// reservation, a missing permit and an unavailable accounting connection.
// This function diagnoses failures; it never changes admission or recovery.
func embeddingAdmissionCode(err error) string {
	if errors.Is(err, errSourceProbeFailed) {
		return "source_probe_failed"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "context_deadline"
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return "network_timeout"
	}
	message := err.Error()
	for _, code := range []string{
		"SAFETY_TRIPPED", "SAFETY_NO_PERMIT", "RECOVERY_MISSING", "RECOVERY_BINDING_MISSING",
		"RECOVERY_EXPIRED", "STEADY_HEALTH_EXPIRED", "APPROVED_SOURCE_DENIED", "RECOVERY_SOURCE_DENIED",
		"RECOVERY_ADMISSION_DENIED", "RECOVERY_RUN_DENIED", "RECOVERY_FROZEN",
	} {
		if strings.Contains(message, code) {
			return strings.ToLower(code)
		}
	}
	if strings.Contains(message, "reused or mismatched embedding reservation") {
		return "reservation_mismatch"
	}
	if strings.Contains(message, "single request exceeds token allocation") {
		return "request_exceeds_allocation"
	}
	return "unclassified"
}
