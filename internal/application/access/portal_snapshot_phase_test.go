package access

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPortalSnapshotPhaseMergesReadsWithoutSharingMutableFacts(t *testing.T) {
	ctx := WithPortalSnapshotReads(context.Background())
	store := new(int)
	var calls atomic.Int32
	load := func() (*types.EnterpriseReadSnapshot, error) {
		calls.Add(1)
		return &types.EnterpriseReadSnapshot{UserID: "A", Documents: []types.EnterpriseDocumentState{{ID: "doc"}}}, nil
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, err := ReadPortalSnapshot(ctx, store, "A", 1, "kb", false, true, load)
			require.NoError(t, err)
			value.Documents[0].ID = "caller mutation"
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, calls.Load())
	value, err := ReadPortalSnapshot(ctx, store, "A", 1, "kb", false, true, load)
	require.NoError(t, err)
	require.Equal(t, "doc", value.Documents[0].ID)
	for _, fresh := range []context.Context{WithoutPortalSnapshotReads(ctx), WithPortalSnapshotReads(context.Background())} {
		_, err = ReadPortalSnapshot(fresh, store, "A", 1, "kb", false, true, load)
		require.NoError(t, err)
	}
	require.EqualValues(t, 3, calls.Load(), "output fences and new requests must load live facts")
	_, err = ReadPortalSnapshot(ctx, store, "B", 1, "kb", false, true, load)
	require.NoError(t, err)
	_, err = ReadPortalSnapshot(ctx, store, "A", 2, "kb", false, true, load)
	require.NoError(t, err)
	require.EqualValues(t, 5, calls.Load())
}

func TestPortalSnapshotPhaseDoesNotCacheFailures(t *testing.T) {
	ctx := WithPortalSnapshotReads(context.Background())
	store := new(int)
	for range 2 {
		_, err := ReadPortalSnapshot(ctx, store, "A", 1, "kb", false, true,
			func() (*types.EnterpriseReadSnapshot, error) { return nil, errors.New("unavailable") })
		require.Error(t, err)
	}
	_, err := ReadPortalSnapshot(ctx, store, "A", 1, "kb", false, true,
		func() (*types.EnterpriseReadSnapshot, error) { return &types.EnterpriseReadSnapshot{}, nil })
	require.NoError(t, err)
}
