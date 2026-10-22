package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// Both legacy connectors use this engine. A failed durable checkpoint must
// stop before a later resource, deletion or fetch can run under a lost fence.
type checkpointFailureOps struct{ lists, fetches int }

func (o *checkpointFailureOps) List(context.Context, *Client, string) ([]string, error, error) {
	o.lists++
	return []string{"document"}, nil, nil
}
func (*checkpointFailureOps) Token(n string) string  { return n }
func (*checkpointFailureOps) Title(n string) string  { return n }
func (*checkpointFailureOps) ObjType(string) string  { return "docx" }
func (*checkpointFailureOps) EditTime(string) string { return "new" }
func (o *checkpointFailureOps) Fetch(context.Context, *Client, string, string, bool) ([]*types.FetchedItem, error) {
	o.fetches++
	return []*types.FetchedItem{{ExternalID: "document", Content: []byte("body")}}, nil
}
func (*checkpointFailureOps) ListFailureItems(string, error) []types.FetchedItem { return nil }
func (*checkpointFailureOps) ResourceNoun() string                               { return "documents" }
func (*checkpointFailureOps) EmptyResourceIDsError() string                      { return "empty" }
func (*checkpointFailureOps) LogTag() string                                     { return "[synthetic]" }
func (*checkpointFailureOps) DecodeCursorTimes(map[string]interface{}) map[string]map[string]string {
	return nil
}
func (*checkpointFailureOps) EncodeCursor(map[string]map[string]string, time.Time) *types.SyncCursor {
	return &types.SyncCursor{}
}

type failedCheckpointHandler struct {
	err     error
	emitted int
}

func (h *failedCheckpointHandler) Emit(context.Context, types.FetchedItem) error {
	h.emitted++
	return nil
}
func (h *failedCheckpointHandler) Checkpoint(context.Context, *types.SyncCursor) error { return h.err }

func TestIncrementalCheckpointFailureStopsSharedEngine(t *testing.T) {
	before := FeishuStreamCheckpointInterval
	FeishuStreamCheckpointInterval = 1
	t.Cleanup(func() { FeishuStreamCheckpointInterval = before })
	for _, cause := range []error{errors.New("database commit unavailable"), types.ErrStaleSyncExecution, context.Canceled} {
		t.Run(cause.Error(), func(t *testing.T) {
			ops := &checkpointFailureOps{}
			h := &failedCheckpointHandler{err: cause}
			client := &Client{}
			next, err := runSync(context.Background(), client, &types.DataSourceConfig{}, []string{"first", "must-not-run"}, nil, h, ops)
			if !errors.Is(err, cause) || next != nil {
				t.Fatalf("unconfirmed checkpoint returned cursor=%v err=%v", next, err)
			}
			if ops.lists != 1 || ops.fetches != 1 || h.emitted != 1 {
				t.Fatalf("work continued after checkpoint failure: lists=%d fetches=%d emitted=%d", ops.lists, ops.fetches, h.emitted)
			}
		})
	}
}
