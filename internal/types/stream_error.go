package types

import (
	"context"
	"time"
)

// StreamFailureCode is a finite, internal recovery classification. It never
// contains provider messages or caller-controlled diagnostic text.
type StreamFailureCode uint8

const (
	StreamFailureUnknown StreamFailureCode = iota
	StreamFailureCanceled
	StreamFailureDeadline
	StreamFailureRateLimited
	StreamFailureServerUnavailable
	StreamFailureTransportInterrupted
	StreamFailureStalled
	StreamFailureIncomplete
	StreamFailureContextOverflow
	StreamFailureSafetyBlocked
	StreamFailureProtocol
	StreamFailureRequestRejected
)

// StreamFailure intentionally cannot retain an original error, body or header.
// All fields are private, bounded values. JSON never exports internal policy.
type StreamFailure struct {
	code       StreamFailureCode
	status     int
	retryAfter time.Duration
}

func NewStreamFailure(code StreamFailureCode, status int, retryAfter time.Duration) *StreamFailure {
	if code > StreamFailureRequestRejected {
		code = StreamFailureUnknown
	}
	if status < 100 || status > 599 {
		status = 0
	}
	if retryAfter < 0 {
		retryAfter = 0
	}
	if retryAfter > 30*time.Second {
		retryAfter = 30 * time.Second
	}
	if code != StreamFailureRateLimited && code != StreamFailureServerUnavailable {
		retryAfter = 0
	}
	return &StreamFailure{code: code, status: status, retryAfter: retryAfter}
}
func (e *StreamFailure) Code() StreamFailureCode {
	if e == nil {
		return StreamFailureUnknown
	}
	return e.code
}
func (e *StreamFailure) Status() int {
	if e == nil {
		return 0
	}
	return e.status
}
func (e *StreamFailure) RetryAfter() time.Duration {
	if e == nil {
		return 0
	}
	return e.retryAfter
}
func (e *StreamFailure) PublicContent() string {
	switch e.Code() {
	case StreamFailureCanceled:
		return context.Canceled.Error()
	case StreamFailureDeadline:
		return context.DeadlineExceeded.Error()
	case StreamFailureIncomplete:
		return StreamEndedEarlyError
	default:
		return "stream_error: stream failed"
	}
}
func (e *StreamFailure) Error() string { return e.PublicContent() }
func (e *StreamFailure) Is(target error) bool {
	return e.Code() == StreamFailureCanceled && target == context.Canceled || e.Code() == StreamFailureDeadline && target == context.DeadlineExceeded
}
func (e *StreamFailure) MarshalJSON() ([]byte, error) { return []byte(`{}`), nil }
func (e *StreamFailure) Retryable() bool {
	switch e.Code() {
	case StreamFailureDeadline, StreamFailureRateLimited, StreamFailureServerUnavailable, StreamFailureTransportInterrupted, StreamFailureStalled, StreamFailureIncomplete:
		return true
	default:
		return false
	}
}
func (e *StreamFailure) CodeName() string {
	switch e.Code() {
	case StreamFailureCanceled:
		return "context_canceled"
	case StreamFailureDeadline:
		return "context_deadline_exceeded"
	case StreamFailureRateLimited:
		return "rate_limited"
	case StreamFailureServerUnavailable:
		return "server_unavailable"
	case StreamFailureTransportInterrupted:
		return "transport_interrupted"
	case StreamFailureStalled:
		return "stalled"
	case StreamFailureIncomplete:
		return "incomplete"
	case StreamFailureContextOverflow:
		return "context_overflow"
	case StreamFailureSafetyBlocked:
		return "safety_blocked"
	case StreamFailureProtocol:
		return "protocol_error"
	case StreamFailureRequestRejected:
		return "request_rejected"
	default:
		return "stream_error"
	}
}
