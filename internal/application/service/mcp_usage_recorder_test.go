package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type retryUsageRepo struct {
	*usageRepoStub
	failure bool
}

func (r *retryUsageRepo) RecordMCPUsage(
	ctx context.Context,
	event *types.MCPUsageEvent,
	links []types.UsageResourceLink,
) (bool, error) {
	if r.failure {
		return false, errors.New("database unavailable")
	}
	return r.usageRepoStub.RecordMCPUsage(ctx, event, links)
}

func TestMCPUsageSpoolSurvivesCanceledRequestAndRestart(t *testing.T) {
	repo := &retryUsageRepo{usageRepoStub: &usageRepoStub{}}
	dir := t.TempDir()
	recorder := newMCPUsageRecorder(repo, dir)
	ctx, cancel := context.WithCancel(
		types.WithTenantAPIKeyScope(
			types.WithCaller(
				types.WithPrincipal(
					context.Background(),
					types.Principal{Type: types.PrincipalWebUser, ID: "employee"},
				),
				types.Caller{TenantID: 100},
			),
			types.TenantAPIKeyScope{KeyID: 42},
		),
	)
	cancel()
	stamp := time.Now().Add(-time.Minute)
	report := types.MCPUsageReport{
		EventKey:   "stable-invocation",
		ToolName:   "read_document",
		EntryPoint: "go_personal",
		Success:    true,
		OccurredAt: stamp,
	}
	require.NoError(t, recorder.Record(ctx, report))
	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, files, 1)
	bytes, err := os.ReadFile(filepath.Join(dir, files[0].Name()))
	require.NoError(t, err)
	require.NotContains(t, string(bytes), "Authorization")
	// Constructing a new collector emulates a process restart over the volume.
	restarted := newMCPUsageRecorder(repo, dir)
	restarted.drain()
	require.Equal(t, 1, repo.mcpHit)
	require.Equal(t, "employee", repo.mcp.PrincipalID)
	require.Equal(t, uint64(42), *repo.mcp.APIKeyID)
	require.Equal(t, uint64(100), *repo.mcp.CallerTenantID)
	require.True(t, stamp.Equal(repo.mcp.OccurredAt))
	require.Equal(t, "stable-invocation", repo.mcp.EventKey)
	files, err = os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, files)
}

func TestMCPUsageSpoolBoundedRetriesAndDeadLetter(t *testing.T) {
	repo := &retryUsageRepo{usageRepoStub: &usageRepoStub{}, failure: true}
	r := newMCPUsageRecorder(repo, t.TempDir())
	require.NoError(
		t,
		r.Record(context.Background(), types.MCPUsageReport{EventKey: "retry", ToolName: "search_knowledge"}),
	)
	for i := 0; i < 5; i++ {
		r.drain()
	}
	files, err := filepath.Glob(filepath.Join(r.dir, "*.dead"))
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Zero(t, repo.mcpHit)
	r.count = 10000
	require.Error(
		t,
		r.Record(context.Background(), types.MCPUsageReport{EventKey: "overflow", ToolName: "search_knowledge"}),
	)
}

func TestMCPUsageInterruptedAdmissionAndConcurrentFinalization(t *testing.T) {
	repo := &retryUsageRepo{usageRepoStub: &usageRepoStub{}}
	dir := t.TempDir()
	r := newMCPUsageRecorder(repo, dir)
	require.NoError(
		t,
		r.Begin(context.Background(), types.MCPUsageReport{EventKey: "unfinished", ToolName: "search_knowledge"}),
	)
	newMCPUsageRecorder(repo, dir).drain()
	require.Equal(t, "interrupted", repo.mcp.ErrorCode)
	require.False(t, repo.mcp.Success)
	r = newMCPUsageRecorder(repo, dir)
	started := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			report := types.MCPUsageReport{
				EventKey: types.UsageEventKey("concurrent", time.Unix(int64(i), 0).String()),
				ToolName: "search_knowledge",
				Success:  true,
			}
			if err := r.Begin(context.Background(), report); err != nil {
				t.Error(err)
			}
			if err := r.Record(context.Background(), report); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	t.Logf("32 concurrent durable admissions + finalizations: %s", time.Since(started))
	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, files, 32)
	r.drain()
	require.Equal(t, 33, repo.mcpHit)
}
