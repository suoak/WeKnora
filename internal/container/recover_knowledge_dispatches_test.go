package container

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestResetPendingTasksPreservesDurableDocumentIntent(t *testing.T) {
	t.Setenv("REDIS_ADDR", "")
	db := setupResetPendingDB(t)
	for _, state := range []string{"pending", "dispatched", "processing", "recovery", "processed"} {
		id := "dispatch-" + state
		require.NoError(
			t,
			db.Exec(
				"INSERT INTO knowledges (id,tenant_id,parse_status) VALUES (?,?,?)",
				id,
				7,
				types.ParseStatusPending,
			).Error,
		)
		require.NoError(
			t,
			db.Create(
				&types.TaskPendingOp{
					TenantID: 7,
					TaskType: types.TypeDocumentProcess,
					Scope:    types.KnowledgeDispatchScope,
					ScopeID:  id,
					DedupKey: "document-" + id,
					Op:       state,
					Payload:  []byte("{}"),
				},
			).Error,
		)
	}
	resetPendingTasks(db)
	var count int64
	require.NoError(
		t,
		db.Table("knowledges").Where("parse_status = ?", types.ParseStatusPending).Count(&count).Error,
	)
	require.EqualValues(t, 5, count)
}
