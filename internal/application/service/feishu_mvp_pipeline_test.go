package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	pgretriever "github.com/Tencent/WeKnora/internal/application/repository/retriever/postgres"
	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Synthetic Feishu/embedding providers; real markdown parser, chunk DB, PG
// vector/BM25, durable dispatch and post-process handoff. Not M5 business proof.
type mvpPipelineEmbedder struct{}

func (mvpPipelineEmbedder) Embed(context.Context, string) ([]float32, error) {
	return []float32{1, 0, 0}, nil
}

func (mvpPipelineEmbedder) BatchEmbed(_ context.Context, values []string) ([][]float32, error) {
	rows := make([][]float32, len(values))
	for i := range rows {
		rows[i] = []float32{1, 0, 0}
	}
	return rows, nil
}
func (mvpPipelineEmbedder) GetDimensions() int { return 3 }

func (e mvpPipelineEmbedder) BatchEmbedWithPool(
	ctx context.Context,
	_ embedding.Embedder,
	values []string,
) ([][]float32, error) {
	return e.BatchEmbed(ctx, values)
}
func (mvpPipelineEmbedder) GetModelID() string   { return "mvp-synthetic" }
func (mvpPipelineEmbedder) GetModelName() string { return "mvp-synthetic" }

type mvpPipelineTenant struct{ interfaces.TenantRepository }

func (mvpPipelineTenant) GetTenantByID(context.Context, uint64) (*types.Tenant, error) {
	return &types.Tenant{
		ID: 1,
		RetrieverEngines: types.RetrieverEngines{
			Engines: []types.RetrieverEngineParams{
				{
					RetrieverEngineType: types.PostgresRetrieverEngineType,
					RetrieverType:       types.VectorRetrieverType,
				},
				{
					RetrieverEngineType: types.PostgresRetrieverEngineType,
					RetrieverType:       types.KeywordsRetrieverType,
				},
			},
		},
	}, nil
}
func (mvpPipelineTenant) AdjustStorageUsed(context.Context, uint64, int64) error { return nil }

func TestMVPPublicParsePGVectorBM25AndWikiHandoff(t *testing.T) { runMVPPipeline(t, false) }

func TestApplicationGrantPGVectorBM25AndWikiHandoff(t *testing.T) { runMVPPipeline(t, true) }

