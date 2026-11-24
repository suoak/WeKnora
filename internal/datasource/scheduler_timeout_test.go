package datasource

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

type timeoutCapturingEnqueuer struct {
	task    *asynq.Task
	timeout time.Duration
}

func (e *timeoutCapturingEnqueuer) Enqueue(task *asynq.Task, options ...asynq.Option) (*asynq.TaskInfo, error) {
	e.task = task
	for _, option := range options {
		if option.Type() == asynq.TimeoutOpt {
			e.timeout = option.Value().(time.Duration)
		}
	}
	return &asynq.TaskInfo{ID: "scheduled-timeout-test"}, nil
}

func TestSchedulerUsesConfiguredSourceTimeout(t *testing.T) {
	t.Setenv("DATASOURCE_SYNC_TIMEOUT_OVERRIDES_JSON", `{"large":"6h","existing":"24h"}`)
	for _, tc := range []struct {
		source string
		want   time.Duration
	}{{"large", 6 * time.Hour}, {"existing", 24 * time.Hour}, {"ordinary", 2 * time.Hour}} {
		t.Run(tc.source, func(t *testing.T) {
			repo := newFakeDataSourceRepo()
			require.NoError(t, repo.Create(context.Background(), &types.DataSource{
				ID: tc.source, TenantID: 1, Type: "notion", Status: types.DataSourceStatusActive,
				SyncSchedule: "0 */30 * * * *",
			}))
			queue := &timeoutCapturingEnqueuer{}
			scheduler := NewScheduler(repo, newFakeSyncLogRepo(), queue)
			scheduler.triggerSync(tc.source, 1)
			require.NotNil(t, queue.task, "normal scheduler did not enqueue")
			require.Equal(t, tc.want, queue.timeout, "scheduled source budget differs from configured budget")
			var payload types.DataSourceSyncPayload
			require.NoError(t, json.Unmarshal(queue.task.Payload(), &payload))
			require.Equal(t, tc.source, payload.DataSourceID)
			require.Equal(t, "schedule", payload.Trigger)
			require.False(t, payload.ForceFull)
		})
	}
}
