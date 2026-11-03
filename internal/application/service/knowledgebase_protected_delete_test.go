package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type protectedKBDeleteRepo struct {
	populatedKBKnowledgeRepo
	calls int
	err error
}

func (r *protectedKBDeleteRepo) CleanupLegacyKnowledgeForKBDeletion(
	_ context.Context, tenant uint64, kb, kid string, _ []types.RetrieverEngineParams,
) (bool, error) {
	if tenant != 1 || kb != "kb" || kid != "protected" {
		return true, errors.New("wrong deletion identity")
	}
	r.calls++
	return true, r.err
}

func TestProcessKBDeleteProtectedDocumentsNeverUseOrdinaryCleanup(t *testing.T) {
	for _, injected := range []error{nil, errors.New("live publication lease")} {
		repo := &protectedKBDeleteRepo{
			populatedKBKnowledgeRepo: populatedKBKnowledgeRepo{items: []*types.Knowledge{
				{ID: "protected", TenantID: 1, KnowledgeBaseID: "kb", StorageSize: 100, FilePath: "retained"},
			}}, err: injected,
		}
		// Nil ordinary file/model/vector/tenant dependencies panic if invoked.
		svc := &knowledgeBaseService{kgRepo: repo, taskInspector: &recordingKBTaskInspector{}}
		err := svc.ProcessKBDelete(context.Background(), kbDeletePayload(t, "kb", 1))
		require.Equal(t, 1, repo.calls)
		if injected == nil {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, injected)
		}
	}
}
