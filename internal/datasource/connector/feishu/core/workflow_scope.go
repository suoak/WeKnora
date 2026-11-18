package core

import (
	"context"
	"sync"
)

type workflowScopeKey struct{}

// A scope belongs to one in-process app policy and one active outer workflow.
// Synchronous document reparses and snapshot checks share that workflow's slot;
// independent callers still acquire their own. This is never source authority.
type workflowScope struct {
	mu      sync.Mutex
	policy  *appPolicy
	ctx     context.Context
	cancel  context.CancelFunc
	release func()
	refs    int
	closed  bool
}

func (s *workflowScope) releaser(outer bool) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			if outer {
				s.closed = true
			}
			s.refs--
			last := s.refs == 0
			s.mu.Unlock()
			if outer {
				s.cancel()
			}
			if last {
				s.release()
			}
		})
	}
}

func (p *appPolicy) workflowContext(ctx context.Context) (context.Context, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if scope, ok := ctx.Value(workflowScopeKey{}).(*workflowScope); ok && scope.policy == p {
		scope.mu.Lock()
		err := scope.ctx.Err()
		if scope.closed || err != nil {
			scope.mu.Unlock()
			if err == nil {
				err = context.Canceled
			}
			return nil, nil, err
		}
		scope.refs++
		scope.mu.Unlock()
		return ctx, scope.releaser(false), nil
	}
	release, err := p.acquireWorkflow(ctx)
	if err != nil {
		return nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		release()
		return nil, nil, err
	}
	owned, cancel := context.WithCancel(ctx)
	scope := &workflowScope{policy: p, ctx: owned, cancel: cancel, release: release, refs: 1}
	return context.WithValue(owned, workflowScopeKey{}, scope), scope.releaser(true), nil
}
