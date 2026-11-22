package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/perftrace"
	"github.com/Tencent/WeKnora/internal/types"
)

const portalCandidateWorkSlots = 32
const portalCandidateReplyBytes = 1 << 20

var errPortalCandidateCapacity = errors.New("candidate computation capacity unavailable")
var errPortalCandidateCalculation = errors.New("candidate computation failed")
var errPortalCandidatePanic = errors.New("candidate driver panic")

type portalCandidateKey struct {
	engine                        access.DocumentRetriever
	scope, request, batch, params [32]byte
}

type portalCandidateTask struct {
	done             chan struct{}
	cancel           context.CancelFunc
	rows             []*types.RetrieveResult
	err              error
	refs             int
	finished, exited bool
}

// Only running computations are discoverable. Live reservations include finished
// replies until their admitted waiters release them AND the driver exits.
// 32 x 1 MiB bounds retained candidate replies; bodies are never retained.
type portalCandidateWork struct {
	mu     sync.Mutex
	active map[portalCandidateKey]*portalCandidateTask
	live   map[*portalCandidateTask]bool
	closed bool
	wg     sync.WaitGroup
}

func newPortalCandidateWork() *portalCandidateWork {
	return &portalCandidateWork{active: make(map[portalCandidateKey]*portalCandidateTask), live: make(map[*portalCandidateTask]bool)}
}

func (w *portalCandidateWork) release(t *portalCandidateTask) {
	w.mu.Lock()
	defer w.mu.Unlock()
	t.refs--
	if t.refs == 0 {
		t.cancel()
		if t.exited {
			delete(w.live, t)
		}
	}
}

func (w *portalCandidateWork) finish(key portalCandidateKey, t *portalCandidateTask, rows []*types.RetrieveResult, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if t.finished {
		return
	}
	t.finished = true
	t.rows, t.err = rows, err
	if w.active[key] == t {
		delete(w.active, key)
	}
	close(t.done)
}

func (w *portalCandidateWork) do(ctx context.Context, key portalCandidateKey,
	load func(context.Context) ([]*types.RetrieveResult, error),
) ([]*types.RetrieveResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil, context.Canceled
	}
	t := w.active[key]
	if t == nil {
		if len(w.live) >= portalCandidateWorkSlots {
			w.mu.Unlock()
			return nil, errPortalCandidateCapacity
		}
		deadline := time.Now().Add(portalSearchModeTimeout)
		// The worker has its own bounded active lifetime. A short first caller
		// must not impose its deadline on an independently live follower. Each
		// waiter still obeys its own mode/HTTP deadline; zero waiters cancels SQL.
		work, cancel := context.WithDeadline(context.WithoutCancel(ctx), deadline)
		t = &portalCandidateTask{done: make(chan struct{}), cancel: cancel, refs: 1}
		w.active[key], w.live[t] = t, true
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			defer func() {
				if recover() != nil {
					w.finish(key, t, nil, errPortalCandidatePanic)
				}
				cancel()
				w.mu.Lock()
				t.exited = true
				if t.refs == 0 {
					delete(w.live, t)
				}
				w.mu.Unlock()
			}()
			stop := context.AfterFunc(work, func() { w.finish(key, t, nil, work.Err()) })
			defer stop()
			done := perftrace.Measure(work, perftrace.CandidateCompute)
			var rows []*types.RetrieveResult
			err := work.Err()
			if err == nil {
				rows, err = load(work)
			}
			done()
			if err == nil {
				rows, err = portalCandidateProjection(rows)
			}
			if err != nil && !portalBatchAuthorizationError(err) && !errors.Is(err, context.Canceled) &&
				!errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, errPortalCandidateCapacity) {
				err = errPortalCandidateCalculation
			}
			w.finish(key, t, rows, err)
		}()
	} else {
		t.refs++
		perftrace.Measure(ctx, perftrace.CandidateJoin)()
	}
	w.mu.Unlock()
	defer w.release(t)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-t.done:
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return clonePortalBatchResults(t.rows), t.err
	}
}

func portalCandidateProjection(rows []*types.RetrieveResult) ([]*types.RetrieveResult, error) {
	if len(rows) > 2 {
		return nil, errPortalCandidateCapacity
	}
	var result []*types.RetrieveResult
	bytes, count := 0, 0
	for _, row := range rows {
		bytes += 128
		if row == nil {
			return nil, errPortalCandidateCalculation
		}
		if row.Error != nil {
			return nil, row.Error
		}
		copyRow := *row
		copyRow.Results = nil
		for _, c := range row.Results {
			if c == nil {
				return nil, access.ErrForbidden
			}
			count++
			// Conservative allocation charge includes struct/slice/hash overhead,
			// copied string allocation classes and result-group storage.
			bytes += 768 + 2*(len(c.ID)+len(c.SourceID)+len(c.ChunkID)+len(c.KnowledgeID)+len(c.KnowledgeBaseID)+len(c.TagID))
			if count > 800 || bytes > portalCandidateReplyBytes {
				return nil, errPortalCandidateCapacity
			}
			v := *c
			sum := sha256.Sum256([]byte(c.Content))
			v.Content = ""
			v.PortalCandidateDigest = hex.EncodeToString(sum[:])
			v.ID = strings.Clone(c.ID)
			v.SourceID = strings.Clone(c.SourceID)
			v.ChunkID = strings.Clone(c.ChunkID)
			v.KnowledgeID = strings.Clone(c.KnowledgeID)
			v.KnowledgeBaseID = strings.Clone(c.KnowledgeBaseID)
			v.TagID = strings.Clone(c.TagID)
			copyRow.Results = append(copyRow.Results, &v)
		}
		result = append(result, &copyRow)
	}
	return result, nil
}

// ClosePortalCandidateWork is registered with application resource cleanup.
// Cancellation stops accepting work immediately and is propagated to SQL.
func (s *PortalMCPKnowledge) ClosePortalCandidateWork() error {
	if s == nil {
		return nil
	}
	s.candidateOnce.Do(func() { s.candidateWork = newPortalCandidateWork() })
	w := s.candidateWork
	w.mu.Lock()
	w.closed = true
	for t := range w.live {
		t.cancel()
	}
	w.mu.Unlock()
	done := make(chan struct{})
	go func() { w.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
		return context.DeadlineExceeded
	}
}

func portalContentDigest(text, digest string) string {
	if digest != "" {
		return digest
	}
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
