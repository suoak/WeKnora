package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// KnowledgeDispatchRepository reuses the generic durable pending-op table.
// Implementations must atomically accept a Knowledge and its immutable intent.
type KnowledgeDispatchRepository interface {
	AcceptKnowledgeDispatch(context.Context, *types.Knowledge, *types.TaskPendingOp) error
	FindFeishuPersonalKnowledge(context.Context, uint64, string, string, string) (*types.Knowledge, error)
	EnsureKnowledgeDispatch(context.Context, *types.Knowledge, *types.TaskPendingOp) error
	GetKnowledgeDispatch(context.Context, uint64, string) (*types.TaskPendingOp, error)
	ListKnowledgeDispatches(context.Context, int64, int) ([]*types.TaskPendingOp, error)
	SetKnowledgeDispatchState(context.Context, int64, string) error
	RecordKnowledgeDispatchFailure(context.Context, int64, string) error
	WithKnowledgeDispatchLock(context.Context, string, func(context.Context) error) error
}

// KnowledgeDispatchRecovery defines knowledge dispatch recovery state.
type KnowledgeDispatchRecovery interface {
	ResumeKnowledgeDispatch(context.Context, *types.Knowledge) error
	RecoverKnowledgeDispatches(context.Context) error
}
