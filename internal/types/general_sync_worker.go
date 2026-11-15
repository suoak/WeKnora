package types

import (
	"context"
	"errors"
)

// ErrGeneralSyncCapacityReserved rejects a Feishu task on the general-only lane.
var ErrGeneralSyncCapacityReserved = errors.New("general sync capacity is reserved for non-Feishu sources")

type (
	generalSyncWorkerKey struct{}
	generalSyncBudgetKey struct{}
)

// WithGeneralSyncRetryBudget binds a native queue budget to SQL generations.
func WithGeneralSyncRetryBudget(ctx context.Context, retries int) context.Context {
	return context.WithValue(ctx, generalSyncBudgetKey{}, int64(max(0, retries)))
}

// GeneralSyncRetryBudget prevents two delivery lanes from multiplying attempts.
func GeneralSyncRetryBudget(ctx context.Context) (int64, bool) {
	limit, ok := ctx.Value(generalSyncBudgetKey{}).(int64)
	return limit, ok
}

// WithGeneralSyncWorker marks only the dedicated server's execution context.
func WithGeneralSyncWorker(ctx context.Context) context.Context {
	return context.WithValue(ctx, generalSyncWorkerKey{}, true)
}

// IsGeneralSyncWorker never derives worker identity from client/task payloads.
func IsGeneralSyncWorker(ctx context.Context) bool {
	marked, _ := ctx.Value(generalSyncWorkerKey{}).(bool)
	return marked
}

// FeishuConnectorType includes the international and Drive variants.
func FeishuConnectorType(kind string) bool {
	switch kind {
	case ConnectorTypeFeishu, ConnectorTypeFeishuDrive, ConnectorTypeLark, ConnectorTypeLarkDrive:
		return true
	}
	return false
}
