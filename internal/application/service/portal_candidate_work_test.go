package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func candidateTestRows() []*types.RetrieveResult {
	return []*types.RetrieveResult{{RetrieverType: types.KeywordsRetrieverType, Results: []*types.IndexWithScore{
		{ID: "1", KnowledgeBaseID: "kb", KnowledgeID: "doc", ChunkID: "chunk", SourceID: "chunk", Content: "canonical private body", Score: 7},
	}}}
}

func candidateRefs(w *portalCandidateWork, key portalCandidateKey) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	if t := w.active[key]; t != nil {
		return t.refs
	}
	return 0
}

func TestPortalCandidateWorkLeaderCancellationDoesNotCancelFollower(t *testing.T) {
	w := newPortalCandidateWork()
	key := portalCandidateKey{}
	ctx, cancel := context.WithCancel(context.Background())
	start, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	load := func(ctx context.Context) ([]*types.RetrieveResult, error) {
		if calls.Add(1) == 1 {
			close(start)
		}
		select {
		case <-release:
			return candidateTestRows(), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	first := make(chan error, 1)
	go func() { _, e := w.do(ctx, key, load); first <- e }()
	<-start
	type reply struct {
		rows []*types.RetrieveResult
		err  error
	}
	second := make(chan reply, 1)
	go func() { r, e := w.do(context.Background(), key, load); second <- reply{r, e} }()
	require.Eventually(t, func() bool { return candidateRefs(w, key) == 2 }, time.Second, time.Millisecond)
	cancel()
	require.ErrorIs(t, <-first, context.Canceled)
	require.EqualValues(t, 1, calls.Load())
	close(release)
	r := <-second
	require.NoError(t, r.err)
	require.Empty(t, r.rows[0].Results[0].Content)
	require.Equal(t, portalContentDigest("canonical private body", ""), r.rows[0].Results[0].PortalCandidateDigest)
	r.rows[0].Results[0].ID = "caller mutation"
	r2, e := w.do(context.Background(), key, load)
	require.NoError(t, e)
	require.EqualValues(t, 2, calls.Load(), "finished computations must not be cached")
	require.Equal(t, "1", r2[0].Results[0].ID)
}

func TestPortalCandidateWorkAllCancelDeadlineAndClose(t *testing.T) {
	for _, kind := range []string{"last_waiter", "deadline", "shutdown"} {
		t.Run(kind, func(t *testing.T) {
			w := newPortalCandidateWork()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if kind == "deadline" {
				var c context.CancelFunc
				ctx, c = context.WithTimeout(ctx, 40*time.Millisecond)
				defer c()
			}
			started, exited := make(chan struct{}), make(chan struct{})
			done := make(chan error, 1)
			go func() {
				_, e := w.do(ctx, portalCandidateKey{}, func(work context.Context) ([]*types.RetrieveResult, error) {
					close(started)
					<-work.Done()
					close(exited)
					return nil, work.Err()
				})
				done <- e
			}()
			<-started
			if kind == "shutdown" {
				w.mu.Lock()
				w.closed = true
				for task := range w.live {
					task.cancel()
				}
				w.mu.Unlock()
			} else if kind == "last_waiter" {
				cancel()
			}
			require.Error(t, <-done)
			select {
			case <-exited:
			case <-time.After(time.Second):
				t.Fatal("driver cancellation was not propagated")
			}
			require.Eventually(t, func() bool { w.mu.Lock(); defer w.mu.Unlock(); return len(w.live) == 0 && len(w.active) == 0 }, time.Second, time.Millisecond)
		})
	}
}

func TestPortalCandidateWorkShortLeaderDeadlineDoesNotExpireFollower(t *testing.T) {
	w := newPortalCandidateWork()
	key := portalCandidateKey{}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	load := func(work context.Context) ([]*types.RetrieveResult, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return candidateTestRows(), nil
		case <-work.Done():
			return nil, work.Err()
		}
	}
	first, second := make(chan error, 1), make(chan error, 1)
	go func() { _, e := w.do(ctx, key, load); first <- e }()
	<-started
	go func() { _, e := w.do(context.Background(), key, load); second <- e }()
	require.Eventually(t, func() bool { return candidateRefs(w, key) == 2 }, time.Second, time.Millisecond)
	require.ErrorIs(t, <-first, context.DeadlineExceeded)
	close(release)
	require.NoError(t, <-second)
	require.EqualValues(t, 1, calls.Load())
}

func TestPortalCandidateWorkIdentityIsolationAndBounds(t *testing.T) {
	w := newPortalCandidateWork()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < portalCandidateWorkSlots; i++ {
		key := portalCandidateKey{}
		key.scope[0] = byte(i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = w.do(ctx, key, func(work context.Context) ([]*types.RetrieveResult, error) {
				calls.Add(1)
				<-work.Done()
				return nil, work.Err()
			})
		}()
	}
	require.Eventually(t, func() bool { return calls.Load() == portalCandidateWorkSlots }, time.Second, time.Millisecond)
	key := portalCandidateKey{}
	key.scope[1] = 1
	_, e := w.do(ctx, key, func(context.Context) ([]*types.RetrieveResult, error) {
		t.Error("capacity must fall back without starting another pooled task")
		return nil, nil
	})
	require.ErrorIs(t, e, errPortalCandidateCapacity)
	cancel()
	wg.Wait()
	rows := candidateTestRows()
	rows[0].Results[0].ID = strings.Repeat("x", portalCandidateReplyBytes)
	_, e = portalCandidateProjection(rows)
	require.ErrorIs(t, e, errPortalCandidateCapacity)
	rows = candidateTestRows()
	for len(rows[0].Results) < 801 {
		rows[0].Results = append(rows[0].Results, rows[0].Results[0])
	}
	_, e = portalCandidateProjection(rows)
	require.ErrorIs(t, e, errPortalCandidateCapacity)
}

func TestPortalCandidateWorkPanicCannotLeakTask(t *testing.T) {
	w := newPortalCandidateWork()
	_, e := w.do(context.Background(), portalCandidateKey{}, func(context.Context) ([]*types.RetrieveResult, error) { panic("driver panic must remain private") })
	require.True(t, errors.Is(e, errPortalCandidatePanic))
	require.Eventually(t, func() bool { w.mu.Lock(); defer w.mu.Unlock(); return len(w.active) == 0 && len(w.live) == 0 }, time.Second, time.Millisecond)
	s := &PortalMCPKnowledge{}
	require.NoError(t, s.ClosePortalCandidateWork())
	_, e = s.candidateWork.do(context.Background(), portalCandidateKey{}, func(context.Context) ([]*types.RetrieveResult, error) {
		t.Fatal("closed service started work")
		return nil, nil
	})
	require.ErrorIs(t, e, context.Canceled)
}
