package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type exactSummaryBatchChunks struct {
	interfaces.ChunkRepository
	rows map[string]*types.Chunk
}

func (r *exactSummaryBatchChunks) GetChunkByID(ctx context.Context, tenant uint64, id string) (*types.Chunk, error) {
	row := r.rows[id]
	if row == nil {
		return nil, fmt.Errorf("chunk not found")
	}
	if tenant != row.TenantID {
		return nil, types.ErrExactDocumentScope
	}
	ok, err := types.CheckExactDocumentRead(ctx, row.KnowledgeID, row.KnowledgeBaseID)
	if !ok || err != nil {
		return nil, types.ErrExactDocumentScope
	}
	clone := *row
	return &clone, nil
}

func TestExactSummaryFreshnessUsesBoundedLiveProofAndAllParserFingerprints(t *testing.T) {
	for _, scenario := range []string{"current", "changed", "enabled", "missing", "revoked"} {
		t.Run(scenario, func(t *testing.T) {
			consumer := &exactPersonalConsumer{
				source:  &personalExactDocument{tenant: 1, knowledge: "doc", kb: "kb"},
				outputs: map[string][32]byte{},
			}
			repo := &exactSummaryBatchChunks{rows: map[string]*types.Chunk{}}
			var source []*types.Chunk
			for i := range 205 {
				row := &types.Chunk{
					ID: fmt.Sprint(i), TenantID: 1, KnowledgeID: "doc", KnowledgeBaseID: "kb",
					Content: "verified parser output", ChunkType: types.ChunkTypeText,
				}
				digest, err := exactChunkDigest(row)
				require.NoError(t, err)
				consumer.outputs[row.ID] = digest
				repo.rows[row.ID] = row
				clone := *row
				clone.IsEnabled = true // Summary input projection; stored staging rows stay disabled.
				source = append(source, &clone)
			}
			calls, refuseAt := 0, 0
			ctx, err := types.WithExactDocumentScope(types.WithExecutionTenant(context.Background(), 1),
				types.ExactDocumentScope{
					TenantID: 1, KnowledgeID: "doc", KBID: "kb",
					Read: func(context.Context) error {
						calls++
						if calls == refuseAt {
							return types.ErrExactDocumentScope
						}
						return nil
					}, Commit: func(*gorm.DB) error { return nil },
				})
			require.NoError(t, err)
			switch scenario {
			case "changed":
				repo.rows["150"].Content = "unproved replacement"
			case "enabled":
				repo.rows["150"].IsEnabled = true
			case "missing":
				delete(repo.rows, "150")
			case "revoked":
				refuseAt = calls + 4 // After the second page, before any summary is saved.
			}
			stale, err := exactSummarySourceChanged(ctx, consumer, repo, 1, "doc", source)
			if scenario == "current" {
				require.NoError(t, err)
				require.False(t, stale)
				require.Equal(t, 7, calls, "205 fresh chunk checks need six boundary proofs, not 205 remote calls")
				return
			}
			require.True(t, stale)
			if scenario == "enabled" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
