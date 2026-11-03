package service

import (
	"context"
	"errors"
	"sync"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
)

// These three independent checks use the same immutable personal identity and
// existing client request budgets/stop controls. Nothing is cached: each proof
// waits for all three CURRENT answers, and any missing/error answer stays closed.
func inspectExactPersonalPermissions(
	ctx context.Context, check func(context.Context, string) (bool, error),
) (core.PersonalPermissions, error) {
	if err := ctx.Err(); err != nil {
		return core.PersonalPermissions{}, errors.Join(core.ErrPersonalBlocksPolicyUnverified, err)
	}
	actions := [...]string{"view", "export", "copy"}
	var allowed [3]bool
	var failures [3]error
	var pending sync.WaitGroup
	for index, action := range actions {
		pending.Add(1)
		go func(index int, action string) {
			defer pending.Done()
			allowed[index], failures[index] = check(ctx, action)
		}(index, action)
	}
	pending.Wait()
	if err := errors.Join(failures[:]...); err != nil {
		return core.PersonalPermissions{}, errors.Join(core.ErrPersonalBlocksPolicyUnverified, err)
	}
	if err := ctx.Err(); err != nil {
		return core.PersonalPermissions{}, errors.Join(core.ErrPersonalBlocksPolicyUnverified, err)
	}
	return core.PersonalPermissions{View: allowed[0], Export: allowed[1], Copy: allowed[2]}, nil
}
