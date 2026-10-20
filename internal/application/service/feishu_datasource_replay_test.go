package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type replayKnowledge struct {
	interfaces.KnowledgeService
	calls int
	row   *types.Knowledge
	err   error
}

func (s *replayKnowledge) ResumeKnowledgeDispatch(_ context.Context, existing *types.Knowledge) error {
	if s.row == nil || s.row.ID != existing.ID {
		return ErrFeishuDispatchUnconfirmed
	}
	return s.err
}
func (s *replayKnowledge) RecoverKnowledgeDispatches(context.Context) error { return s.err }

func (s *replayKnowledge) ReparseKnowledge(context.Context, string, *types.KnowledgeProcessOverrides) (*types.Knowledge, error) {
	s.calls++
	return s.row, s.err
}

func replayRow(status string) *types.Knowledge {
	sum := md5.Sum([]byte("v1"))
	return &types.Knowledge{ID: "stable", FileHash: hex.EncodeToString(sum[:]), FileName: "doc.docx", ParseStatus: status}
}

func TestFeishuReplayPreservesExistingID(t *testing.T) {
	for _, status := range []string{types.ParseStatusCompleted, types.ParseStatusFinalizing, types.ParseStatusProcessing} {
		t.Run(status, func(t *testing.T) {
			svc := &replayKnowledge{}
			err := reconcileFeishuReplay(context.Background(), svc, replayRow(status), &types.FetchedItem{Content: []byte("v1"), FileName: "doc.docx"})
			var duplicate *types.DuplicateKnowledgeError
			require.ErrorAs(t, err, &duplicate)
			require.Equal(t, "stable", duplicate.Knowledge.ID)
			require.Zero(t, svc.calls)
		})
	}
}

func TestFeishuChangedReplayNeverDeletesOrReplaces(t *testing.T) {
	svc := &replayKnowledge{}
	for _, item := range []*types.FetchedItem{{Content: []byte("v2"), FileName: "doc.docx"}, {URL: "https://example.test"}} {
		err := reconcileFeishuReplay(context.Background(), svc, replayRow(types.ParseStatusCompleted), item)
		require.ErrorIs(t, err, ErrFeishuStagingRequired)
		require.Zero(t, svc.calls)
	}
}

func TestFeishuDispatchUncertaintyNeverReparsesExistingContent(t *testing.T) {
	for _, status := range []string{types.ParseStatusPending, types.ParseStatusFailed} {
		row := replayRow(status)
		row.ErrorMessage = "Failed to enqueue processing task"
		svc := &replayKnowledge{}
		err := reconcileFeishuReplay(context.Background(), svc, row, &types.FetchedItem{Content: []byte("v1"), FileName: "doc.docx"})
		require.ErrorIs(t, err, ErrFeishuDispatchUnconfirmed)
		require.Zero(t, svc.calls)
	}
}

func TestFeishuAdmissionScopeDoesNotChangeOtherConnectors(t *testing.T) {
	for _, kind := range []string{"rss", "gitlab", "notion", "confluence"} {
		require.False(t, isFeishuAdmissionSource(kind))
	}
	for _, kind := range []string{"feishu", "feishu_drive", "lark", "lark_drive"} {
		require.True(t, isFeishuAdmissionSource(kind))
	}
}

// This release-gate test intentionally specifies the required recovery
// contract. A fail-closed response preserves data but does not satisfy it.
func TestFeishuGateBEnqueueFailureMustRecoverOnSameKnowledgeID(t *testing.T) {
	row := replayRow(types.ParseStatusFailed)
	row.ErrorMessage = "Failed to enqueue processing task"
	svc := &replayKnowledge{row: &types.Knowledge{ID: row.ID, ParseStatus: types.ParseStatusPending}}
	err := reconcileFeishuReplay(context.Background(), svc, row, &types.FetchedItem{Content: []byte("v1"), FileName: "doc.docx"})
	var accepted *types.DuplicateKnowledgeError
	require.ErrorAs(t, err, &accepted, "Gate B requires durable dispatch recovery, not permanent needs_attention")
	require.Equal(t, row.ID, accepted.Knowledge.ID)
}
