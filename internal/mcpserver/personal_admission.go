package mcpserver

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/perftrace"
)

var errPersonalCapacity = errors.New("personal MCP concurrency capacity exceeded")

type personalWaiter struct {
	user    string
	ctx     context.Context
	ready   chan struct{}
	granted bool
}

// One mutex protects accounting and handoff. Waiting never reserves an execution
// slot; an ineligible user's head cannot block another user. Per-user queue caps
// also prevent one principal from filling the entire waiting room.
type personalAdmission struct {
	mu                                          sync.Mutex
	global, perUser, queueLimit, userQueueLimit int
	wait                                        time.Duration
	active                                      int
	users                                       map[string]int
	queue                                       []*personalWaiter
	rejected                                    uint64
}

type personalLoad = perftrace.AdmissionLoad

func newPersonalAdmission(c config.PersonalMCPConcurrencyConfig, protocol bool) *personalAdmission {
	global, user := c.GlobalLimit, c.UserLimit
	if protocol {
		global, user = c.ProtocolLimit, c.ProtocolUserLimit
	}
	return &personalAdmission{
		global: global, perUser: user, queueLimit: c.QueueLimit,
		userQueueLimit: c.UserQueueLimit, wait: time.Duration(c.WaitTimeoutMs) * time.Millisecond,
		users: map[string]int{},
	}
}

func newPersonalStreamAdmission(c config.PersonalMCPConcurrencyConfig) *personalAdmission {
	c.ProtocolLimit, c.ProtocolUserLimit = c.StreamLimit, c.StreamUserLimit
	return newPersonalAdmission(c, true)
}

func (a *personalAdmission) loadLocked(user string) personalLoad {
	l := personalLoad{Active: a.active, Queued: len(a.queue), UserActive: a.users[user], Rejected: a.rejected}
	for _, w := range a.queue {
		if w.user == user {
			l.UserQueued++
		}
	}
	return l
}

func (a *personalAdmission) snapshot(user string) personalLoad {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.loadLocked(user)
}

func (a *personalAdmission) grantLocked(user string) {
	a.active++
	a.users[user]++
}

func (a *personalAdmission) releaseLocked(user string) {
	a.active--
	a.users[user]--
	if a.users[user] == 0 {
		delete(a.users, user)
	}
}

func (a *personalAdmission) dispatchLocked() {
	for i := 0; i < len(a.queue); {
		w := a.queue[i]
		if w.ctx.Err() != nil {
			a.queue = slices.Delete(a.queue, i, i+1)
			continue
		}
		if a.active >= a.global {
			return
		}
		if a.users[w.user] >= a.perUser {
			i++
			continue
		}
		a.queue = slices.Delete(a.queue, i, i+1)
		a.grantLocked(w.user)
		w.granted = true
		close(w.ready)
	}
}

// acquire returns an idempotent release. Cancellation racing a grant takes the
// slot back under the same lock, so no permit or user entry can leak.
func (a *personalAdmission) acquire(ctx context.Context, user string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.dispatchLocked()
	if len(a.queue) == 0 && a.active < a.global && a.users[user] < a.perUser {
		a.grantLocked(user)
		a.mu.Unlock()
		return a.release(user), nil
	}
	l := a.loadLocked(user)
	// When other users are waiting at their quota, use otherwise idle capacity.
	if a.active < a.global && a.users[user] < a.perUser && l.UserQueued == 0 {
		a.grantLocked(user)
		a.mu.Unlock()
		return a.release(user), nil
	}
	if l.Queued >= a.queueLimit || l.UserQueued >= a.userQueueLimit {
		a.rejected++
		a.mu.Unlock()
		return nil, errPersonalCapacity
	}
	w := &personalWaiter{user: user, ctx: ctx, ready: make(chan struct{})}
	a.queue = append(a.queue, w)
	a.mu.Unlock()
	timer := time.NewTimer(a.wait)
	defer timer.Stop()
	var err error
	select {
	case <-w.ready:
		err = ctx.Err()
	case <-ctx.Done():
		err = ctx.Err()
	case <-timer.C:
		err = errPersonalCapacity
	}
	a.mu.Lock()
	if err == nil {
		a.mu.Unlock()
		return a.release(user), nil
	}
	if w.granted {
		a.releaseLocked(user)
	} else {
		for i, p := range a.queue {
			if p == w {
				a.queue = slices.Delete(a.queue, i, i+1)
				break
			}
		}
	}
	if errors.Is(err, errPersonalCapacity) {
		a.rejected++
	}
	a.dispatchLocked()
	a.mu.Unlock()
	return nil, err
}

func (a *personalAdmission) release(user string) func() {
	var once sync.Once
	return func() {
		once.Do(func() { a.mu.Lock(); defer a.mu.Unlock(); a.releaseLocked(user); a.dispatchLocked() })
	}
}
