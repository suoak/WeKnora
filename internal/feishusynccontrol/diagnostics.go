package feishusynccontrol

import (
	"context"
	"errors"
	"net"
	"strings"
)

var (
	errConsumerInactive = errors.New("sync consumer inactive")
	errConsumerAbsent   = errors.New("sync consumer absent")
)

type probeFailure struct {
	stage string
	cause error
}

func (e *probeFailure) Error() string { return "consumer probe failed" }
func (e *probeFailure) Unwrap() error { return e.cause }

// failureClass emits only fixed protocol categories. Redis/HTTP errors may
// contain addresses, credentials or document information and are never logged.
func failureClass(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "context_deadline"
	}
	if errors.Is(err, context.Canceled) {
		return "context_canceled"
	}
	if errors.Is(err, errConsumerAbsent) {
		return "consumer_absent"
	}
	if errors.Is(err, errConsumerInactive) {
		return "consumer_inactive"
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return "redis_timeout"
	}
	for _, code := range []string{
		"POLICY_MISMATCH", "WORKER_LOST", "CONSUMER_LOST", "UNREGISTERED_CONSUMER",
		"CONSUMERS_NOT_READY", "WORKER_ID_CONFLICT", "TRIPPED", "ADMITTED_INCARNATION_CONFLICT",
		"INCARNATION_CONFLICT", "CONSUMER_START_TIMEOUT",
	} {
		if err != nil && strings.Contains(err.Error(), code) {
			return strings.ToLower(code)
		}
	}
	return "redis_or_probe_error"
}

func automaticFailureReason(stage string, err error) string {
	var probe *probeFailure
	if errors.As(err, &probe) {
		stage = probe.stage
	}
	return "automatic_" + stage + "_" + failureClass(err)
}
