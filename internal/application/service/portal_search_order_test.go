package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPortalGlobalSearchWorkHintsStayWithinAuthorizedDocuments(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	require.NoError(t, f.DB.Exec("UPDATE knowledges SET storage_size=?", int64(1)<<50).Error)
	for id, size := range map[string]int64{"public-doc": 200, "public-two-doc": 300, "private-home-doc": -1} {
		require.NoError(t, f.DB.Exec("UPDATE knowledges SET storage_size=? WHERE id=?", size, id).Error)
	}
	s := publishedServiceM7(t, f)
	ctx := fixture.Key("A", 1, 1, true)
	targets, err := s.Resolve(ctx, nil)
	require.NoError(t, err)
	require.Len(t, targets, 3)
	hints := f.Store.PortalSearchWorkHints(ctx, targets)
	require.Len(t, hints, len(targets))
	expected := map[string]int64{"public-kb": 200, "public-two": 300, "private-home": 0}
	for i, target := range targets {
		require.Equal(t, expected[target.Descriptor().ID], hints[i], "ungranted documents must not affect hints")
	}
	order := portalSearchOrder(ctx, f.Store, targets)
	require.Equal(t, "public-two", targets[order[0]].Descriptor().ID)
	require.Equal(t, "public-kb", targets[order[1]].Descriptor().ID)
	require.Equal(t, "private-home", targets[order[2]].Descriptor().ID)
	// A concurrent ownership change cannot borrow another tenant's work hint.
	require.NoError(t, f.DB.Exec("UPDATE knowledges SET tenant_id=1 WHERE id='public-doc'").Error)
	hints = f.Store.PortalSearchWorkHints(ctx, targets)
	for i, target := range targets {
		if target.Descriptor().ID == "public-kb" {
			require.Zero(t, hints[i])
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	require.Nil(t, f.Store.PortalSearchWorkHints(cancelled, targets))
	require.Equal(t, []int{0, 1, 2}, portalSearchOrder(cancelled, f.Store, targets))
}

func TestPortalGlobalSearchStartsHeavyLateTargetInFirstWorkerBatch(t *testing.T) {
	f, s, base, count := globalPortalSearchFixture(t, 10)
	require.NoError(t, f.DB.Exec(`INSERT INTO knowledges
 (id,tenant_id,knowledge_base_id,type,title,source,file_name,parse_status,enable_status,channel,storage_size)
 SELECT 'large-late',tenant_id,'parallel-09',type,'synthetic large late',source,file_name,
 parse_status,enable_status,channel,?
 FROM knowledges WHERE id='public-doc'`, int64(1)<<40).Error)
	targets, err := s.Resolve(base, nil)
	require.NoError(t, err)
	position := -1
	for i, target := range targets {
		if target.Descriptor().ID == "parallel-09" {
			position = i
		}
	}
	require.GreaterOrEqual(t, position, portalSearchTargetConcurrency)
	ctx, cancel := context.WithTimeout(base, 20*time.Second)
	defer cancel()
	started := make(chan string, count*2)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	s.Engine = func(_ context.Context, d access.PortalKBDescriptor) (access.DocumentRetriever, error) {
		return portalV1Driver{f: func(ctx context.Context, _ types.RetrieveParams) ([]*types.RetrieveResult, error) {
			started <- d.ID
			select {
			case <-release:
				return nil, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}}, nil
	}
	reply := make(chan globalPortalSearchReply, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		result, err := s.Search(ctx, nil, "synthetic", "hybrid", 1)
		reply <- globalPortalSearchReply{result, err}
	}()
	defer func() { cancel(); unblock(); <-finished }()
	firstBatch := make([]string, 0, portalSearchTargetConcurrency)
	for range portalSearchTargetConcurrency {
		select {
		case id := <-started:
			firstBatch = append(firstBatch, id)
		case <-ctx.Done():
			t.Fatal("first worker batch did not start")
		}
	}
	require.Contains(t, firstBatch, "parallel-09")
	unblock()
	got := <-reply
	require.NoError(t, got.err)
	require.Equal(t, "complete", got.result.Status)
	require.Equal(t, count, got.result.Searched)
	require.Zero(t, got.result.Failed)
}
