package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestFeishuPDFTimeoutReleasesExecutionButBoundsNativeWork(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	done := make(chan feishuPDFReadResult, 1)
	go func() {
		result, err := readBoundedFeishuPDF(context.Background(), 30*time.Millisecond, time.Hour,
			func(context.Context) error { return nil },
			func(context.Context) (*types.ReadResult, error) {
				close(started)
				<-release // Deliberately ignores context, like the native converter.
				return &types.ReadResult{MarkdownContent: "late unpublished content"}, nil
			})
		done <- feishuPDFReadResult{result: result, err: err}
	}()
	<-started
	select {
	case out := <-done:
		require.Nil(t, out.result)
		var refusal *feishuStagingRefusal
		require.ErrorAs(t, out.err, &refusal)
		require.Equal(t, "parser_timeout", refusal.reason)
	case <-time.After(time.Second):
		t.Fatal("non-cooperative parsing pinned the source execution")
	}
	var called atomic.Bool
	result, err := readBoundedFeishuPDF(context.Background(), time.Second, time.Hour,
		func(context.Context) error { return nil }, func(context.Context) (*types.ReadResult, error) {
			called.Store(true)
			return nil, nil
		})
	require.Nil(t, result)
	require.False(t, called.Load(), "a timed-out native call must keep its bounded slot")
	var refusal *feishuStagingRefusal
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "parser_busy", refusal.reason)
	s := &knowledgeService{}
	result, err = s.readFeishuCandidate(context.Background(), feishuPDFTestReader(
		func(context.Context, *types.ReadRequest) (*types.ReadResult, error) {
			return &types.ReadResult{MarkdownContent: "XLSX still makes progress"}, nil
		}), &types.ReadRequest{FileType: "xlsx"}, func(context.Context) error {
		t.Fatal("ordinary XLSX parsing must retain its existing lease path")
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, "XLSX still makes progress", result.MarkdownContent)
}

type feishuPDFTestReader func(context.Context, *types.ReadRequest) (*types.ReadResult, error)

func (f feishuPDFTestReader) Read(ctx context.Context, request *types.ReadRequest) (*types.ReadResult, error) {
	return f(ctx, request)
}

func TestFeishuPDFCancellationAndLostLeaseDiscardLateOutput(t *testing.T) {
	// Wait for the previous test's deliberately abandoned converter to return.
	require.Eventually(t, func() bool { return len(feishuPDFReaderSlot) == 0 }, time.Second, time.Millisecond)
	for _, lostLease := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		release, started := make(chan struct{}), make(chan struct{})
		var renewals atomic.Int32
		lost := errors.New("attempt ownership lost")
		done := make(chan feishuPDFReadResult, 1)
		go func() {
			result, err := readBoundedFeishuPDF(ctx, time.Second, 5*time.Millisecond,
				func(context.Context) error {
					if renewals.Add(1) > 1 && lostLease {
						return lost
					}
					return nil
				}, func(context.Context) (*types.ReadResult, error) {
					close(started)
					<-release
					return &types.ReadResult{MarkdownContent: "late unpublished content"}, nil
				})
			done <- feishuPDFReadResult{result: result, err: err}
		}()
		<-started
		if !lostLease {
			cancel()
		}
		select {
		case out := <-done:
			require.Nil(t, out.result)
			if lostLease {
				require.ErrorIs(t, out.err, lost)
			} else {
				require.ErrorIs(t, out.err, context.Canceled)
			}
		case <-time.After(time.Second):
			t.Error("canceled parsing pinned the source execution")
		}
		cancel()
		close(release)
		require.Eventually(t, func() bool { return len(feishuPDFReaderSlot) == 0 }, time.Second, time.Millisecond)
	}
}

func TestFeishuPDFRenewsDuringSuccessfulParseAndContainsPanic(t *testing.T) {
	require.Eventually(t, func() bool { return len(feishuPDFReaderSlot) == 0 }, time.Second, time.Millisecond)
	var renewals atomic.Int32
	ready := make(chan struct{})
	result, err := readBoundedFeishuPDF(context.Background(), time.Second, time.Millisecond,
		func(context.Context) error {
			if renewals.Add(1) == 3 {
				close(ready)
			}
			return nil
		}, func(context.Context) (*types.ReadResult, error) {
			<-ready
			return &types.ReadResult{MarkdownContent: "verified"}, nil
		})
	require.NoError(t, err)
	require.Equal(t, "verified", result.MarkdownContent)
	require.GreaterOrEqual(t, renewals.Load(), int32(3))
	result, err = readBoundedFeishuPDF(context.Background(), time.Second, time.Hour,
		func(context.Context) error { return nil }, func(context.Context) (*types.ReadResult, error) {
			panic("private parser internals")
		})
	require.Nil(t, result)
	require.EqualError(t, err, "Feishu PDF parser failed")
	result, err = readBoundedFeishuPDF(context.Background(), time.Second, time.Hour,
		func(context.Context) error { return nil }, func(context.Context) (*types.ReadResult, error) {
			return &types.ReadResult{MarkdownContent: "next"}, nil
		})
	require.NoError(t, err)
	require.Equal(t, "next", result.MarkdownContent)
}
