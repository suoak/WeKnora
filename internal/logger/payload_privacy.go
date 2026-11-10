package logger

import (
	"context"
	"io"

	"github.com/sirupsen/logrus"
)

type (
	payloadPrivacyKey   struct{}
	isolatedPersonalKey struct{}
)

// WithPrivatePayload prevents arbitrary downstream diagnostics from becoming a
// second transport for MCP input or retrieved content. Admission/access metrics
// must use an explicit, fixed metadata projection outside this context.
func WithPrivatePayload(ctx context.Context) context.Context {
	return context.WithValue(ctx, payloadPrivacyKey{}, true)
}

func PrivatePayload(ctx context.Context) bool {
	return ctx != nil && ctx.Value(payloadPrivacyKey{}) == true
}

// WithIsolatedPersonalPayload marks only the independently routed isolation
// entry. The old personal URL keeps its protocol and result contract.
func WithIsolatedPersonalPayload(ctx context.Context) context.Context {
	return context.WithValue(WithPrivatePayload(ctx), isolatedPersonalKey{}, true)
}

func IsolatedPersonalPayload(ctx context.Context) bool {
	return ctx != nil && ctx.Value(isolatedPersonalKey{}) == true
}

var privatePayloadLogger = func() *logrus.Logger {
	l := logrus.New()
	l.SetOutput(io.Discard)
	return l
}()
