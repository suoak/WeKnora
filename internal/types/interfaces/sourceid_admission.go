package interfaces

import (
	"context"
	"github.com/Tencent/WeKnora/internal/types"
)

// SourceIDEmployeeAdmissionResolver is an independently confirmed SID adapter.
// No real adapter is registered until endpoint/field, authority, freshness,
// subject binding and recheck semantics have been formally provided.
type SourceIDEmployeeAdmissionResolver interface {
	Resolve(context.Context, types.SourceIDAdmissionRequest) (types.SourceIDAdmissionDecision, error)
}
