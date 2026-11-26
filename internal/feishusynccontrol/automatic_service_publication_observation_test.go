package feishusynccontrol

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Pause the REAL Publish after it acquires its normal source execution fence,
// rather than replacing publication with a manual success row or fake receipt.
func publishWithCanaryObservation(
	work context.Context, t *testing.T, s *automaticService, db *gorm.DB, store *repository.FeishuLegacyStore,
	source *types.DataSource, attempt *types.FeishuLegacyAttempt,
) *types.FeishuLegacyReceipt {
	t.Helper()
	locked, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unlock := func() { once.Do(func() { close(release) }) }
	var armed atomic.Bool
	armed.Store(true)
	callback := "test:real_publish_source_fence_" + uuid.NewString()
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		statement := tx.Statement.SQL.String()
		if strings.Contains(statement, "data_sources") && strings.Contains(statement, "FOR UPDATE") &&
			armed.CompareAndSwap(true, false) {
			close(locked)
			<-release
		}
	}))
	pubCtx, cancel := context.WithCancel(work)
	var receipt *types.FeishuLegacyReceipt
	var publicationErr error
	defer func() {
		unlock()
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("actual publication did not stop; retain callback instead of mutating it concurrently")
			return
		}
		require.NoError(t, db.Callback().Query().Remove(callback))
	}()
	go func() {
		receipt, publicationErr = store.Publish(pubCtx, source, attempt)
		close(done)
	}()
	select {
	case <-locked:
	case <-done:
		t.Fatalf("actual Publish did not reach its source fence: %v", publicationErr)
	case <-time.After(2 * time.Second):
		t.Fatal("actual Publish did not acquire its source fence in bounded time")
	}
	previousPoll := s.lastCanaryRead
	limit := time.Now().Add(2 * time.Second)
	for !s.lastCanaryRead.After(previousPoll) {
		require.True(t, time.Now().Before(limit), "must observe an actual Canary poll without changing its clock")
		probe, stop := context.WithTimeout(context.Background(), 200*time.Millisecond)
		err := s.c.Step(probe)
		stop()
		require.NoError(t, err, "controller must not wait behind the real publication it is observing")
		state, _, err := s.current(context.Background())
		require.NoError(t, err)
		require.Equal(t, "canary", state.Phase)
		require.Empty(t, state.CanaryProofSHA256, "an in-flight publication is never signed off as success")
		time.Sleep(100 * time.Millisecond)
	}
	unlock()
	finish := time.NewTimer(3 * time.Second)
	defer finish.Stop()
	for {
		select {
		case <-done:
			require.NoError(t, publicationErr)
			require.NotNil(t, receipt)
			return receipt
		case <-finish.C:
			t.Fatal("actual publication did not finish after the owned test barrier was released")
			return nil
		default:
			probe, stop := context.WithTimeout(context.Background(), 200*time.Millisecond)
			err := s.c.Step(probe)
			stop()
			require.NoError(t, err, "maintain the real controller lease until publication actually commits")
			time.Sleep(50 * time.Millisecond)
		}
	}
}