func runMVPPipeline(t *testing.T, application bool) {
	for _, wiki := range []bool{false, true} {
		t.Run(fmt.Sprintf("wiki_%t", wiki), func(t *testing.T) {
			var f *personalRecoveryFixture
			if application {
				f, _ = newApplicationGrantFixture(t)
			} else {
				f = newMVPFixture(t)
			}
			// syncdb isolates tables in a fresh schema. Native extension types
			// and operators live in public; include it without sharing test rows.
			parsed, err := url.Parse(f.db.Dialector.(*postgres.Dialector).DSN)
			require.NoError(t, err)
			query := parsed.Query()
			require.Contains(t, query.Get("search_path"), "sync_test_")
			query.Set("search_path", query.Get("search_path")+",public")
			parsed.RawQuery = query.Encode()
			db, err := gorm.Open(postgres.Open(parsed.String()), &gorm.Config{})
			require.NoError(t, err)
			pool, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { _ = pool.Close() })
			f.db = db
			f.s.db = db
			f.s.settings = repository.NewSystemSettingRepository(db)
			f.p.repo = repository.NewPersonalRecoveryRepository(db)
			f.p.repo.FirstCreateOnly = true
			f.p.repo.ApplicationGrantValidator = f.p.validateApplicationIdentityTx
			f.ks.repo = repository.NewKnowledgeRepository(db)
			f.ks.chunkRepo = repository.NewChunkRepository(db)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer access-A" {
					w.WriteHeader(403)
					return
				}
				switch r.URL.Path {
				case "/open-apis/drive/v1/metas/batch_query":
					_, _ = io.WriteString(
						w,
						"{\"code\":0,\"data\":{\"metas\":[{\"doc_type\":\"file\","+
							"\"doc_token\":\"real\",\"title\":\"test.md\","+
							"\"latest_modify_time\":\"1\"}]}}",
					)
				case "/open-apis/drive/v1/files/real/download":
					_, _ = io.WriteString(
						w,
						"# Recovery knowledge\n\nPersistent personal import uses the original "+
							"logical document identity. Searchable recovery evidence.",
					)
				default:
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			if !application {
				f.s.newClient = func(cfg *core.Config) *core.Client {
					cfg.BaseURL = server.URL
					return core.NewClient(
						cfg,
					)
				}
			}
			for _, sql := range []string{
				"CREATE TABLE embeddings(id bigserial PRIMARY KEY,created_at " +
					"timestamptz,updated_at timestamptz,source_id varchar(64) NOT NULL," +
					"source_type integer NOT NULL,chunk_id varchar(64),knowledge_id " +
					"varchar(64),knowledge_base_id varchar(64),tag_id text,content text," +
					"dimension integer NOT NULL,embedding halfvec,is_enabled boolean," +
					"UNIQUE(source_id,source_type))",
				"CREATE INDEX mvp_bm25 ON embeddings USING bm25(id,knowledge_base_id," +
					"content,knowledge_id,chunk_id) WITH(key_field='id')",
			} {
				require.NoError(t, f.db.Exec(sql).Error)
			}
			require.NoError(
				t,
				f.db.AutoMigrate(&types.TaskDeadLetter{}, &types.WikiPage{}, &types.WikiPageRevision{}),
			)
			kb := &types.KnowledgeBase{
				ID:               "kb",
				TenantID:         1,
				CreatorID:        "A",
				Type:             types.KnowledgeBaseTypeDocument,
				EmbeddingModelID: "mvp-synthetic",
				ChunkingConfig:   types.ChunkingConfig{ChunkSize: 1024, ChunkOverlap: 32},
				IndexingStrategy: types.IndexingStrategy{
					VectorEnabled:  true,
					KeywordEnabled: true,
					WikiEnabled:    wiki,
				},
			}
			if application {
				kb.ChunkingConfig.ParserEngineRules = []types.ParserEngineRule{
					{FileTypes: []string{"docx"}, Engine: "anydoc"},
				}
			}
			f.ks.kbService = dispatchActualKB{kb: kb}
			f.s.kbs = dispatchActualKB{kb: kb}
			f.ks.tenantRepo = mvpPipelineTenant{}
			f.ks.tenantService = &stubTenantServiceForModelDelete{}
			f.ks.modelService = parentChildModelService{embedder: mvpPipelineEmbedder{}}
			pg := pgretriever.NewPostgresRetrieveEngineRepository(f.db)
			f.ks.retrieveEngine = parentChildRetrieveRegistry{
				engine: retriever.NewKVHybridRetrieveEngine(pg, types.PostgresRetrieverEngineType),
			}
			f.ks.graphEngine = parentChildGraphRepo{}
			// The Wiki-off case composes queue recovery with real parse/index
			// completion, rather than inferring completion from queue delivery.
			var queue *dispatchActualQueue
			if !wiki {
				addr := os.Getenv("FEISHU_DISPATCH_REDIS_ADDR")
				require.Equal(t, "knowhub-p1-dispatch-redis-20261018:6379", addr)
				rdb := redis.NewClient(&redis.Options{Addr: addr})
				defer func(cleanup func() error) { _ = cleanup() }(rdb.Close)
				require.NoError(t, rdb.FlushDB(f.ctx).Err())
				down := asynq.NewClient(
					asynq.RedisClientOpt{
						Addr:         "127.0.0.1:1",
						DialTimeout:  50 * time.Millisecond,
						ReadTimeout:  50 * time.Millisecond,
						WriteTimeout: 50 * time.Millisecond,
					},
				)
				defer func(cleanup func() error) { _ = cleanup() }(down.Close)
				queue = &dispatchActualQueue{client: down}
				f.ks.task = queue
			}
			sourceURL := "https://tenant.feishu.cn/file/real"
			if application {
				sourceURL = "https://tenant.feishu.cn/docx/real"
			}
			m := f.prepare(sourceURL)
			require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
			k, _, payload := mvpPublicTask(t, f, m)
			no := false
			require.NoError(t, k.SetProcessOverrides(&types.KnowledgeProcessOverrides{SummaryEnabled: &no}))
			require.NoError(t, f.ks.repo.UpdateKnowledge(f.ctx, k))
			if queue != nil {
				addr := os.Getenv("FEISHU_DISPATCH_REDIS_ADDR")
				client := asynq.NewClient(asynq.RedisClientOpt{Addr: addr})
				defer func(cleanup func() error) { _ = cleanup() }(client.Close)
				queue.client = client
				inspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: addr})
				defer func(cleanup func() error) { _ = cleanup() }(inspector.Close)
				f.ks.taskInspector = dispatchActualInspector{raw: inspector}
				f.ks.redisClient = redis.NewClient(&redis.Options{Addr: addr})
				defer func(cleanup func() error) { _ = cleanup() }(f.ks.redisClient.Close)
				restarted := restartKnowledgeServiceForTest(t, f.ks)
				require.NoError(t, restarted.RecoverKnowledgeDispatches(f.ctx))
				results := make(chan error, 4)
				worker := asynq.NewServer(
					asynq.RedisClientOpt{Addr: addr},
					asynq.Config{
						Concurrency:       2,
						Queues:            map[string]int{types.QueueDefault: 1},
						TaskCheckInterval: 50 * time.Millisecond,
						ShutdownTimeout:   time.Second,
					},
				)
				mux := asynq.NewServeMux()
				taskType := types.TypeDocumentProcess
				if application {
					taskType = types.TypeFeishuApplicationProcess
				}
				mux.HandleFunc(taskType, func(ctx context.Context, t *asynq.Task) error {
					err := restarted.ProcessDocument(ctx, t)
					select {
					case results <- err:
					default:
					}
					return err
				})
				require.NoError(t, worker.Start(mux))
				defer worker.Shutdown()
				select {
				case err := <-results:
					require.NoError(t, err)
				case <-time.After(15 * time.Second):
					t.Fatal("original Knowledge task never completed after Redis recovery")
				}
			} else {
				b, _ := json.Marshal(payload)
				require.NoError(t, f.ks.ProcessDocument(f.ctx, asynq.NewTask(func() string {
					if application {
						return types.TypeFeishuApplicationProcess
					}
					return types.TypeDocumentProcess
				}(), b)))
			}
			var receipt types.TaskPendingOp
			require.NoError(
				t,
				f.db.Where("scope = ? AND scope_id = ?", personalIndexReceiptScope, k.ID).
					First(&receipt).
					Error,
			)
			require.Equal(t, "verified", receipt.Op)
			chunks, err := f.ks.chunkRepo.ListAllChunksByKnowledgeID(f.ctx, k.TenantID, k.ID)
			require.NoError(t, err)
			require.NotEmpty(t, chunks)
			vector, err := pg.Retrieve(
				f.ctx,
				types.RetrieveParams{
					KnowledgeBaseIDs: []string{"kb"},
					Embedding:        []float32{1, 0, 0},
					TopK:             5,
					RetrieverType:    types.VectorRetrieverType,
					AdditionalParams: map[string]any{"dimension": 3},
				},
			)
			require.NoError(t, err)
			require.NotEmpty(t, vector)
			keyword, err := pg.Retrieve(
				f.ctx,
				types.RetrieveParams{KnowledgeBaseIDs: []string{"kb"}, Query: func() string {
					if application {
						return "fixture"
					}
					return "recovery"
				}(), TopK: 5, RetrieverType: types.KeywordsRetrieverType},
			)
			require.NoError(t, err)
			require.NotEmpty(t, keyword)
			post := NewKnowledgePostProcessService(
				f.ks.repo,
				f.ks.kbService,
				nil,
				f.ks.chunkRepo,
				f.ks.task,
				repository.NewTaskPendingOpsRepository(f.db),
				nil,
				nil,
			)
			postBody, _ := json.Marshal(
				types.KnowledgePostProcessPayload{
					TenantID:        k.TenantID,
					KnowledgeBaseID: k.KnowledgeBaseID,
					KnowledgeID:     k.ID,
					Attempt:         payload.Attempt,
				},
			)
			require.NoError(t, post.Handle(f.ctx, asynq.NewTask(types.TypeKnowledgePostProcess, postBody)))
			rows, err := f.s.ListImports(f.ctx, "kb")
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.Equal(t, "verified", rows[0].IndexStatus)
			if wiki {
				require.Equal(t, "pending", rows[0].WikiStatus)
				require.NotEqual(t, "synced", rows[0].Status)
				var n int64
				require.NoError(
					t,
					f.db.Model(&types.TaskPendingOp{}).
						Where("task_type = ? AND scope_id = ? AND dedup_key = ?", wikiTaskType, "kb", k.ID).
						Count(&n).
						Error,
				)
				require.EqualValues(t, 1, n)
			} else {
				require.Equal(t, "disabled", rows[0].WikiStatus)
				require.Equal(t, "synced", rows[0].Status)
			}
			mvpAssertSingle(t, f, m)
		})
	}
}
