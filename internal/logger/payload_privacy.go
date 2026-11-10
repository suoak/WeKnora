package logger

import (
	"context"
	"io"

	"github.com/sirupsen/logrus"
)

type payloadPrivacyKey struct{}

// WithPrivatePayload prevents arbitrary downstream diagnostics from becoming a
// second transport for MCP input or retrieved content. Admission/access metrics
// must use an explicit, fixed metadata projection outside this context.
func WithPrivatePayload(ctx context.Context) context.Context {
	return context.WithValue(ctx, payloadPrivacyKey{}, true)
}

func PrivatePayload(ctx context.Context) bool {
	return ctx != nil && ctx.Value(payloadPrivacyKey{}) == true
}

var privatePayloadLogger = func() *logrus.Logger {
	l := logrus.New()
	l.SetOutput(io.Discard)
	return l
}()
