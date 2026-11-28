package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	pgretrieve "github.com/Tencent/WeKnora/internal/application/repository/retriever/postgres"
	"github.com/Tencent/WeKnora/internal/database"
	"github.com/Tencent/WeKnora/internal/testutil/feishulegacy"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type legacyAckLossPool struct {
	*sql.DB
	armed *atomic.Bool
}

func (p *legacyAckLossPool) BeginTx(
	ctx context.Context,
	opts *sql.TxOptions,
) (gorm.ConnPool, error) {
	tx, err := p.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &legacyAckLossTx{Tx: tx, armed: p.armed}, nil
}

type legacyAckLossTx struct {
	*sql.Tx
	armed *atomic.Bool
}

func (t *legacyAckLossTx) Commit() error {
	if err := t.Tx.Commit(); err != nil {
		return err
	}
	if t.armed.Swap(false) {
		return errors.New("injected acknowledgement loss AFTER real PostgreSQL COMMIT")
	}
	return nil
}

func TestFeishuLegacyAtomicPostgres(t *testing.T) {
	dsn := feishulegacy.PrivateDSN(t)
	if dsn == "" {
		t.Skip("requires disposable feishu_atomic_test PostgreSQL/ParadeDB")
	}
	require.Contains(t, dsn, "feishu_atomic_test", "never use a shared or production database")
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	t.Chdir(root)
	require.NoError(t, database.RunMigrations(dsn))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(
		t,
		db.Exec(
			"CREATE INDEX feishu_test_hnsw3 ON embeddings USING hnsw "+
				"((embedding::halfvec(3)) halfvec_cosine_ops) WHERE dimension=3",
		).Error,
	)
	tenant := &types.Tenant{ID: 809901, Name: "synthetic-feishu-atomic", Status: "active"}
	require.NoError(t, db.Create(tenant).Error)
	model := &types.Model{
		ID:       "synthetic-1024",
		TenantID: tenant.ID,
		Name:     "synthetic",
		Type:     types.ModelTypeEmbedding,
	}
	require.NoError(t, db.Create(model).Error)
	kb := &types.KnowledgeBase{
		ID:               uuid.NewString(),
		TenantID:         tenant.ID,
		Name:             "synthetic",
		Type:             "document",
		EmbeddingModelID: "synthetic-1024",
		IndexingStrategy: types.IndexingStrategy{WikiEnabled: true},
	}
	require.NoError(t, db.Create(kb).Error)
	ds := &types.DataSource{
		ID:              uuid.NewString(),
		TenantID:        tenant.ID,
		KnowledgeBaseID: kb.ID,
		Name:            "synthetic-Wiki",
		Type:            "feishu",
		Status:          "active",
		Config:          types.JSON(`{}`),
	}
	require.NoError(t, db.Create(ds).Error)
	log := &types.SyncLog{
		ID:                  uuid.NewString(),
		DataSourceID:        ds.ID,
		TenantID:            tenant.ID,
		Status:              types.SyncLogStatusRunning,
		ExecutionGeneration: 1,
		ExecutionClaimed:    true,
	}
	require.NoError(t, db.Create(log).Error)
	ctx := types.WithSyncExecution(
		context.Background(),
		types.SyncExecution{
			DataSourceID: ds.ID,
			SyncLogID:    log.ID,
			TenantID:     tenant.ID,
			Generation:   1,
		},
	)
	store := NewFeishuLegacyStore(db)
	stamp, err := store.ModelStamp(ctx, kb.EmbeddingModelID)
	require.NoError(t, err)
	boundMetadata, _ := json.Marshal(
		map[string]string{
			"datasource_id":      ds.ID,
			"external_id":        "same-cloud-id",
			"source_resource_id": "root",
		},
	)
	kid := uuid.NewString()
	baseline := &types.Knowledge{
		ID:              kid,
		TenantID:        tenant.ID,
		KnowledgeBaseID: kb.ID,
		Title:           "RC3 existing",
		Type:            "file",
		Channel:         "feishu",
		Metadata:        types.JSON(boundMetadata),
		FileHash:        "legacy-before-enrollment",
		ParseStatus:     types.ParseStatusCompleted,
		EnableStatus:    "enabled",
	}
	require.NoError(t, db.Create(baseline).Error)
	baselineChunk := uuid.NewString()
	require.NoError(
		t,
		db.Create(
			&types.Chunk{
				ID:              baselineChunk,
				TenantID:        tenant.ID,
				KnowledgeBaseID: kb.ID,
				KnowledgeID:     kid,
				Content:         "rc3baselinecanary",
				IsEnabled:       true,
				ChunkType:       "text",
			},
		).Error,
	)
	require.NoError(
		t,
		db.Exec(
			"INSERT INTO embeddings(source_id,source_type,chunk_id,knowledge_i"+
				"d,knowledge_base_id,content,dimension,embedding,is_enabled) VALUE"+
				"S(?,0,?,?,?,'rc3baselinecanary',3,'[1,0,0]'::halfvec,true)",
			baselineChunk,
			baselineChunk,
			kid,
			kb.ID,
		).Error,
	)
	makeVersion := func(body string, rev int) (
		*types.FeishuLegacyResource, *types.FeishuLegacyAttempt, types.FeishuLegacyManifest,
	) {
		item := &types.FetchedItem{
			ExternalID: "same-cloud-id",
			UpdatedAt:  time.Date(2026, 10, 21, 1, rev, 0, 0, time.UTC),
			Content:    []byte(body),
			FileName:   "document.md",
		}
		h := sha256.Sum256(item.Content)
		r, a, e := store.Begin(ctx, ds, item, hex.EncodeToString(h[:]))
		require.NoError(t, e)
		if kid == "" {
			kid = r.KnowledgeID
		}
		require.Equal(t, kid, r.KnowledgeID)
		chunk := &types.Chunk{
			ID:                     uuid.NewString(),
			TenantID:               tenant.ID,
			KnowledgeBaseID:        kb.ID,
			KnowledgeID:            kid,
			Content:                body,
			IsEnabled:              true,
			IndexStatus:            "ready",
			ChunkType:              "text",
			RelationChunks:         types.JSON(`[]`),
			IndirectRelationChunks: types.JSON(`[]`),
			Metadata:               types.JSON(`{}`),
		}
		return r, a, types.FeishuLegacyManifest{
			Knowledge: types.Knowledge{
				ID:              kid,
				TenantID:        tenant.ID,
				KnowledgeBaseID: kb.ID,
				Title:           "canary",
				Type:            "file",
				Channel:         "feishu",
				FileName:        "document.md",
				FileType:        "md",
				FileHash:        body,
				Metadata:        types.JSON(boundMetadata),
			},
			Chunks:           []*types.Chunk{chunk},
			EmbeddingModelID: kb.EmbeddingModelID,
			KBConfigSHA256:   LegacyKBConfigHash(kb),
			ModelStamp:       stamp,
			Dimension:        3,
		}
	}
	visible := func(word string) int64 {
		var n int64
		require.NoError(
			t,
			db.Raw("SELECT count(*) FROM embeddings WHERE knowledge_id=? AND content ||| ?", kid, word).
				Scan(&n).
				Error,
		)
		return n
	}
	r, a, m := makeVersion("oldcanary", 1)
	require.NoError(t, store.Prepare(ctx, ds, a, m, [][]float32{{1, 0, 0}}))
	require.Zero(t, visible("oldcanary"), "private candidate leaked into public BM25")
	require.EqualValues(
		t,
		1,
		visible("rc3baselinecanary"),
		"existing RC3 index must remain readable during staging",
	)
	var private int64
	require.NoError(
		t,
		db.Raw("SELECT count(*) FROM feishu_legacy_index_candidates WHERE attempt_id=? AND "+
			"content ||| ?", a.ID, "oldcanary").
			Scan(&private).
			Error,
	)
	require.EqualValues(t, 1, private)
	// If the durable Wiki write fails, the entire publication rolls back. The
	// previous verified index remains current and the candidate stays retryable.
	require.NoError(t, db.Exec(`CREATE FUNCTION feishu_test_wiki_enqueue_fault() RETURNS trigger
 LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic Wiki enqueue failure'; END $$;
 CREATE TRIGGER feishu_test_wiki_enqueue_fault BEFORE INSERT ON task_pending_ops
 FOR EACH ROW EXECUTE FUNCTION feishu_test_wiki_enqueue_fault()`).Error)
	_, err = store.Publish(ctx, ds, a)
	require.Error(t, err)
	require.Zero(t, visible("oldcanary"))
	require.EqualValues(t, 1, visible("rc3baselinecanary"))
	require.NoError(t, db.Exec(`DROP TRIGGER feishu_test_wiki_enqueue_fault ON task_pending_ops;
 DROP FUNCTION feishu_test_wiki_enqueue_fault()`).Error)
	receipt, err := store.Publish(ctx, ds, a)
	require.NoError(t, err)
	require.EqualValues(t, 1, receipt.Chunks)
	require.EqualValues(t, 1, visible("oldcanary"))
	require.Zero(
		t,
		visible("rc3baselinecanary"),
		"old committed version must not mix with the new version",
	)
	_, err = store.Receipt(ctx, r, a)
	require.NoError(t, err, "lost-ACK reconciliation must use durable receipt")
	var wikiOps []types.TaskPendingOp
	require.NoError(t, db.Where("scope=? AND scope_id=? AND dedup_key=?",
		types.TaskScopeKnowledgeBase, kb.ID, kid).Find(&wikiOps).Error)
	require.Len(t, wikiOps, 1, "receipt retries must not duplicate the committed Wiki operation")
	require.Equal(t, types.TypeWikiIngest, wikiOps[0].TaskType)
	require.Equal(t, "ingest", wikiOps[0].Op)
	var wikiPayload map[string]string
	require.NoError(t, json.Unmarshal(wikiOps[0].Payload, &wikiPayload))
	require.Equal(t, kid, wikiPayload["knowledge_id"])
	require.NotContains(
		t,
		string(wikiOps[0].Payload),
		"oldcanary",
		"only an identity enters the Wiki queue",
	)
	t.Run("large-checkpoint-yield-requires-live-request-and-progress", func(t *testing.T) {
		tx := db.Begin()
		require.NoError(t, tx.Error)
		defer tx.Rollback()
		local := NewFeishuLegacyStore(tx)
		request, err := local.SourceReparseTarget(ctx, ds, kid)
		require.NoError(t, err)
		request.Initiator = types.TaskInitiator{
			UserID: uuid.NewString(),
			Role:   types.TenantRoleAdmin,
		}
		request.KBConfigHash = LegacyKBConfigHash(kb)
		_, err = local.QueueSourceDocumentReparse(ctx, ds, request)
		require.NoError(t, err)
		// Install a typed spreadsheet claim in the isolated private test store.
		// The shared fixture's published document is markdown; no API or public
		// source binding is modified by this checkpoint-protocol test.
		require.NoError(
			t,
			tx.Exec(`UPDATE task_pending_ops SET payload=jsonb_set(payload,'{file_type}','"xlsx"')
WHERE scope=? AND dedup_key=? AND op='queued'`, sourceDocumentReparseScope, kid).Error,
		)
		work, claimed, err := local.ClaimSourceDocumentReparse(ctx, ds)
		require.NoError(t, err)
		item := &types.FetchedItem{
			ExternalID: "same-cloud-id", SourceResourceID: "root", FileName: "document.md",
			Content: []byte("oldcanary"), UpdatedAt: time.Date(2026, 10, 21, 1, 1, 0, 0, time.UTC),
		}
		hash := sha256.Sum256(item.Content)
		_, candidate, err := local.Begin(work, ds, item, hex.EncodeToString(hash[:]))
		require.NoError(t, err)
		checkpoint := types.FeishuLegacyEmbeddingCheckpoint{
			Version: 2, RequestID: claimed.ID, SourceConfig: claimed.ConfigHash,
			ModelID: m.EmbeddingModelID, ModelStamp: m.ModelStamp,
			KBConfigSHA256: m.KBConfigSHA256, Dimension: m.Dimension,
		}
		for i := 0; i < 10001; i++ {
			text := fmt.Sprintf("input-%d", i)
			checkpoint.Inputs = append(checkpoint.Inputs, types.FeishuLegacyEmbeddingInput{
				ChunkID: uuid.NewString(), SHA256: LegacyEmbeddingInputHash(text),
			})
		}
		require.Error(
			t,
			local.StartEmbeddingCheckpoint(ctx, ds, candidate, checkpoint),
			"ordinary sync must retain its original limit",
		)
		require.NoError(t, local.StartEmbeddingCheckpoint(work, ds, candidate, checkpoint))
		require.Error(
			t,
			local.SaveEmbeddingCheckpoint(
				ctx,
				ds,
				candidate,
				[]string{"input-0"},
				[][]float32{{1, 0, 0}},
			),
		)
		require.NoError(
			t,
			local.SaveEmbeddingCheckpoint(
				work,
				ds,
				candidate,
				[]string{"input-0"},
				[][]float32{{1, 0, 0}},
			),
		)
		progress := &FeishuEmbeddingProgress{Owner: candidate, Completed: 1, Total: 10001}
		// No new progress may reset the failure counter.
		claim := work.Value(sourceDocumentReparseKey{}).(*sourceDocumentReparseClaim)
		claim.request.CompletedChunks = 1
		payload, err := json.Marshal(claim.request)
		require.NoError(t, err)
		require.NoError(
			t,
			tx.Model(&types.TaskPendingOp{}).
				Where("id=?", claim.op.ID).
				Update("payload", json.RawMessage(payload)).
				Error,
		)
		require.Error(t, local.YieldSourceDocumentReparse(work, ds, progress))
		claim.request.CompletedChunks = 0
		payload, err = json.Marshal(claim.request)
		require.NoError(t, err)
		require.NoError(
			t,
			tx.Model(&types.TaskPendingOp{}).
				Where("id=?", claim.op.ID).
				Update("payload", json.RawMessage(payload)).
				Error,
		)
		require.NoError(t, local.YieldSourceDocumentReparse(work, ds, progress))
		status, err := local.SourceDocumentReparseStatus(ctx, ds, kid)
		require.NoError(t, err)
		require.Equal(t, "queued", status.State)
		require.Equal(t, 0, status.Attempts)
		require.Equal(t, 1, status.CompletedChunks)
		require.Equal(t, 10001, status.TotalChunks)
		require.Error(
			t,
			local.SaveEmbeddingCheckpoint(
				work,
				ds,
				candidate,
				[]string{"input-1"},
				[][]float32{{0, 1, 0}},
			),
			"released owner cannot write",
		)
		// A new valid source execution must reclaim the durable intent, not its old context.
		var op types.TaskPendingOp
		require.NoError(t, tx.First(&op, "id=?", claim.op.ID).Error)
		var saved types.FeishuDocumentReparse
		require.NoError(t, json.Unmarshal(op.Payload, &saved))
		require.Equal(t, 1, saved.ProgressWindows)
		require.True(t, saved.RetryAfter.After(time.Now()))
		saved.RetryAfter = time.Now().Add(-time.Minute)
		payload, err = json.Marshal(saved)
		require.NoError(t, err)
		require.NoError(
			t,
			tx.Model(&types.TaskPendingOp{}).
				Where("id=?", op.ID).
				Update("payload", json.RawMessage(payload)).
				Error,
		)
		execution, ok := types.SyncExecutionFromContext(ctx)
		require.True(t, ok)
		execution.Generation++
		require.NoError(
			t,
			tx.Model(&types.SyncLog{}).
				Where("id=?", execution.SyncLogID).
				Update("execution_generation", execution.Generation).
				Error,
		)
		nextCtx := types.WithSyncExecution(context.Background(), execution)
		nextWork, nextRequest, err := local.ClaimSourceDocumentReparse(nextCtx, ds)
		require.NoError(t, err)
		require.NotNil(t, nextRequest)
		require.Equal(t, 1, nextRequest.Attempts)
		_, nextOwner, err := local.Begin(nextWork, ds, item, hex.EncodeToString(hash[:]))
		require.NoError(t, err)
		require.NotEqual(t, candidate.ID, nextOwner.ID)
		for i := range checkpoint.Inputs {
			checkpoint.Inputs[i].ChunkID = uuid.NewString()
		}
		require.NoError(t, local.StartEmbeddingCheckpoint(nextWork, ds, nextOwner, checkpoint))
		cache, err := local.ReusableEmbeddingCheckpoint(nextWork, ds, nextOwner)
		require.NoError(t, err)
		require.Equal(t, []float32{1, 0, 0}, cache[LegacyEmbeddingInputHash("input-0")])
		require.NoError(t, local.SaveEmbeddingCheckpoint(nextWork, ds, nextOwner,
			[]string{"input-0", "input-1"}, [][]float32{{1, 0, 0}, {0, 1, 0}}))
		require.NoError(
			t,
			local.YieldSourceDocumentReparse(
				nextWork,
				ds,
				&FeishuEmbeddingProgress{Owner: nextOwner, Completed: 2, Total: 10001},
			),
		)
		var oldRows int64
		require.NoError(
			t,
			tx.Table("feishu_legacy_index_candidates").
				Where("attempt_id=?", candidate.ID).
				Count(&oldRows).
				Error,
		)
		require.Zero(
			t,
			oldRows,
			"the imported predecessor must not accumulate private duplicate vector rows",
		)
		status, err = local.SourceDocumentReparseStatus(nextCtx, ds, kid)
		require.NoError(t, err)
		require.Equal(t, 2, status.CompletedChunks)
		require.Zero(t, status.Attempts)
	})
	t.Run(
		"large-checkpoint-publication-requires-the-live-request-and-complete-input-set",
		func(t *testing.T) {
			tx := db.Begin()
			require.NoError(t, tx.Error)
			defer tx.Rollback()
			local := NewFeishuLegacyStore(tx)
			request, err := local.SourceReparseTarget(ctx, ds, kid)
			require.NoError(t, err)
			request.Initiator = types.TaskInitiator{
				UserID: uuid.NewString(),
				Role:   types.TenantRoleAdmin,
			}
			request.KBConfigHash = LegacyKBConfigHash(kb)
			_, err = local.QueueSourceDocumentReparse(ctx, ds, request)
			require.NoError(t, err)
			require.NoError(
				t,
				tx.Exec(
					`UPDATE task_pending_ops SET payload=jsonb_set(payload,'{file_type}','"xlsx"')
WHERE scope=? AND dedup_key=? AND op='queued'`,
					sourceDocumentReparseScope,
					kid,
				).Error,
			)
			work, claimed, err := local.ClaimSourceDocumentReparse(ctx, ds)
			require.NoError(t, err)
			item := &types.FetchedItem{
				ExternalID:       "same-cloud-id",
				SourceResourceID: "root",
				FileName:         "document.md",
				Content:          []byte("oldcanary"),
				UpdatedAt:        time.Date(2026, 10, 21, 1, 1, 0, 0, time.UTC),
			}
			hash := sha256.Sum256(item.Content)
			_, owner, err := local.Begin(work, ds, item, hex.EncodeToString(hash[:]))
			require.NoError(t, err)
			manifest := m
			manifest.Chunks = nil
			manifest.Knowledge.FileType = "xlsx"
			checkpoint := types.FeishuLegacyEmbeddingCheckpoint{
				Version:        2,
				RequestID:      claimed.ID,
				SourceConfig:   claimed.ConfigHash,
				ModelID:        m.EmbeddingModelID,
				ModelStamp:     m.ModelStamp,
				KBConfigSHA256: m.KBConfigSHA256,
				Dimension:      3,
			}
			vectors := make([][]float32, 10001)
			for i := range vectors {
				body := "largecanary"
				if i == len(vectors)-1 {
					body = "lastlargecanary"
				}
				chunk := *m.Chunks[0]
				chunk.ID = uuid.NewString()
				chunk.Content = body
				chunk.ChunkIndex = i
				manifest.Chunks = append(manifest.Chunks, &chunk)
				vectors[i] = []float32{1, 0, 0}
				checkpoint.Inputs = append(
					checkpoint.Inputs,
					types.FeishuLegacyEmbeddingInput{
						ChunkID: chunk.ID,
						SHA256: LegacyEmbeddingInputHash(
							buildLegacyIndexContent(m.Knowledge.Title, body),
						),
					},
				)
			}
			require.NoError(t, local.StartEmbeddingCheckpoint(work, ds, owner, checkpoint))
			require.NoError(t, local.SaveEmbeddingCheckpoint(work, ds, owner, []string{
				buildLegacyIndexContent(
					m.Knowledge.Title,
					"largecanary",
				), buildLegacyIndexContent(m.Knowledge.Title, "lastlargecanary"),
			}, [][]float32{{1, 0, 0}, {1, 0, 0}}))
			require.Error(
				t,
				local.Prepare(ctx, ds, owner, manifest, vectors),
				"the normal sync context cannot prepare a large request",
			)
			require.NoError(t, local.Prepare(work, ds, owner, manifest, vectors))
			_, err = local.Publish(ctx, ds, owner)
			require.Error(t, err, "a prepared manifest alone is not publication authority")
			receipt, err := local.Publish(work, ds, owner)
			require.NoError(t, err)
			require.EqualValues(t, 10001, receipt.Chunks)
			var publicCount int64
			require.NoError(
				t,
				tx.Model(&types.Chunk{}).
					Where("knowledge_id=? AND deleted_at IS NULL", kid).
					Count(&publicCount).
					Error,
			)
			require.EqualValues(t, 10001, publicCount)
			status, err := local.SourceDocumentReparseStatus(work, ds, kid)
			require.NoError(t, err)
			require.Equal(t, "completed", status.State)
		},
	)
	t.Run("source-document-reparse-is-durable-and-publishes-atomically", func(t *testing.T) {
		tx := db.Begin()
		require.NoError(t, tx.Error)
		defer tx.Rollback()
		selected := *ds
		selected.Config = types.JSON(`{"resource_ids":["root","approved-overlap"]}`)
		ds := &selected
		require.NoError(t, tx.Model(&types.DataSource{}).Where("id=?", ds.ID).Update("config", ds.Config).Error)
		local := NewFeishuLegacyStore(tx)
		request, err := local.SourceReparseTarget(ctx, ds, kid)
		require.NoError(t, err)
		request.Initiator = types.TaskInitiator{
			UserID: uuid.NewString(),
			Role:   types.TenantRoleAdmin,
		}
		request.KBConfigHash = LegacyKBConfigHash(kb)
		request.ProcessConfig = &types.KnowledgeProcessOverrides{
			ParserEngineOverrides: map[string]string{"test": "kept"},
		}
		accepted, err := local.QueueSourceDocumentReparse(ctx, ds, request)
		require.NoError(t, err)
		require.Equal(t, "queued", accepted.State)
		repeated, err := local.QueueSourceDocumentReparse(ctx, ds, request)
		require.NoError(t, err)
		require.Equal(
			t,
			accepted.ID,
			repeated.ID,
			"duplicate clicks must share one durable request",
		)
		conflict := *request
		conflict.ProcessConfig = &types.KnowledgeProcessOverrides{}
		_, err = local.QueueSourceDocumentReparse(ctx, ds, &conflict)
		require.ErrorIs(t, err, ErrLegacyVersionBusy)
		_, _, err = local.ClaimSourceDocumentReparse(context.Background(), ds)
		require.Error(t, err, "a stored request alone is never execution authority")
		work, claimed, err := local.ClaimSourceDocumentReparse(ctx, ds)
		require.NoError(t, err)
		require.Equal(t, accepted.ID, claimed.ID)
		require.Equal(t, 1, claimed.Attempts)
		config, ok := SourceDocumentReparseOverrides(work)
		require.True(t, ok)
		config.ParserEngineOverrides["test"] = "cannot-change-the-claim"
		config, _ = SourceDocumentReparseOverrides(work)
		require.Equal(t, "kept", config.ParserEngineOverrides["test"])
		item := &types.FetchedItem{
			ExternalID: "same-cloud-id", SourceResourceID: "root", FileName: "document.md",
			Content: []byte("oldcanary"), UpdatedAt: time.Date(2026, 10, 21, 1, 1, 0, 0, time.UTC),
		}
		hash := sha256.Sum256(item.Content)
		_, unchanged, err := local.Begin(ctx, ds, item, hex.EncodeToString(hash[:]))
		require.NoError(t, err)
		require.Equal(t, "published", unchanged.Status)
		resource, candidate, err := local.Begin(work, ds, item, hex.EncodeToString(hash[:]))
		require.NoError(t, err)
		require.Equal(
			t,
			"staging",
			candidate.Status,
			"explicit reparse must not reuse an unchanged receipt",
		)
		require.NotEqual(t, a.ID, candidate.ID)
		require.Equal(t, kid, resource.KnowledgeID)
		var manifest types.FeishuLegacyManifest
		encoded, err := json.Marshal(m)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(encoded, &manifest))
		manifest.Chunks[0].ID = uuid.NewString()
		require.NoError(t, manifest.Knowledge.SetProcessOverrides(request.ProcessConfig))
		var previous types.Knowledge
		require.NoError(t, tx.First(&previous, "id=?", kid).Error)
		concurrent := previous
		require.NoError(t, concurrent.SetProcessOverrides(&types.KnowledgeProcessOverrides{}))
		require.ErrorContains(t, sourceReparseProcessMetadata(work, tx, ds, resource,
			&concurrent, &manifest.Knowledge, true), "knowledge_process_config_changed",
			"an approved request cannot overwrite a concurrent parameter edit")
		unapproved := manifest.Knowledge
		require.NoError(t, unapproved.SetProcessOverrides(&types.KnowledgeProcessOverrides{}))
		require.ErrorContains(t, sourceReparseProcessMetadata(work, tx, ds, resource,
			&previous, &unapproved, true), "knowledge_process_config_changed",
			"the claim permits only the exact parameters accepted from the user")
		require.NoError(t, local.Prepare(work, ds, candidate, manifest, [][]float32{{1, 0, 0}}))
		var visibleOld int64
		require.NoError(t, tx.Model(&types.Chunk{}).Where("id=? AND is_enabled", m.Chunks[0].ID).
			Count(&visibleOld).Error)
		require.EqualValues(t, 1, visibleOld, "staging must preserve the serving version")
		require.NoError(t, tx.Exec(`CREATE FUNCTION reparse_completion_fault() RETURNS trigger
 LANGUAGE plpgsql AS $$ BEGIN IF NEW.scope='feishu_document_reparse_v1' AND NEW.op='completed'
 THEN RAISE EXCEPTION 'synthetic completion failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER reparse_completion_fault BEFORE UPDATE ON task_pending_ops
 FOR EACH ROW EXECUTE FUNCTION reparse_completion_fault()`).Error)
		_, err = local.Publish(work, ds, candidate)
		require.Error(t, err)
		status, err := local.SourceDocumentReparseStatus(ctx, ds, kid)
		require.NoError(t, err)
		require.Equal(t, "processing", status.State)
		version := strconv.FormatInt(item.UpdatedAt.Unix(), 10)
		confirmed, err := local.ConfirmSourceReparsePublication(ctx, ds, "root", item.ExternalID, version)
		require.NoError(t, err)
		require.False(t, confirmed, "a failed publication cannot acknowledge an exhausted document")
		var stillCurrent types.FeishuLegacyResource
		require.NoError(t, tx.First(&stillCurrent, "id=?", resource.ID).Error)
		require.Equal(
			t,
			a.ID,
			stillCurrent.ActiveAttempt,
			"request ACK failure must roll publication back",
		)
		require.NoError(t, tx.Exec(`DROP TRIGGER reparse_completion_fault ON task_pending_ops;
 DROP FUNCTION reparse_completion_fault()`).Error)
		published, err := local.Publish(work, ds, candidate)
		require.NoError(t, err)
		require.Equal(t, candidate.ID, published.AttemptID)
		status, err = local.SourceDocumentReparseStatus(ctx, ds, kid)
		require.NoError(t, err)
		require.Equal(t, "completed", status.State)
		confirmed, err = local.ConfirmSourceReparsePublication(ctx, ds, "root", item.ExternalID, version)
		require.NoError(t, err)
		require.True(t, confirmed, "the current completed publication may be acknowledged by normal sync")
		confirmed, err = local.ConfirmSourceReparsePublication(ctx, ds, "approved-overlap", item.ExternalID, version)
		require.NoError(t, err)
		require.True(t, confirmed, "overlapping roots within the same unchanged selection share one publication")
		for _, target := range [][2]string{{"other-root", version}, {"root", version + "0"}} {
			confirmed, err = local.ConfirmSourceReparsePublication(ctx, ds, target[0], item.ExternalID, target[1])
			require.NoError(t, err)
			require.False(t, confirmed, "a different root or source version must keep its failure record")
		}
		_, err = local.ConfirmSourceReparsePublication(context.Background(), ds, "root", item.ExternalID, version)
		require.Error(t, err, "a completed request is not execution authority")
		var completedOp types.TaskPendingOp
		require.NoError(t, tx.Where("scope=? AND scope_id=? AND dedup_key=? AND op='completed'",
			sourceDocumentReparseScope, ds.ID, kid).First(&completedOp).Error)
		for _, field := range []string{"config_hash", "kb_config_hash", "published_attempt", "external_id"} {
			var edited map[string]any
			require.NoError(t, json.Unmarshal(completedOp.Payload, &edited))
			edited[field] = "mismatched"
			payload, err := json.Marshal(edited)
			require.NoError(t, err)
			require.NoError(t, tx.Model(&types.TaskPendingOp{}).Where("id=?", completedOp.ID).
				UpdateColumn("payload", json.RawMessage(payload)).Error)
			confirmed, err = local.ConfirmSourceReparsePublication(ctx, ds, "root", item.ExternalID, version)
			require.NoError(t, err)
			require.False(t, confirmed, field)
		}
		require.NoError(t, tx.Model(&types.TaskPendingOp{}).Where("id=?", completedOp.ID).
			UpdateColumn("payload", completedOp.Payload).Error)
		require.NoError(t, tx.Model(&types.FeishuLegacyResource{}).Where("id=?", resource.ID).
			Updates(map[string]any{"current_attempt": a.ID, "active_attempt": a.ID}).Error)
		confirmed, err = local.ConfirmSourceReparsePublication(ctx, ds, "root", item.ExternalID, version)
		require.NoError(t, err)
		require.False(t, confirmed, "an older publication cannot satisfy the completed request receipt")
		require.NoError(t, tx.Model(&types.FeishuLegacyResource{}).Where("id=?", resource.ID).
			Updates(map[string]any{"current_attempt": candidate.ID, "active_attempt": candidate.ID}).Error)
		catalog, err := local.SourceDocumentReparseStatuses(ctx, ds.TenantID, []string{kid})
		require.NoError(t, err)
		require.Len(t, catalog, 1)
		require.Equal(t, "completed", catalog[0].State)
		projection, err := json.Marshal(catalog)
		require.NoError(t, err)
		require.NotContains(t, string(projection), "process_config")
		require.NotContains(t, string(projection), request.Initiator.UserID)
		otherTenant, err := local.SourceDocumentReparseStatuses(ctx, ds.TenantID+1, []string{kid})
		require.NoError(t, err)
		require.Empty(t, otherTenant)
		_, _, err = local.Begin(work, ds, item, hex.EncodeToString(hash[:]))
		require.Error(t, err, "a completed request cannot be replayed to force another candidate")
	})
	t.Run("source-document-reparse-capacity-preserves-budget-and-checkpoint", func(t *testing.T) {
		tx := db.Begin()
		require.NoError(t, tx.Error)
		defer tx.Rollback()
		local := NewFeishuLegacyStore(tx)
		request, err := local.SourceReparseTarget(ctx, ds, kid)
		require.NoError(t, err)
		request.Initiator = types.TaskInitiator{UserID: uuid.NewString(), Role: types.TenantRoleAdmin}
		request.KBConfigHash = LegacyKBConfigHash(kb)
		request.ProgressWindows, request.CompletedChunks, request.TotalChunks = 4, 5100, 35443
		_, err = local.QueueSourceDocumentReparse(ctx, ds, request)
		require.NoError(t, err)
		originalID := request.ID
		for generation := 1; generation <= 4; generation++ {
			require.NoError(t, tx.Model(&types.SyncLog{}).Where("id=?", log.ID).
				UpdateColumn("execution_generation", generation).Error)
			run := types.WithSyncExecution(context.Background(), types.SyncExecution{
				DataSourceID: ds.ID, SyncLogID: log.ID, TenantID: tenant.ID, Generation: int64(generation),
			})
			work, claimed, err := local.ClaimSourceDocumentReparse(run, ds)
			require.NoError(t, err)
			require.NotNil(t, claimed)
			require.Equal(t, 1, claimed.Attempts)
			require.NoError(t, local.FinishSourceDocumentReparse(
				work, ds, "safe_staging_capacity_insufficient", true, false))
			var op types.TaskPendingOp
			require.NoError(t, tx.Where("scope=? AND scope_id=? AND dedup_key=?",
				sourceDocumentReparseScope, ds.ID, kid).First(&op).Error)
			var persisted types.FeishuDocumentReparse
			require.NoError(t, json.Unmarshal(op.Payload, &persisted))
			require.Equal(t, "queued", op.Op)
			require.Zero(t, persisted.Attempts)
			require.Equal(t, originalID, persisted.ID)
			require.Equal(t, request.ProgressWindows, persisted.ProgressWindows)
			require.Equal(t, request.CompletedChunks, persisted.CompletedChunks)
			require.Equal(t, request.TotalChunks, persisted.TotalChunks)
			require.Equal(t, request.ConfigHash, persisted.ConfigHash)
			require.WithinDuration(t, time.Now().Add(20*time.Minute), persisted.RetryAfter, 10*time.Second)
			_, pending, err := local.ClaimSourceDocumentReparse(run, ds)
			require.NoError(t, err)
			require.Nil(t, pending, "capacity waiting must not spin")
			persisted.RetryAfter = time.Now().Add(-time.Second)
			payload, err := json.Marshal(persisted)
			require.NoError(t, err)
			require.NoError(t, tx.Model(&op).UpdateColumn("payload", json.RawMessage(payload)).Error)
		}
		var oldChunks int64
		require.NoError(t, tx.Model(&types.Chunk{}).
			Where("id=? AND is_enabled", m.Chunks[0].ID).Count(&oldChunks).Error)
		require.EqualValues(t, 1, oldChunks, "capacity backpressure must not rewrite serving output")
	})
	t.Run("source-document-reparse-retry-budget-survives-new-executions", func(t *testing.T) {
		tx := db.Begin()
		require.NoError(t, tx.Error)
		defer tx.Rollback()
		local := NewFeishuLegacyStore(tx)
		request, err := local.SourceReparseTarget(ctx, ds, kid)
		require.NoError(t, err)
		request.Initiator = types.TaskInitiator{
			UserID: uuid.NewString(),
			Role:   types.TenantRoleAdmin,
		}
		request.KBConfigHash = LegacyKBConfigHash(kb)
		_, err = local.QueueSourceDocumentReparse(ctx, ds, request)
		require.NoError(t, err)
		for attempt := 1; attempt <= 3; attempt++ {
			require.NoError(t, tx.Model(&types.SyncLog{}).Where("id=?", log.ID).
				UpdateColumn("execution_generation", attempt).Error)
			generation := types.WithSyncExecution(context.Background(), types.SyncExecution{
				DataSourceID: ds.ID, SyncLogID: log.ID, TenantID: tenant.ID, Generation: int64(attempt),
			})
			work, accepted, err := local.ClaimSourceDocumentReparse(generation, ds)
			require.NoError(t, err)
			require.NotNil(t, accepted)
			require.Equal(t, attempt, accepted.Attempts)
			_, duplicate, err := local.ClaimSourceDocumentReparse(generation, ds)
			require.NoError(t, err)
			require.Nil(
				t,
				duplicate,
				"one execution cannot claim an already processing request twice",
			)
			failure := WithSourceDocumentReparseFailure(work, reparseSyntheticSQLFailure{state: "23505"}, false)
			if attempt == 1 {
				stale := types.WithSyncExecution(failure, types.SyncExecution{
					DataSourceID: ds.ID, SyncLogID: log.ID, TenantID: tenant.ID, Generation: 999,
				})
				require.Error(t, local.FinishSourceDocumentReparse(stale, ds, "temporary_failure", true, false))
				require.NoError(t, tx.Exec(`CREATE FUNCTION reparse_failure_fault() RETURNS trigger
 LANGUAGE plpgsql AS $$ BEGIN IF NEW.scope='feishu_document_reparse_v1' AND NEW.op='queued'
 THEN RAISE EXCEPTION 'synthetic failure ACK fault'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER reparse_failure_fault BEFORE UPDATE ON task_pending_ops
 FOR EACH ROW EXECUTE FUNCTION reparse_failure_fault()`).Error)
				require.Error(t, local.FinishSourceDocumentReparse(failure, ds, "temporary_failure", true, false))
				var count int64
				require.NoError(t, tx.Model(&types.TaskPendingOp{}).
					Where("scope=? AND scope_id=?", sourceReparseFailureScope, request.ID).Count(&count).Error)
				require.Zero(t, count, "failed authority or rolled back ACK cannot leave an audit receipt")
				require.NoError(t, tx.Exec(`DROP TRIGGER reparse_failure_fault ON task_pending_ops;
 DROP FUNCTION reparse_failure_fault()`).Error)
				status, err := local.SourceDocumentReparseStatus(generation, ds, kid)
				require.NoError(t, err)
				require.Equal(t, "processing", status.State)
			}
			require.NoError(
				t,
				local.FinishSourceDocumentReparse(failure, ds, "temporary_failure", true, false),
			)
			var receipts []types.TaskPendingOp
			require.NoError(t, tx.Where("scope=? AND scope_id=?", sourceReparseFailureScope, request.ID).
				Order("id").Find(&receipts).Error)
			require.Len(t, receipts, attempt)
			for _, receipt := range receipts {
				require.Equal(t, "recorded", receipt.Op)
				require.Contains(t, string(receipt.Payload), "23505")
				require.Contains(t, string(receipt.Payload), "temporary_failure")
				require.NotContains(t, string(receipt.Payload), "SYNTHETIC_SECRET")
			}
			status, err := local.SourceDocumentReparseStatus(generation, ds, kid)
			require.NoError(t, err)
			if attempt < 3 {
				require.Equal(t, "queued", status.State)
				var op types.TaskPendingOp
				require.NoError(t, tx.Where("scope=? AND scope_id=? AND dedup_key=?",
					sourceDocumentReparseScope, ds.ID, kid).First(&op).Error)
				var persisted types.FeishuDocumentReparse
				require.NoError(t, json.Unmarshal(op.Payload, &persisted))
				require.NoError(t, local.DeferSourceDocumentReparse(generation, &persisted))
				require.NoError(t, tx.First(&op, "id=?", op.ID).Error)
				require.Contains(t, string(op.Payload), "queue_delivery_pending")
				require.NotContains(t, string(op.Payload), "fingerprint")
				require.NoError(t, json.Unmarshal(op.Payload, &persisted))
				persisted.RetryAfter = time.Now().Add(-time.Second)
				payload, err := json.Marshal(persisted)
				require.NoError(t, err)
				require.NoError(
					t,
					tx.Model(&op).UpdateColumn("payload", json.RawMessage(payload)).Error,
				)
			} else {
				require.Equal(t, "failed", status.State)
			}
		}
		pending, err := local.PendingSourceDocumentReparses(ctx)
		require.NoError(t, err)
		require.Empty(t, pending, "a later recovery sweep must not reset exhausted parser attempts")
		var oldChunks int64
		require.NoError(t, tx.Model(&types.Chunk{}).
			Where("id=? AND is_enabled", m.Chunks[0].ID).Count(&oldChunks).Error)
		require.EqualValues(t, 1, oldChunks)
	})
	t.Run(
		"permission-refusal-quarantines-one-document-without-stopping-source",
		func(t *testing.T) {
			tx := db.Begin()
			require.NoError(t, tx.Error)
			defer tx.Rollback()
			local := NewFeishuLegacyStore(tx)
			guardedExecution := types.WithReconciliationCommitGuard(
				ctx,
				func(check *gorm.DB) error {
					var current types.DataSource
					if err := check.First(&current, "id=?", ds.ID).Error; err != nil {
						return err
					}
					if current.LegacyAccessState != ds.LegacyAccessState {
						return errors.New(
							"signed source identity changed during document quarantine",
						)
					}
					return nil
				},
			)
			require.NoError(t, local.RestrictDocument(guardedExecution, ds, "same-cloud-id"))
			var source types.DataSource
			require.NoError(t, tx.First(&source, "id=?", ds.ID).Error)
			require.Equal(t, ds.LegacyAccessState, source.LegacyAccessState)
			var quarantined types.FeishuLegacyResource
			require.NoError(t, tx.First(&quarantined, "id=?", r.ID).Error)
			require.Equal(t, "hold", quarantined.State)
			require.Greater(t, len(quarantined.QuarantineSnapshot), 2)
			guarded := types.EnableLegacyReadGuards(tx)
			require.ErrorIs(
				t,
				types.CheckLegacySourceRead(guarded.WithContext(context.Background()), "", kb.ID),
				types.ErrLegacySourceRestricted,
				"old derived content stays closed until safe regeneration",
			)
		},
	)
	t.Run("document-deny-survives-cleanup-guard-failure", func(t *testing.T) {
		tx := db.Begin()
		require.NoError(t, tx.Error)
		defer tx.Rollback()
		local := NewFeishuLegacyStore(tx)
		denied := types.WithReconciliationCommitGuard(ctx, func(*gorm.DB) error {
			return errors.New("current publication authority unavailable")
		})
		require.Error(t, local.RestrictDocument(denied, ds, "same-cloud-id"))
		var source types.DataSource
		require.NoError(t, tx.First(&source, "id=?", ds.ID).Error)
		require.Equal(t, ds.LegacyAccessState, source.LegacyAccessState)
		var quarantined types.FeishuLegacyResource
		require.NoError(t, tx.First(&quarantined, "id=?", r.ID).Error)
		require.Equal(t, "hold", quarantined.State)
		guarded := types.EnableLegacyReadGuards(tx)
		require.ErrorIs(
			t,
			types.CheckLegacySourceRead(guarded.WithContext(context.Background()), "", kb.ID),
			types.ErrLegacySourceRestricted,
		)
	})
	t.Run("document-failure-retains-only-a-verified-active-version", func(t *testing.T) {
		item := &types.FetchedItem{
			ExternalID: "same-cloud-id", SourceResourceID: "selected-root", Content: []byte("failed-new-candidate"),
			UpdatedAt: time.Now().UTC(), Metadata: map[string]string{},
		}
		retained, e := store.RetainVerifiedVersion(ctx, ds, item)
		require.NoError(t, e)
		require.Equal(t, receipt.AttemptID, retained.AttemptID)
		require.Equal(t, receipt.ManifestSHA256, retained.ManifestSHA256)
		require.EqualValues(t, 1, visible("oldcanary"))
		require.Zero(t, visible("failed-new-candidate"))
		for _, category := range []string{"permission", "auth", "unknown"} {
			candidate := *item
			candidate.Content = nil
			candidate.Metadata = map[string]string{"error": category}
			_, e := store.RetainVerifiedVersion(ctx, ds, &candidate)
			require.Error(t, e)
		}
		_, e = store.RetainVerifiedVersion(context.Background(), ds, item)
		require.Error(t, e, "an unclaimed retry cannot assert current access")
		denied := types.WithFeishuSourceReadGuard(ctx, func(context.Context) error {
			return errors.New("current resource permission withdrawn")
		})
		_, e = store.RetainVerifiedVersion(denied, ds, item)
		require.Error(t, e, "a retained historical receipt never overrides current permission")
		candidate := *item
		candidate.ExternalID = "not-imported"
		_, e = store.RetainVerifiedVersion(ctx, ds, &candidate)
		require.Error(t, e, "historical unknown versions must not receive a receipt")
		empty, e := store.UnpublishedResource(ctx, ds, "not-imported")
		require.NoError(t, e)
		require.True(t, empty)
		empty, e = store.UnpublishedResource(ctx, ds, item.ExternalID)
		require.NoError(t, e)
		require.False(t, empty, "previously imported data still needs explicit quarantine")
		_, e = store.UnpublishedResource(context.Background(), ds, "not-imported")
		require.Error(t, e)
	})
	for _, window := range []string{"prepare", "publish"} {
		t.Run("process-config-edit-before-"+window, func(t *testing.T) {
			metadata, e := json.Marshal(
				map[string]string{"datasource_id": ds.ID, "external_id": "config-" + window},
			)
			require.NoError(t, e)
			old := types.Knowledge{
				ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID,
				Type: "file", Channel: "feishu", Metadata: types.JSON(metadata),
				FileHash: "config-before", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled",
			}
			no, yes := false, true
			require.NoError(
				t,
				old.SetProcessOverrides(&types.KnowledgeProcessOverrides{SummaryEnabled: &no}),
			)
			require.NoError(t, db.Create(&old).Error)
			chunk := &types.Chunk{
				ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID,
				KnowledgeID: old.ID, ChunkType: types.ChunkTypeText, Content: "configbeforecanary", IsEnabled: true,
			}
			require.NoError(t, db.Create(chunk).Error)
			require.NoError(
				t,
				db.Exec(`INSERT INTO embeddings(source_id,source_type,chunk_id,knowledge_id,
				knowledge_base_id,content,dimension,embedding,is_enabled)
				VALUES(?,0,?,?,?,'configbeforecanary',3,'[1,0,0]'::halfvec,true)`,
					chunk.ID, chunk.ID, old.ID, kb.ID).Error,
			)
			candidateHash := sha256.Sum256([]byte("config-next"))
			_, attempt, e := store.Begin(ctx, ds, &types.FetchedItem{
				ExternalID: "config-" + window, UpdatedAt: time.Date(2026, 10, 21, 1, 1, 0, 0, time.UTC),
			}, hex.EncodeToString(candidateHash[:]))
			require.NoError(t, e)
			candidate := old
			candidate.FileHash = "config-next"
			metadata, e = json.Marshal(
				old.GetMetadata(),
			) // Reproduce the staging adapter's string projection.
			require.NoError(t, e)
			candidate.Metadata = types.JSON(metadata)
			manifest := types.FeishuLegacyManifest{
				Knowledge: candidate, Chunks: []*types.Chunk{{
					ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID,
					KnowledgeID: old.ID, ChunkType: types.ChunkTypeText, Content: "confignextcanary",
					IsEnabled: true,
				}},
				EmbeddingModelID: kb.EmbeddingModelID,
				KBConfigSHA256:   LegacyKBConfigHash(kb),
				ModelStamp:       stamp,
				Dimension:        3,
			}
			if window == "publish" {
				require.NoError(
					t,
					store.Prepare(ctx, ds, attempt, manifest, [][]float32{{1, 0, 0}}),
				)
			}
			require.NoError(
				t,
				old.SetProcessOverrides(&types.KnowledgeProcessOverrides{SummaryEnabled: &yes}),
			)
			// An ordinary editor is already denied by the database's source
			// ownership fence. Inject a competing write with this live owner's
			// nonce to exercise the additional configuration comparison without
			// disabling any trigger or changing the source generation.
			require.Error(
				t,
				db.Model(&types.Knowledge{}).
					Where("id=?", old.ID).
					Update("metadata", old.Metadata).
					Error,
			)
			require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
				if e := tx.Exec(`SELECT set_config('knowhub.feishu_attempt',?,true),
					set_config('knowhub.feishu_nonce',?,true)`, attempt.ID, attempt.OwnerNonce).Error; e != nil {
					return e
				}
				return tx.Model(&types.Knowledge{}).
					Where("id=?", old.ID).
					Update("metadata", old.Metadata).
					Error
			}))
			snapshot := func() string {
				var state string
				require.NoError(t, db.Raw(`SELECT jsonb_build_object('knowledge',
					(SELECT to_jsonb(k) FROM knowledges k WHERE id=?),'chunks',
					(SELECT jsonb_agg(to_jsonb(c) ORDER BY c.id) FROM chunks c WHERE knowledge_id=?),'embeddings',
					(SELECT jsonb_agg(to_jsonb(v) ORDER BY to_jsonb(v)::text)
					 FROM embeddings v WHERE knowledge_id=?))::text`,
					old.ID, old.ID, old.ID).Scan(&state).Error)
				return state
			}
			before := snapshot()
			if window == "prepare" {
				e = store.Prepare(ctx, ds, attempt, manifest, [][]float32{{1, 0, 0}})
			} else {
				_, e = store.Publish(ctx, ds, attempt)
			}
			require.ErrorContains(t, e, "knowledge_process_config_changed")
			require.JSONEq(
				t,
				before,
				snapshot(),
				"rejected configuration race preserves the current published version",
			)
		})
	}
	t.Run("P1-same-high-water-timestamp-change-rejected", func(t *testing.T) {
		// Reproduction: same bytes at a later source time reuse the published
		// attempt, leaving its original Revision behind the resource high-water.
		later := time.Date(2026, 10, 21, 1, 1, 30, 0, time.UTC)
		h := sha256.Sum256([]byte("oldcanary"))
		_, duplicate, e := store.Begin(
			ctx,
			ds,
			&types.FetchedItem{ExternalID: "same-cloud-id", UpdatedAt: later},
			hex.EncodeToString(h[:]),
		)
		require.NoError(t, e)
		require.Equal(t, a.ID, duplicate.ID)
		changed := sha256.Sum256([]byte("ambiguous changed bytes"))
		_, _, e = store.Begin(
			ctx,
			ds,
			&types.FetchedItem{ExternalID: "same-cloud-id", UpdatedAt: later},
			hex.EncodeToString(changed[:]),
		)
		require.ErrorIs(t, e, ErrLegacyVersionFence)
		var diagnostic *LegacyFenceError
		require.ErrorAs(t, e, &diagnostic)
		require.Equal(t, "source_revision_content_conflict", diagnostic.Reason)
		require.EqualValues(t, 1, visible("oldcanary"))
	})
	t.Run("P1-active-duplicate-preserves-live-owner", func(t *testing.T) {
		candidateTime := time.Date(2026, 10, 21, 1, 1, 45, 0, time.UTC)
		candidateHash := sha256.Sum256([]byte("live candidate"))
		_, live, e := store.Begin(
			ctx,
			ds,
			&types.FetchedItem{ExternalID: "same-cloud-id", UpdatedAt: candidateTime},
			hex.EncodeToString(candidateHash[:]),
		)
		require.NoError(t, e)
		defer func() { require.NoError(t, store.Abandon(ctx, live)) }()
		oldHash := sha256.Sum256([]byte("oldcanary"))
		_, _, e = store.Begin(
			ctx,
			ds,
			&types.FetchedItem{
				ExternalID: "same-cloud-id",
				UpdatedAt:  candidateTime.Add(5 * time.Second),
			},
			hex.EncodeToString(oldHash[:]),
		)
		require.ErrorIs(
			t,
			e,
			ErrLegacyVersionBusy,
			"duplicate active bytes cannot overtake a live candidate",
		)
		var watermark string
		require.NoError(
			t,
			db.Raw("SELECT source_revision FROM feishu_legacy_resources WHERE id=?", r.ID).
				Scan(&watermark).
				Error,
		)
		require.Equal(t, candidateTime.Format(time.RFC3339Nano), watermark)
	})
	t.Run("legacy-worker-mutations-denied", func(t *testing.T) {
		for _, sql := range []string{
			"DELETE FROM embeddings WHERE knowledge_id=?",
			"UPDATE chunks SET content='stale' WHERE knowledge_id=?",
			"UPDATE knowledges SET title='stale' WHERE id=?",
		} {
			require.Error(t, db.Exec(sql, kid).Error)
		}
		require.EqualValues(t, 1, visible("oldcanary"))
	})
	t.Run("failed-candidate-preserves-current", func(t *testing.T) {
		_, next, candidate := makeVersion("invalidvector", 2)
		require.Error(t, store.Prepare(ctx, ds, next, candidate, [][]float32{{1, 0}}))
		require.EqualValues(t, 1, visible("oldcanary"))
		require.NoError(t, store.Abandon(ctx, next))
		_, retry, retriedCandidate := makeVersion("invalidvector", 2)
		require.NoError(
			t,
			store.Prepare(ctx, ds, retry, retriedCandidate, [][]float32{{1, 0, 0}}),
			"same-high-water same-content failed attempt must remain retryable",
		)
		require.NoError(t, store.Abandon(ctx, retry))
	})
	t.Run("partial-public-index-write-rolls-back", func(t *testing.T) {
		_, next, candidate := makeVersion("faultcanary", 3)
		require.NoError(t, store.Prepare(ctx, ds, next, candidate, [][]float32{{0, 1, 0}}))
		require.NoError(
			t,
			db.Exec(
				`CREATE FUNCTION feishu_test_fail_insert() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
IF NEW.content LIKE '%faultcanary%' THEN
RAISE EXCEPTION 'injected index failure';
END IF;
RETURN NEW;
END $$;
CREATE TRIGGER feishu_test_fail_insert BEFORE INSERT ON embeddings
FOR EACH ROW EXECUTE FUNCTION feishu_test_fail_insert();`,
			).Error,
		)
		_, e := store.Publish(ctx, ds, next)
		require.Error(t, e)
		require.EqualValues(t, 1, visible("oldcanary"))
		require.Zero(t, visible("faultcanary"))
		var active types.FeishuLegacyResource
		require.NoError(t, db.First(&active, "id=?", r.ID).Error)
		require.Equal(t, a.ID, active.ActiveAttempt)
		require.NoError(
			t,
			db.Exec(
				"DROP TRIGGER feishu_test_fail_insert ON embeddings; DROP FUNCTION "+
					"feishu_test_fail_insert()",
			).Error,
		)
		require.NoError(t, store.Abandon(ctx, next))
	})
	t.Run("atomic-read-and-delayed-cleanup", func(t *testing.T) {
		_, next, candidate := makeVersion("newcanary", 4)
		require.NoError(t, store.Prepare(ctx, ds, next, candidate, [][]float32{{0, 1, 0}}))
		// A reader begun before publication keeps the complete old MVCC view.
		sqlDB, e := db.DB()
		require.NoError(t, e)
		reader, e := sqlDB.BeginTx(ctx, nil)
		require.NoError(t, e)
		_, e = reader.Exec("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ")
		require.NoError(t, e)
		var body string
		require.NoError(
			t,
			reader.QueryRow("SELECT content FROM chunks WHERE knowledge_id=$1 AND deleted_at IS NULL", kid).
				Scan(&body),
		)
		require.Equal(t, "oldcanary", body)
		_, e = store.Publish(ctx, ds, next)
		require.NoError(t, e)
		require.NoError(
			t,
			reader.QueryRow("SELECT content FROM embeddings WHERE knowledge_id=$1", kid).
				Scan(&body),
		)
		require.True(t, strings.Contains(body, "oldcanary"))
		require.NoError(t, reader.Rollback())
		require.Zero(t, visible("oldcanary"))
		require.EqualValues(t, 1, visible("newcanary"))
		require.Error(
			t,
			db.Exec("DELETE FROM embeddings WHERE knowledge_id=?", kid).Error,
			"old worker cleanup after its earlier check must be rejected",
		)
		_, e = store.Publish(ctx, ds, a)
		require.Error(t, e, "superseded attempt cannot republish")
		_, e = store.Receipt(ctx, r, next)
		require.NoError(t, e)
	})
	t.Run("enrollment-drains-uncommitted-old-worker", func(t *testing.T) {
		other := &types.DataSource{
			ID:              uuid.NewString(),
			TenantID:        tenant.ID,
			KnowledgeBaseID: kb.ID,
			Name:            "old worker",
			Type:            "feishu_drive",
			Status:          "active",
			Config:          types.JSON(`{}`),
		}
		require.NoError(t, db.Create(other).Error)
		log := &types.SyncLog{
			ID:                  uuid.NewString(),
			DataSourceID:        other.ID,
			TenantID:            tenant.ID,
			Status:              types.SyncLogStatusRunning,
			ExecutionGeneration: 1,
			ExecutionClaimed:    true,
		}
		require.NoError(t, db.Create(log).Error)
		otherCtx := types.WithSyncExecution(
			context.Background(),
			types.SyncExecution{
				DataSourceID: other.ID,
				SyncLogID:    log.ID,
				TenantID:     tenant.ID,
				Generation:   1,
			},
		)
		oldTx := db.Begin()
		require.NoError(t, oldTx.Error)
		defer oldTx.Rollback()
		meta, _ := json.Marshal(
			map[string]string{"datasource_id": other.ID, "external_id": "inflight-cloud"},
		)
		inflightKID := uuid.NewString()
		require.NoError(
			t,
			oldTx.Create(
				&types.Knowledge{
					ID:              inflightKID,
					TenantID:        tenant.ID,
					KnowledgeBaseID: kb.ID,
					Channel:         "feishu",
					Type:            "file",
					Title:           "old worker",
					Metadata:        types.JSON(meta),
					FileHash:        "inflight",
					ParseStatus:     types.ParseStatusCompleted,
					EnableStatus:    "enabled",
				},
			).Error,
		)
		item := &types.FetchedItem{
			ExternalID: "inflight-cloud",
			Content:    []byte("new version"),
			UpdatedAt:  time.Now().UTC(),
		}
		hash := sha256.Sum256(item.Content)
		type result struct {
			resource *types.FeishuLegacyResource
			err      error
		}
		done := make(chan result, 1)
		go func() {
			r, _, e := store.Begin(otherCtx, other, item, hex.EncodeToString(hash[:]))
			done <- result{r, e}
		}()
		require.Eventually(t, func() bool {
			var waiting int64
			_ = db.Raw("SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND NOT granted").
				Scan(&waiting).
				Error
			return waiting > 0
		}, 3*time.Second, 10*time.Millisecond)
		select {
		case <-done:
			t.Fatal("enrollment passed an uncommitted old writer")
		default:
		}
		require.NoError(t, oldTx.Commit().Error)
		select {
		case result := <-done:
			require.NoError(t, result.err)
			require.Equal(t, inflightKID, result.resource.KnowledgeID)
		case <-time.After(5 * time.Second):
			t.Fatal("enrollment failed to resume after old writer commit")
		}
	})
	t.Run("commit-ack-loss-uses-durable-receipt", func(t *testing.T) {
		resource, next, candidate := makeVersion("acklostcanary", 5)
		require.NoError(t, store.Prepare(ctx, ds, next, candidate, [][]float32{{1, 0, 0}}))
		pool, e := db.DB()
		require.NoError(t, e)
		armed := &atomic.Bool{}
		armed.Store(true)
		faultDB := db.Session(&gorm.Session{NewDB: true})
		faultPool := &legacyAckLossPool{DB: pool, armed: armed}
		faultDB.Statement = &gorm.Statement{DB: faultDB, Context: ctx, ConnPool: faultPool}
		_, e = NewFeishuLegacyStore(faultDB).Publish(ctx, ds, next)
		require.ErrorContains(t, e, "acknowledgement loss")
		require.EqualValues(t, 1, visible("acklostcanary"))
		_, e = store.Receipt(ctx, resource, next)
		require.NoError(t, e, "independent connection must reconcile actual committed state")
		_, e = store.Publish(ctx, ds, next)
		require.Error(t, e, "do not run destructive publication a second time after unknown commit")
	})
	t.Run("stale-publication-cannot-confirm-sync-cursor", func(t *testing.T) {
		checkpointCtx := types.WithLegacyReceiptSet(ctx)
		types.AddLegacyReceipt(checkpointCtx, *receipt)
		ds.LastSyncCursor = types.JSON(`{"must_not_persist":true}`)
		_, e := NewSyncLogRepository(db).(*SyncLogRepository).WriteExecution(
			checkpointCtx,
			types.SyncExecution{
				DataSourceID: ds.ID,
				SyncLogID:    log.ID,
				TenantID:     tenant.ID,
				Generation:   1,
			},
			ds,
			log,
			false,
		)
		require.ErrorIs(t, e, ErrLegacyVersionFence)
		var saved types.DataSource
		require.NoError(t, db.First(&saved, "id=?", ds.ID).Error)
		require.NotContains(t, string(saved.LastSyncCursor), "must_not_persist")
	})
	t.Run("expired-owner-cannot-write-after-takeover", func(t *testing.T) {
		_, stale, candidate := makeVersion("expiredcandidate", 6)
		require.NoError(t, store.Prepare(ctx, ds, stale, candidate, [][]float32{{1, 0, 0}}))
		require.NoError(
			t,
			db.Model(&types.FeishuLegacyAttempt{}).
				Where("id=?", stale.ID).
				Update("lease_until", time.Now().Add(-time.Minute)).
				Error,
		)
		_, current, next := makeVersion("takeovercanary", 7)
		require.Error(t, store.CheckOwner(ctx, ds, stale))
		require.Error(t, store.Prepare(ctx, ds, stale, candidate, [][]float32{{1, 0, 0}}))
		_, e := store.Publish(ctx, ds, stale)
		require.Error(t, e)
		require.NoError(t, store.Prepare(ctx, ds, current, next, [][]float32{{1, 0, 0}}))
		_, e = store.Publish(ctx, ds, current)
		require.NoError(t, e)
		require.NoError(
			t,
			store.Abandon(ctx, stale),
			"stale cleanup must not mutate current publication",
		)
		require.EqualValues(t, 1, visible("takeovercanary"))
	})
	t.Run("model-change-blocks-staged-publication", func(t *testing.T) {
		_, attempt, candidate := makeVersion("wrongmodelcanary", 8)
		require.NoError(t, store.Prepare(ctx, ds, attempt, candidate, [][]float32{{1, 0, 0}}))
		require.NoError(
			t,
			db.Model(&types.Model{}).
				Where("id=?", model.ID).
				UpdateColumn("name", "different-binding").
				Error,
		)
		_, e := store.Publish(ctx, ds, attempt)
		require.ErrorIs(t, e, ErrLegacyVersionFence)
		require.EqualValues(t, 1, visible("takeovercanary"))
		require.Zero(t, visible("wrongmodelcanary"))
		require.NoError(
			t,
			db.Model(&types.Model{}).Where("id=?", model.ID).UpdateColumn("name", model.Name).Error,
		)
		require.NoError(t, store.Abandon(ctx, attempt))
	})
	t.Run("quota-failure-preserves-current-publication", func(t *testing.T) {
		_, attempt, candidate := makeVersion("muchlongerquotafailurecanarymustnotpublish", 9)
		require.NoError(t, store.Prepare(ctx, ds, attempt, candidate, [][]float32{{1, 0, 0}}))
		var before types.Tenant
		require.NoError(t, db.First(&before, "id=?", tenant.ID).Error)
		require.NoError(
			t,
			db.Model(&types.Tenant{}).
				Where("id=?", tenant.ID).
				UpdateColumn("storage_quota", 1).
				Error,
		)
		_, e := store.Publish(ctx, ds, attempt)
		require.ErrorContains(t, e, "quota")
		require.EqualValues(t, 1, visible("takeovercanary"))
		require.Zero(t, visible("muchlongerquotafailurecanarymustnotpublish"))
		var after types.Tenant
		require.NoError(t, db.First(&after, "id=?", tenant.ID).Error)
		require.Equal(
			t,
			before.StorageUsed,
			after.StorageUsed,
			"failed publication must not alter quota accounting",
		)
		require.NoError(
			t,
			db.Model(&types.Tenant{}).
				Where("id=?", tenant.ID).
				UpdateColumn("storage_quota", before.StorageQuota).
				Error,
		)
		require.NoError(t, store.Abandon(ctx, attempt))
	})
	t.Run("persistent-uncertainty-closes-direct-history-model-and-file-reads", func(t *testing.T) {
		guardDB := types.EnableLegacyReadGuards(db.Session(&gorm.Session{NewDB: true}))
		requestScope := types.WithLegacyReadScope(context.Background())
		engine := pgretrieve.NewPostgresRetrieveEngineRepository(guardDB)
		search := func(empty bool) {
			for _, kind := range []types.RetrieverType{types.KeywordsRetrieverType, types.VectorRetrieverType} {
				rows, e := engine.Retrieve(
					requestScope,
					types.RetrieveParams{
						Query:            "takeovercanary",
						Embedding:        []float32{1, 0, 0},
						KnowledgeBaseIDs: []string{kb.ID},
						KnowledgeIDs:     []string{kid},
						TopK:             10,
						Threshold:        0,
						RetrieverType:    kind,
					},
				)
				require.NoError(t, e)
				count := 0
				for _, row := range rows {
					require.NoError(t, row.Error)
					count += len(row.Results)
				}
				if empty {
					require.Zero(t, count, "held KB must be excluded inside real candidate query")
				} else {
					require.Positive(t, count)
				}
			}
		}
		search(false)
		require.NoError(t, store.SetSourceUncertain(ctx, ds, true))
		search(false)
		require.NoError(
			t,
			types.CheckLegacySourceRead(guardDB, kid, kb.ID),
			"a generic fetch failure does not revoke an existing document",
		)
		var retryableSource types.DataSource
		require.NoError(t, db.First(&retryableSource, "id=?", ds.ID).Error)
		require.NotEqual(t, "hold", retryableSource.LegacyAccessState)

		require.NoError(t, store.SetSourceUncertain(ctx, ds, false))
		require.NoError(t, types.CheckLegacySourceRead(guardDB, kid, kb.ID))
		require.NoError(
			t,
			db.Exec(
				`CREATE FUNCTION feishu_test_redaction_fault() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
IF NEW.content='' THEN
RAISE EXCEPTION 'injected redaction failure';
END IF;
RETURN NEW;
END $$;
CREATE TRIGGER feishu_test_redaction_fault BEFORE UPDATE ON chunks
FOR EACH ROW EXECUTE FUNCTION feishu_test_redaction_fault();`,
			).Error,
		)
		_, e := store.RestrictSource(ctx, ds, "same-cloud-id", false)
		require.Error(t, e)
		require.ErrorIs(
			t,
			types.CheckLegacySourceRead(guardDB, kid, kb.ID),
			types.ErrLegacySourceRestricted,
			"failed redaction must retain the independently committed deny",
		)
		search(true)
		require.NoError(
			t,
			db.Exec(
				"DROP TRIGGER feishu_test_redaction_fault ON chunks; DROP FUNCTION "+
					"feishu_test_redaction_fault()",
			).Error,
		)
		terminal, e := store.RestrictSource(ctx, ds, "same-cloud-id", false)
		require.NoError(t, e)
		require.False(t, terminal, "absence is not confirmed permanent deletion")
		require.ErrorIs(
			t,
			types.CheckLegacySourceRead(guardDB, kid, kb.ID),
			types.ErrLegacySourceRestricted,
		)
		require.ErrorIs(
			t,
			store.SetSourceUncertain(ctx, ds, false),
			types.ErrLegacySourceRestricted,
		)
		var bodies int64
		require.NoError(
			t,
			db.Raw("SELECT count(*) FROM embeddings WHERE knowledge_id=? AND (is_enab"+
				"led OR content<>'')", kid).
				Scan(&bodies).
				Error,
		)
		require.Zero(t, bodies)
		var snapshot string
		require.NoError(
			t,
			db.Raw("SELECT quarantine_snapshot->'knowledge'->>'file_hash' FROM "+
				"feishu_legacy_resources WHERE id=?", r.ID).
				Scan(&snapshot).
				Error,
		)
		require.Equal(t, "takeovercanary", snapshot, "effective content must survive privately")
		terminal, e = store.RestrictSource(ctx, ds, "same-cloud-id", true)
		require.NoError(t, e)
		require.True(t, terminal)
		terminal, e = store.RestrictSource(ctx, ds, "same-cloud-id", true)
		require.NoError(t, e)
		require.True(t, terminal, "trusted withdrawal retry is idempotent")
	})
}
