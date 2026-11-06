//go:build anydoc && personal_exact_live

package service

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

func resumeNativeDOCX(t *testing.T) []byte {
	t.Helper()
	parts := map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ` +
			`ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Override PartName="/word/document.xml" ` +
			`ContentType="application/vnd.openxmlformats-officedocument.` +
			`wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" ` +
			`Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" ` +
			`Target="word/document.xml"/></Relationships>`,
		"word/document.xml": `<w:document ` +
			`xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
			strings.Repeat(`<w:p><w:r><w:t>Verified recovery source: independent research index, `+
				`document provenance, source permissions and `+
				`current resource identity.</w:t></w:r></w:p>`, 20) +
			`</w:body></w:document>`,
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, body := range parts {
		entry, err := writer.Create(name)
		require.NoError(t, err)
		_, err = entry.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	return buffer.Bytes()
}

func TestPersonalExactResumeNativePublishesRetainedChunksWithoutDuplicates(t *testing.T) {
	t.Setenv("SYNC_EXECUTION_POSTGRES_PUBLIC_EXTENSIONS", "1")
	t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY", "1")
	f := newMVPFixture(t)
	f.p.lease = 2 * time.Minute
	data := resumeNativeDOCX(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer access-A", r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/open-apis/drive/v1/metas/batch_query":
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"metas":[{"doc_type":"file",`+
				`"doc_token":"real","title":"resume.docx","latest_modify_time":"1"}]}}`)
		case "/open-apis/drive/v1/files/real/download":
			_, _ = w.Write(data)
		case "/open-apis/drive/v1/permissions/real/members/auth":
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"auth_result":true}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	f.s.newClient = func(cfg *core.Config) *core.Client {
		cfg.BaseURL = server.URL
		return core.NewClient(cfg)
	}
	imported := f.prepare("https://tenant.feishu.cn/file/real")
	require.NoError(t, f.p.Process(f.ctx, f.payload(imported.AttemptID)))
	knowledge, op, payload := mvpPublicTask(t, f, imported)
	t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY_KNOWLEDGE_IDS", knowledge.ID)
	require.NoError(t, f.db.AutoMigrate(&types.StoredResource{}, &types.ResourceBinding{},
		&types.DataSource{}, &types.TaskDeadLetter{}))
	ddl, err := os.ReadFile("../../../migrations/versioned/003036_feishu_legacy_atomic.up.sql")
	require.NoError(t, err)
	legacy := strings.Replace(string(ddl), "ADD COLUMN legacy_access_state",
		"ADD COLUMN IF NOT EXISTS legacy_access_state", 1)
	require.NoError(t, f.db.Exec(legacy).Error)
	sum := sha256.Sum256(data)
	file := types.StoredResource{
		ID: uuid.NewString(), Handle: "abcdefghijklmnopqrstuv", TenantID: 1, Provider: "local",
		PhysicalPath: knowledge.FilePath, LocationHash: "resume-native", Size: int64(len(data)),
		ContentHash: hex.EncodeToString(sum[:]), State: "active", Lifecycle: "persistent",
	}
	require.NoError(t, f.db.Create(&file).Error)
	require.NoError(t, f.db.Create(&types.ResourceBinding{
		ID: uuid.NewString(), ResourceID: file.ID, TenantID: 1,
		OwnerType: "knowledge", OwnerID: knowledge.ID, Relation: "source_file",
	}).Error)
	payload.FilePath = types.BuildResourcePath(file.Handle)
	require.NoError(t, f.db.Exec("UPDATE knowledges SET file_path=? WHERE id=?",
		payload.FilePath, knowledge.ID).Error)
	var intent types.KnowledgeDispatch
	require.NoError(t, json.Unmarshal(op.Payload, &intent))
	intent.Task = payload
	envelope, err := json.Marshal(intent)
	require.NoError(t, err)
	require.NoError(t, f.db.Exec("UPDATE task_pending_ops SET payload=?,op='processing' WHERE id=?",
		string(envelope), op.ID).Error)
	types.EnableLegacyReadGuards(f.db)
	owner, err := f.p.repo.Claim(context.Background(), imported.AttemptID, f.p.lease)
	require.NoError(t, err)
	proof, err := f.p.prepareExactPersonalDocument(context.Background(), owner)
	require.NoError(t, err)
	provider, _, _ := configureOriginalExactConsumer(t, f, proof)
	inner, err := originalAcceptanceChat(t, provider.URL)
	require.NoError(t, err)
	gate := repository.NewModelRepository(f.db).(legacyModelInputGate)
	f.ks.modelService = &exactAcceptanceModels{
		v2PipelineModel: f.ks.modelService.(*v2PipelineModel), recovery: f.p,
		summary: &legacyGuardedChat{inner: inner, gate: gate, tenant: 1},
	}
	require.NoError(t, f.db.Table("knowledge_bases").Where("id=?", proof.kb).
		Update("summary_model_id", "isolated-summary").Error)
	work, err := f.p.exactConsumerContext(context.Background(), proof)
	require.NoError(t, err)
	require.NoError(t, f.p.beginExactPersonalReceipt(work, proof))
	work = context.WithValue(work, personalFirstCreateContextKey{}, true)
	work = context.WithValue(work, knowledgeDispatchContextKey{}, op)
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, f.ks.processDocument(work, asynq.NewTask(types.TypeDocumentProcess, body)))
	consumer, _ := exactPersonalConsumerFrom(work)
	require.True(t, consumer.indexed)
	var originalIDs []string
	require.NoError(t, f.db.Table("chunks").Where("knowledge_id=?", knowledge.ID).
		Order("id").Pluck("id", &originalIDs).Error)
	require.NotEmpty(t, originalIDs)
	var originalVectors []struct {
		ID       int64
		SourceID string
	}
	require.NoError(t, f.db.Table("embeddings").Select("id,source_id").
		Where("knowledge_id=?", knowledge.ID).Order("source_id").Scan(&originalVectors).Error)
	require.NotEmpty(t, originalVectors)
	require.NoError(t, f.p.repo.AbortExactExecution(context.Background(), owner, op.ID))
	observed := mustPersonalAttempt(t, f, owner.ID)
	require.NoError(t, f.p.prepareExactPersonalResume(context.Background(), observed))
	require.NoError(t, f.p.processFirstCreateDocument(context.Background(), payload))
	var receipt types.TaskPendingOp
	require.NoError(t, f.db.Where("scope=? AND scope_id=?", types.PersonalExactReceiptScope,
		knowledge.ID).Take(&receipt).Error)
	require.Equal(t, "published", receipt.Op)
	var published types.PersonalExactReceipt
	require.NoError(t, json.Unmarshal(receipt.Payload, &published))
	require.NoError(t, f.p.verifyExactPublishedDatabase(context.Background(), published))
	var retained []string
	require.NoError(t, f.db.Table("chunks").Where("knowledge_id=? AND chunk_type<>'summary'", knowledge.ID).
		Order("id").Pluck("id", &retained).Error)
	require.Equal(t, originalIDs, retained)
	var retainedVectors []struct {
		ID       int64
		SourceID string
	}
	require.NoError(t, f.db.Table("embeddings").Select("id,source_id").
		Where("knowledge_id=? AND source_id IN ?", knowledge.ID, originalIDs).
		Order("source_id").Scan(&retainedVectors).Error)
	require.Equal(t, originalVectors, retainedVectors)
	var summaryRows int64
	require.NoError(t, f.db.Table("chunks").Where("knowledge_id=? AND chunk_type='summary'", knowledge.ID).
		Count(&summaryRows).Error)
	require.EqualValues(t, 1, summaryRows)
	current := mustPersonalAttempt(t, f, owner.ID)
	require.Equal(t, "indexed", current.Status)
	require.Nil(t, current.LeaseUntil)
	require.Equal(t, observed.RetryCount+1, current.RetryCount)
	dispatch := f.ks.repo.(interfaces.KnowledgeDispatchRepository)
	complete, err := dispatch.GetKnowledgeDispatch(context.Background(), 1, knowledge.ID)
	require.NoError(t, err)
	require.Equal(t, op.ID, complete.ID)
	require.Equal(t, "completed", complete.Op)
	require.NoError(t, f.p.processFirstCreateDocument(context.Background(), payload))
	var after int64
	require.NoError(t, f.db.Table("chunks").Where("knowledge_id=? AND chunk_type='summary'", knowledge.ID).
		Count(&after).Error)
	require.Equal(t, summaryRows, after, "duplicate delivery must not republish")
	search := &knowledgeBaseService{
		personalRecovery: f.p, repo: repository.NewKnowledgeBaseRepository(f.db),
		kgRepo: f.ks.repo, chunkRepo: f.ks.chunkRepo, modelService: f.ks.modelService,
		kbShareService: f.p.shares, retrieveEngine: f.ks.retrieveEngine,
	}
	web := types.WithPrincipal(f.ctx, types.Principal{Type: types.PrincipalWebUser, ID: "A"})
	web = types.WithExecutionTenant(web, 1)
	web, err = restoreSummaryRefreshTenantInfo(web, f.ks.tenantRepo, 1)
	require.NoError(t, err)
	query := make([]float32, 1024)
	query[0] = 1
	results, err := search.HybridSearch(web, proof.kb, types.SearchParams{
		KnowledgeIDs: []string{knowledge.ID}, QueryText: "recovery", QueryEmbedding: query,
		MatchCount: 10,
	})
	require.NoError(t, err)
	require.NotEmpty(t, results, "normal hybrid search must return the published retained source")
	for _, hit := range results {
		require.Equal(t, knowledge.ID, hit.KnowledgeID)
	}
	require.NoError(t, f.s.Disconnect(f.ctx))
	_, err = f.p.exactPublishedReadContext(web, knowledge.ID)
	require.Error(t, err, "disconnect must close current source reads")
}
