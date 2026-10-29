package asynqdl

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

func TestSourceRestrictionArchive(t *testing.T) {
	repo := &fakeRepo{}
	payload := []byte(`{"tenant_id":12,"knowledge_base_id":"kb","knowledge_id":"doc","attempt":3,"file_path":"private-resource","token":"sensitive"}`)
	task := asynq.NewTask(types.TypeDocumentProcess, payload)
	err := errors.Join(asynq.SkipRetry, &types.SourceProcessingRestrictedError{State: "hold"})
	callback := false
	handler := MiddlewareWithCallback(repo, func(context.Context, *asynq.Task, error) { callback = true })(asynq.HandlerFunc(func(context.Context, *asynq.Task) error { return err }))
	require.ErrorIs(t, handler.ProcessTask(context.Background(), task), asynq.SkipRetry)
	require.False(t, callback, "admission stop must not mutate protected Knowledge")
	require.Equal(t, 1, repo.rowCount())
	row := repo.captureRow(0)
	require.Equal(t, "source_processing_restricted:hold", row.LastError)
	require.Equal(t, "doc", row.RelatedID)
	require.False(t, strings.Contains(string(row.Payload), "private-resource"))
	require.False(t, strings.Contains(string(row.Payload), "sensitive"))
	require.False(t, RecordSourceRestriction(repo, context.Background(), task, asynq.SkipRetry))
	require.False(t, RecordSourceRestriction(repo, context.Background(), task, types.ErrLegacySourceRestricted))
	require.Equal(t, 1, repo.rowCount())
}
