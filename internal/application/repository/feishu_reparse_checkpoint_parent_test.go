package repository

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/testutil/syncdb"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestSourceDocumentReparseCheckpointParentRequiresSameBinding(t *testing.T) {
	previous := types.FeishuDocumentReparse{
		ID: "previous", DataSourceID: "source", TenantID: 1,
		KnowledgeBaseID: "kb", KnowledgeID: "doc", RootID: "root", ExternalID: "external", FileType: "xlsx",
		ConfigHash: strings.Repeat(
			"1",
			64,
		), KBConfigHash: strings.Repeat("2", 64), PreviousProcessHash: strings.Repeat("3", 64),
		ErrorCode: "retry_exhausted", Initiator: types.TaskInitiator{UserID: "owner"},
	}
	current := previous
	current.ID, current.ErrorCode = "", ""
	require.True(t, reusableReparseRequest(previous, &current))
	for _, mutation := range []func(*types.FeishuDocumentReparse){
		func(r *types.FeishuDocumentReparse) { r.DataSourceID = "foreign" },
		func(r *types.FeishuDocumentReparse) { r.TenantID = 2 },
		func(r *types.FeishuDocumentReparse) { r.KnowledgeBaseID = "foreign" },
		func(r *types.FeishuDocumentReparse) { r.KnowledgeID = "foreign" },
		func(r *types.FeishuDocumentReparse) { r.RootID = "foreign" },
		func(r *types.FeishuDocumentReparse) { r.ExternalID = "foreign" },
		func(r *types.FeishuDocumentReparse) { r.FileType = "docx" },
		func(r *types.FeishuDocumentReparse) { r.ConfigHash = "changed" },
		func(r *types.FeishuDocumentReparse) { r.KBConfigHash = "changed" },
		func(r *types.FeishuDocumentReparse) { r.PreviousProcessHash = "changed" },
		func(r *types.FeishuDocumentReparse) { r.Initiator.UserID = "other owner" },
		func(r *types.FeishuDocumentReparse) { r.ProcessConfig = &types.KnowledgeProcessOverrides{} },
	} {
		changed := current
		mutation(&changed)
		require.False(t, reusableReparseRequest(previous, &changed))
	}
	previous.ErrorCode = "source_document_access_unavailable"
	require.False(t, reusableReparseRequest(previous, &current))
	require.False(t, reusableReparseRequest(previous, nil))
}

func TestSourceDocumentReparseCheckpointParentIsSelectedByServer(t *testing.T) {
	db := syncdb.Open(t, "sqlite")
	require.NoError(t, db.AutoMigrate(&types.TaskPendingOp{}))
	ds := &types.DataSource{ID: "source", TenantID: 1, KnowledgeBaseID: "kb"}
	prior := types.FeishuDocumentReparse{
		ID: "trusted-parent", DataSourceID: ds.ID, TenantID: ds.TenantID,
		KnowledgeBaseID: ds.KnowledgeBaseID, KnowledgeID: "doc",
		RootID: "root", ExternalID: "external", FileType: "xlsx",
		ConfigHash: strings.Repeat("1", 64), KBConfigHash: strings.Repeat("2", 64), ErrorCode: "retry_exhausted",
		Initiator: types.TaskInitiator{UserID: "owner"},
	}
	payload, err := json.Marshal(prior)
	require.NoError(t, err)
	makeRow := func(tenant uint64, source, doc, state string) {
		row := &types.TaskPendingOp{
			TaskType: sourceDocumentReparseTask, Scope: sourceDocumentReparseScope,
			TenantID: tenant, ScopeID: source, DedupKey: doc, Op: state, Payload: payload, EnqueuedAt: time.Now(),
		}
		require.NoError(t, db.Create(row).Error)
	}
	makeRow(1, "source", "doc", "failed")
	makeRow(2, "source", "doc", "failed")
	makeRow(1, "foreign", "doc", "failed")
	makeRow(1, "source", "foreign", "failed")
	makeRow(1, "source", "doc", "completed")
	current := prior
	current.ID, current.ErrorCode, current.ReuseCheckpointID = "", "", "forged-parent"
	selected, err := sourceReparseCheckpointParent(db, ds, &current)
	require.NoError(t, err)
	require.Equal(t, prior.ID, selected, "untrusted parent input and foreign/newer operations must be ignored")
	current.KBConfigHash = "changed"
	selected, err = sourceReparseCheckpointParent(db, ds, &current)
	require.NoError(t, err)
	require.Empty(t, selected)
}
