// Package logger provides context-aware application logging and SQL diagnostics.
package logger

import (
	"context"
	"time"

	gormlog "gorm.io/gorm/logger"
)

type privateGORMLogger struct{ gormlog.Interface }

// WithPrivateGORMPayload keeps slow/error SQL and interpolated query arguments
// out of diagnostics for private MCP requests, regardless of SQL log level.
func WithPrivateGORMPayload(inner gormlog.Interface) gormlog.Interface {
	return privateGORMLogger{inner}
}

func (l privateGORMLogger) LogMode(level gormlog.LogLevel) gormlog.Interface {
	return privateGORMLogger{l.Interface.LogMode(level)}
}

func (l privateGORMLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if !PrivatePayload(ctx) {
		l.Interface.Trace(ctx, begin, fc, err)
	}
}

func (l privateGORMLogger) Info(ctx context.Context, msg string, args ...any) {
	if !PrivatePayload(ctx) {
		l.Interface.Info(ctx, msg, args...)
	}
}

func (l privateGORMLogger) Warn(ctx context.Context, msg string, args ...any) {
	if !PrivatePayload(ctx) {
		l.Interface.Warn(ctx, msg, args...)
	}
}

func (l privateGORMLogger) Error(ctx context.Context, msg string, args ...any) {
	if !PrivatePayload(ctx) {
		l.Interface.Error(ctx, msg, args...)
	}
}
