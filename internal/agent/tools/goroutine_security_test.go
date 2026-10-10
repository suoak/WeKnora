package tools

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
)

func TestRecoveredPanicRedactsValueLabelAndStack(t *testing.T) {
	var output bytes.Buffer
	logger.SetOutput(&output)
	defer logger.SetOutput(os.Stderr)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer RecoverGoroutine(context.Background(), "SYNTHETIC_LABEL_SECRET", nil)
		panic("SYNTHETIC_TOKEN_SQL_CANARY")
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("panic recovery did not complete")
	}
	text := output.String()
	if !strings.Contains(text, "tool goroutine failed (panic)") {
		t.Fatal("missing controlled failure classification")
	}
	for _, forbidden := range []string{"SYNTHETIC_LABEL_SECRET", "SYNTHETIC_TOKEN_SQL_CANARY", "goroutine_security_test.go", "runtime/debug"} {
		if strings.Contains(text, forbidden) {
			t.Fatal("panic output disclosed sensitive context (withheld)")
		}
	}
}

func TestRecoveredConcurrentCancellationRunsCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		GoRecovered(ctx, func() {
			defer wg.Done()
			<-start
			<-ctx.Done()
			panic("synthetic cancelled adapter")
		})
	}
	cancel()
	close(start)
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation cleanup deadlocked")
	}
	if ctx.Err() != context.Canceled {
		t.Fatal("context cancellation changed")
	}
}
