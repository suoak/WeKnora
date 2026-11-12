package mcpserver

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPersonalAdmissionConcurrentLoads(t *testing.T) {
	for _, n := range []int{3, 5, 10, 20} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			c, _ := (config.PersonalMCPConcurrencyConfig{}).Resolve()
			a := newPersonalAdmission(c, false)
			var successes, peak, active atomic.Int32
			var wg sync.WaitGroup
			start := make(chan struct{})
			for i := 0; i < n; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					release, err := a.acquire(context.Background(), "owner")
					if err != nil {
						require.ErrorIs(t, err, errPersonalCapacity)
						return
					}
					v := active.Add(1)
					for p := peak.Load(); v > p; p = peak.Load() {
						if peak.CompareAndSwap(p, v) {
							break
						}
					}
					time.Sleep(10 * time.Millisecond)
					active.Add(-1)
					successes.Add(1)
					release()
					release()
				}()
			}
			close(start)
			wg.Wait()
			if n <= 10 {
				require.EqualValues(t, n, successes.Load())
			}
			require.GreaterOrEqual(t, successes.Load(), int32(min(4, n)))
			require.LessOrEqual(t, peak.Load(), int32(4))
			require.Zero(t, a.snapshot("owner").Active)
			require.Zero(t, a.snapshot("owner").Queued)
			require.Empty(t, a.users)
		})
	}
}

func TestPersonalAdmissionFairnessAndQueueBounds(t *testing.T) {
	c, _ := (config.PersonalMCPConcurrencyConfig{}).Resolve()
	c.GlobalLimit = 2
	c.UserLimit = 1
	c.QueueLimit = 2
	c.UserQueueLimit = 1
	a := newPersonalAdmission(c, false)
	first, err := a.acquire(context.Background(), "A")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	queued := make(chan error, 1)
	go func() {
		release, err := a.acquire(ctx, "A")
		if err == nil {
			release()
		}
		queued <- err
	}()
	require.Eventually(t, func() bool { return a.snapshot("A").Queued == 1 }, time.Second, time.Millisecond)
	_, err = a.acquire(context.Background(), "A")
	require.ErrorIs(t, err, errPersonalCapacity)
	// A's queued request cannot reserve the spare global slot or block B.
	second, err := a.acquire(context.Background(), "B")
	require.NoError(t, err)
	require.Equal(t, 2, a.snapshot("B").Active)
	cancel()
	require.ErrorIs(t, <-queued, context.Canceled)
	first()
	second()
	require.Empty(t, a.users)
	require.Empty(t, a.queue)
}

func TestPersonalAdmissionCancellationGrantRace(t *testing.T) {
	c, _ := (config.PersonalMCPConcurrencyConfig{}).Resolve()
	c.GlobalLimit = 1
	c.UserLimit = 1
	for i := 0; i < 100; i++ {
		a := newPersonalAdmission(c, false)
		first, err := a.acquire(context.Background(), "A")
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			release, err := a.acquire(ctx, "A")
			if err == nil {
				release()
			}
		}()
		go cancel()
		first()
		<-done
		require.Zero(t, a.snapshot("A").Active)
		require.Empty(t, a.queue)
		require.Empty(t, a.users)
	}
}

func TestPersonalAdmissionTimeoutAndOverload(t *testing.T) {
	c, _ := (config.PersonalMCPConcurrencyConfig{}).Resolve()
	c.GlobalLimit = 1
	c.UserLimit = 1
	a := newPersonalAdmission(c, false)
	a.wait = 10 * time.Millisecond
	first, err := a.acquire(context.Background(), "A")
	require.NoError(t, err)
	defer first()
	_, err = a.acquire(context.Background(), "B")
	require.ErrorIs(t, err, errPersonalCapacity)
	require.Zero(t, a.snapshot("B").Queued)
	require.EqualValues(t, 1, a.snapshot("B").Rejected)
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	_, err = a.acquire(ctx, "C")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Zero(t, a.snapshot("C").Queued)
}

func TestPersonalAdmissionFourIndependentProtocolRequests(t *testing.T) {
	c, err := (config.PersonalMCPConcurrencyConfig{}).Resolve()
	require.NoError(t, err)
	a := newPersonalAdmission(c, true)
	for i := 0; i < 4; i++ {
		release, err := a.acquire(context.Background(), "owner")
		require.NoError(t, err)
		defer release()
	}
	require.Equal(t, 4, a.snapshot("owner").Active)
}

func TestPersonalAdmissionHTTPIdentityProtocolIsolationAndUsage(t *testing.T) {
	s := NewPersonalServer(true, &service.PortalMCPKnowledge{})
	s.admission.wait = time.Millisecond
	for i := 0; i < s.admission.perUser; i++ {
		release, err := s.admission.acquire(context.Background(), "owner")
		require.NoError(t, err)
		defer release()
	}
	sink := &captureUsage{}
	s.SetUsageRecorder(sink)
	for _, isolated := range []bool{false, true} {
		path := personalPath
		h := s.Handler()
		if isolated {
			path = isolatedPersonalPath
			h = s.IsolatedHandler()
		}
		for _, method := range []string{"initialize", "tools/list", "tools/call"} {
			body := `{"jsonrpc":"2.0","id":1,"method":"` + method + `","params":{}}`
			if method == "tools/call" {
				body = `{"jsonrpc":"2.0","id":1,"method":"tools/call",` +
					`"params":{"name":"search_knowledge","arguments":{"query":"safe"}}}`
			}
			r := httptest.NewRequest("POST", path, strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-User-ID", "spoofed")
			principal := types.Principal{Type: types.PrincipalWebUser, ID: "owner"}
			r = r.WithContext(types.WithPrincipal(r.Context(), principal))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if method == "tools/call" {
				require.Equal(t, 429, w.Code)
				require.Equal(t, "1", w.Header().Get("Retry-After"))
				require.Equal(t, "concurrency_limited", w.Header().Get("X-KnowHub-Error-Code"))
			} else {
				require.Equal(t, 403, w.Code, "protocol bypasses full tool pool but still fails authorization")
			}
		}
	}
	require.Len(t, sink.reports, 2)
	for _, report := range sink.reports {
		require.Equal(t, "concurrency_limited", report.ErrorCode)
		require.False(t, report.Success)
	}
	require.NotEqual(t, sink.reports[0].EventKey, sink.reports[1].EventKey)
	// A caller-supplied identity header cannot establish an authenticated principal.
	r := httptest.NewRequest("POST", personalPath, strings.NewReader(`{"method":"tools/list"}`))
	r.Header.Set("X-User-ID", "owner")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	require.Equal(t, 401, w.Code)
}
