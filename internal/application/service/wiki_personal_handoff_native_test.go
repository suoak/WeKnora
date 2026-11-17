//go:build anydoc && personal_exact_live

package service

import (
	"context"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPersonalWikiHandoffDurableAndRecoversLostTrigger(t *testing.T) {
	exerciseNativeResumeWithFormat(t, false, false, nil, nativeResumeFormatOptions{
		Format: "docx", SkipAdditionalReadMatrix: true,
		AssertPublished: func(f *personalRecoveryFixture, source *personalExactDocument, _ types.PersonalExactReceipt) {
			assertPersonalWikiGeneration(t, f, source)
			require.NoError(t, f.db.AutoMigrate(&types.KnowledgeProcessingSpan{}))
			ctx := context.Background()
			queued, err := repository.EnsureWikiPersonalHandoff(ctx, f.db, 1, source.kb, source.knowledge)
			require.NoError(t, err)
			require.False(t, queued, "Wiki-disabled publication does not schedule generation")
			require.NoError(t, f.db.Exec("UPDATE knowledge_bases SET indexing_strategy=jsonb_set(COALESCE(indexing_strategy::jsonb,'{}'::jsonb),'{wiki_enabled}','true'::jsonb) WHERE id=?", source.kb).Error)
			defer func() {
				require.NoError(t, f.db.Exec("UPDATE knowledge_bases SET indexing_strategy=jsonb_set(indexing_strategy::jsonb,'{wiki_enabled}','false'::jsonb) WHERE id=?", source.kb).Error)
			}()
			var before string
			query := "SELECT md5(to_jsonb(k)::text) FROM knowledges k WHERE id=?"
			require.NoError(t, f.db.Raw(query, source.knowledge).Scan(&before).Error)
			queue := &dispatchQueue{down: true}
			f.ks.task = queue
			require.Error(t, f.p.handoffPublishedPersonalWiki(ctx, 1, source.kb, source.knowledge), "Redis failure must leave the durable intent intact")
			count := func(scope string) int64 {
				var n int64
				require.NoError(t, f.db.Model(&types.TaskPendingOp{}).Where("scope=? AND (scope_id=? OR dedup_key=?)", scope, source.knowledge, source.knowledge).Count(&n).Error)
				return n
			}
			require.EqualValues(t, 1, count("personal_wiki_handoff"))
			require.EqualValues(t, 1, count("knowledge_base"))
			var wg sync.WaitGroup
			results := make(chan error, 4)
			for range 4 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, e := repository.EnsureWikiPersonalHandoff(ctx, f.db, 1, source.kb, source.knowledge)
					results <- e
				}()
			}
			wg.Wait()
			close(results)
			for e := range results {
				require.NoError(t, e)
			}
			require.EqualValues(t, 1, count("knowledge_base"), "replicas must not duplicate the ingest intent")
			queue.setDown(false)
			h := NewHousekeepingService(f.db, nil, nil, queue)
			h.recoverPersonalWikiHandoffs(ctx)
			require.Len(t, queue.snapshot(), 1, "completed parsing still allows a lost Wiki trigger to recover")
			require.Equal(t, types.TypeWikiIngest, queue.snapshot()[0].Type())
			require.NoError(t, f.db.Where("scope='knowledge_base' AND dedup_key=?", source.knowledge).Delete(&types.TaskPendingOp{}).Error)
			h.recoverPersonalWikiHandoffs(ctx)
			require.EqualValues(t, 0, count("knowledge_base"), "consumed work is not recreated on every sweep")
			var after string
			require.NoError(t, f.db.Raw(query, source.knowledge).Scan(&after).Error)
			require.Equal(t, before, after, "Wiki repair must preserve receipt-bound parser lifecycle")
		},
	})
}

func TestPersonalWikiHandoffRejectsChangedPublishedInputs(t *testing.T) {
	exerciseNativeResumeWithFormat(t, false, false, nil, nativeResumeFormatOptions{
		Format: "docx", SkipAdditionalReadMatrix: true,
		AssertPublished: func(f *personalRecoveryFixture, source *personalExactDocument, _ types.PersonalExactReceipt) {
			assertPersonalWikiGeneration(t, f, source)
			require.NoError(t, f.db.AutoMigrate(&types.KnowledgeProcessingSpan{}))
			require.NoError(t, f.db.Exec("UPDATE knowledge_bases SET indexing_strategy=jsonb_set(COALESCE(indexing_strategy::jsonb,'{}'::jsonb),'{wiki_enabled}','true'::jsonb) WHERE id=?", source.kb).Error)
			defer func() {
				require.NoError(t, f.db.Exec("UPDATE knowledge_bases SET indexing_strategy=jsonb_set(indexing_strategy::jsonb,'{wiki_enabled}','false'::jsonb) WHERE id=?", source.kb).Error)
			}()
			for _, fault := range []string{"staging", "chunk", "tenant"} {
				tx := f.db.Begin()
				require.NoError(t, tx.Error)
				tenant := uint64(1)
				switch fault {
				case "staging":
					require.NoError(t, tx.Model(&types.TaskPendingOp{}).Where("scope=? AND scope_id=?", types.PersonalExactReceiptScope, source.knowledge).Update("op", "staging").Error)
				case "chunk":
					require.NoError(t, tx.Exec("UPDATE chunks SET content=content||' changed' WHERE knowledge_id=?", source.knowledge).Error)
				case "tenant":
					tenant = 2
				}
				queued, err := repository.EnsureWikiPersonalHandoff(context.Background(), tx, tenant, source.kb, source.knowledge)
				require.Error(t, err, fault)
				require.False(t, queued)
				require.NoError(t, tx.Rollback().Error)
			}
			rows, err := repository.WikiPersonalHandoffCandidates(context.Background(), f.db, 0, 1)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			next, err := repository.WikiPersonalHandoffCandidates(context.Background(), f.db, rows[0].ReceiptID, 1)
			require.NoError(t, err)
			require.Empty(t, next, "cursor advances past a refused old receipt")
			ctx := context.Background()
			plan, err := repository.PrepareWikiPersonalHandoff(ctx, f.db, 1, source.kb, "", false)
			require.NoError(t, err)
			require.Len(t, plan.Publications, 1)
			_, err = repository.PrepareWikiPersonalHandoff(ctx, f.db, 1, source.kb, "stale", true)
			require.Error(t, err)
			applied, err := repository.PrepareWikiPersonalHandoff(ctx, f.db, 1, source.kb, plan.Snapshot, true)
			require.NoError(t, err)
			require.True(t, applied.Applied)
			require.Equal(t, 1, applied.Queued)
			queued, err := repository.EnsureWikiPersonalHandoff(ctx, f.db, 1, source.kb, source.knowledge)
			require.NoError(t, err)
			require.False(t, queued)
			rows, err = repository.WikiPersonalHandoffCandidates(context.Background(), f.db, 0, 1)
			require.NoError(t, err)
			require.Empty(t, rows, "one handoff per immutable publication")
		},
	})
}
