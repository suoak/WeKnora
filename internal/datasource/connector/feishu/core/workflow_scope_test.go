package core

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestWorkflowScopeRetainsBoundAndRejectsExpiredOwnership(t *testing.T) {
	p := &appPolicy{workflowGate: make(chan struct{}, 1)}
	ctx, outer, err := p.workflowContext(context.Background())
	require.NoError(t, err)
	_, nested, err := p.workflowContext(ctx)
	require.NoError(t, err)
	require.Len(t, p.workflowGate, 1)
	independent, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, _, err = p.workflowContext(independent)
	require.ErrorIs(t, err, context.DeadlineExceeded, "independent work cannot borrow another workflow's slot")
	outer()
	outer()
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.Len(t, p.workflowGate, 1, "retain the gate until an already-admitted child exits")
	_, _, err = p.workflowContext(context.WithoutCancel(ctx))
	require.ErrorIs(t, err, context.Canceled, "detaching cancellation cannot revive an expired scope")
	nested()
	nested()
	require.Empty(t, p.workflowGate)
	_, next, err := p.workflowContext(context.Background())
	require.NoError(t, err)
	next()
	require.Empty(t, p.workflowGate)
}

func TestWorkflowScopeCannotBorrowAnotherAppPolicy(t *testing.T) {
	first := &appPolicy{workflowGate: make(chan struct{}, 1)}
	second := &appPolicy{workflowGate: make(chan struct{}, 1)}
	ctx, release, err := first.workflowContext(context.Background())
	require.NoError(t, err)
	defer release()
	_, held, err := second.workflowContext(context.Background())
	require.NoError(t, err)
	blocked, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	_, _, err = second.workflowContext(blocked)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	held()
	_, granted, err := second.workflowContext(ctx)
	require.NoError(t, err)
	granted()
	require.NoError(t, ctx.Err(), "releasing the second policy must not cancel its parent")
}

func TestWorkflowDocumentSnapshotReacquiresIndependentAdmission(t *testing.T) {
	p := &appPolicy{workflowGate: make(chan struct{}, 1)}
	client := &Client{policy: p}
	ops := &documentFetchOps{nodes: []string{"target"}}
	config := &types.DataSourceConfig{ResourceIDs: []string{"root"}}
	item, err := FetchDocumentEngine(context.Background(), client, config, "root", "target", ops)
	require.NoError(t, err)
	require.Empty(t, p.workflowGate)
	held, err := p.acquireWorkflow(context.Background())
	require.NoError(t, err)
	blocked, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = item.CurrentSnapshot(blocked)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, 1, ops.lists, "blocked snapshot must not perform source I/O")
	held()
	_, err = item.CurrentSnapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, ops.lists)
	require.Empty(t, p.workflowGate)
}
