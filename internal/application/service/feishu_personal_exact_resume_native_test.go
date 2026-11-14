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
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	storagefiles "github.com/Tencent/WeKnora/internal/application/service/file"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

func resumeNativeDOCX(t *testing.T) []byte {
	return resumeNativeDOCXWithText(
		t,
		"Verified recovery source: independent research index, document "+
			"provenance, source permissions and current resource identity.",
	)
}

func resumeNativeDOCXWithText(t *testing.T, text string) []byte {
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
			strings.Repeat(`<w:p><w:r><w:t>`+text+`</w:t></w:r></w:p>`, 20) +
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
	exerciseNativeResume(t, false, false)
}

func TestPersonalExactResumeNativePublishesEmptyOutput(t *testing.T) {
	exerciseNativeResume(t, true, false)
}

func TestPersonalExactResumeNativePublishesEmptyOutputWithCatalogImages(t *testing.T) {
	exerciseNativeResume(t, true, true)
}

func resumeNativeImageDOCX(t *testing.T, data []byte) []byte {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	parts := map[string][]byte{}
	for _, entry := range z.File {
		r, err := entry.Open()
		require.NoError(t, err)
		parts[entry.Name], err = io.ReadAll(r)
		require.NoError(t, err)
		require.NoError(t, r.Close())
	}
	parts["[Content_Types].xml"] = []byte(strings.Replace(string(parts["[Content_Types].xml"]), "</Types>",
		`<Default Extension="png" ContentType="image/png"`+
			`/></Types>`, 1))
	parts["word/_rels/document.xml.rels"] = []byte(`<Relationships xmlns="http://schemas.openxmlform` +
		`ats.org/package/2006/rel` +
		`ationships"><Relationship Id="picture" Type="http://schemas.openxmlforma` +
		`ts.org/officeDocument/2006/relationships/image" Target="media/picture.pn` +
		`g"/></Relationships>`)
	drawing := `<w:p><w:r><w:drawing><wp:inline xmlns:wp="http://schemas.openxmlformats.` +
		`org/drawingml/2006/wordprocessingDrawing"><wp:extent cx="95250" cy="9525` +
		`0"/><wp:docPr id="1" name="Checked picture"/><a:graphic xmlns:a="http://` +
		`schemas.openxmlformats.org/drawingml/2006/main"><a:graphicData uri="http` +
		`://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic xmlns:pic` +
		`="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:nvPicPr` +
		`><pic:cNvPr id="1" name="Checked picture"/><pic:cNvPicPr/></pic:nvPicPr>` +
		`<pic:blipFill><a:blip xmlns:r="http://schemas.openxmlformats.org/officeD` +
		`ocument/2006/relationships" r:embed="picture"/><a:stretch><a:fillRect/><` +
		`/a:stretch></pic:blipFill><pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext c` +
		`x="95250" cy="95250"/></a:xfrm><a:prstGeom prst="rect"/></pic:spPr></pic` +
		`:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
	parts["word/document.xml"] = []byte(strings.Replace(string(parts["word/document.xml"]), "</w:body>", drawing+
		"</w:body>", 1))
	var picture bytes.Buffer
	require.NoError(t, png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	parts["word/media/picture.png"] = picture.Bytes()
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	for name, body := range parts {
		entry, err := w.Create(name)
		require.NoError(t, err)
		_, err = entry.Write(body)
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return out.Bytes()
}

func exerciseNativeResume(t *testing.T, empty, withImages bool) {
	exerciseNativeResumeWithData(t, empty, withImages, nil)
}

func TestPersonalExactResumeNativeOriginalWorkbookPublication(t *testing.T) {
	root := os.Getenv("EXACT_PUBLICATION_FIXTURE_DIR")
	if root == "" {
		t.Skip("requires isolated incident originals")
	}
	t.Setenv("FEISHU_PERSONAL_EXACT_EMBEDDED_XLSX", "1")
	for _, id := range []string{
		"3d5c264c-2bf7-48fe-9c8d-f4e729346491", "0765a51b-2ac5-4b9c-952b-f442b5c42397",
		"418a14cb-1c82-4f29-a44b-c4b753d446fa", "142fee4d-b2ea-4b67-8ed8-02fa74544a24",
		"c8961e8d-7a21-415e-9f15-0779c9de5dfc",
	} {
		t.Run(id, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, id+".docx"))
			require.NoError(t, err)
			exerciseNativeResumeWithData(t, false, true, data)
		})
	}
}

func exerciseNativeResumeWithData(t *testing.T, empty, withImages bool, data []byte) {
	exerciseNativeResumeWithFormat(t, empty, withImages, data, nativeResumeFormatOptions{Format: "docx"})
}

type nativeResumeFormatOptions struct {
	Format               string
	AuthorizationFailure bool
	AssertClosed         func(*personalRecoveryFixture, *types.FeishuPersonalAttempt, *atomic.Int64)
}

func exerciseNativeResumeWithFormat(
	t *testing.T, empty, withImages bool, data []byte, options nativeResumeFormatOptions,
) {
	format, authorizationFailure := options.Format, options.AuthorizationFailure
	t.Setenv("SYNC_EXECUTION_POSTGRES_PUBLIC_EXTENSIONS", "1")
	t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY", "1")
	f := newMVPFixture(t)
	f.p.lease = 2 * time.Minute
	if data == nil {
		data = resumeNativeDOCX(t)
		if withImages {
			data = resumeNativeImageDOCX(t, data)
		}
	}
	files := map[string][]byte{
		"real": data,
		"real2": resumeNativeDOCXWithText(
			t,
			"Verified recovery source two: published department knowledge "+
				"remains readable after source disconnect.",
		),
		"real3": resumeNativeDOCXWithText(
			t,
			"Verified recovery source three: source synchronization "+
				"authorization is separate from published knowledge access.",
		),
	}
	var sourceRequests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sourceRequests.Add(1)
		require.Equal(t, "Bearer access-A", r.Header.Get("Authorization"))
		switch {
		case r.URL.Path == "/open-apis/drive/v1/metas/batch_query":
			var request struct {
				Documents []struct {
					Token string `json:"doc_token"`
				} `json:"request_docs"`
			}
			if json.NewDecoder(r.Body).Decode(&request) != nil || len(request.Documents) != 1 {
				w.WriteHeader(400)
				return
			}
			token := request.Documents[0].Token
			if files[token] == nil {
				w.WriteHeader(403)
				return
			}
			extension := ".docx"
			if token == "real" {
				extension = "." + format
			}
			_ = json.NewEncoder(w).
				Encode(map[string]any{
					"code": 0,
					"data": map[string]any{"metas": []any{map[string]any{
						"doc_type":  "file",
						"doc_token": token, "title": "resume-" + token + extension, "latest_modify_time": fmt.
								Sprint(f.revision.Load()),
					}}},
				})
		case strings.HasPrefix(r.URL.Path, "/open-apis/drive/v1/files/") && strings.HasSuffix(r.URL.
			Path, "/download"):
			token := strings.TrimSuffix(
				strings.TrimPrefix(r.URL.Path, "/open-apis/drive/v1/files/"),
				"/download",
			)
			if files[token] == nil {
				w.WriteHeader(403)
				return
			}
			_, _ = w.Write(files[token])
		case strings.HasPrefix(r.URL.Path, "/open-apis/drive/v1/permissions/") && strings.HasSuffix(r.
			URL.Path, "/members/auth"):
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
	retired, err := os.ReadFile("../../../migrations/versioned/000033_add_video_info_to_chunks.up.sql")
	require.NoError(t, err)
	require.NoError(t, f.db.Exec(string(retired)).Error)
	t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY_KNOWLEDGE_IDS", knowledge.ID)
	if format == "xlsx" {
		t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY_AUTOMATIC", "1")
		t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY_KNOWLEDGE_IDS", "")
	}
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
	if withImages {
		catalog := NewResourceCatalog(repository.NewResourceRepository(f.db))
		f.ks.fileSvc = storagefiles.NewResourceCatalogFileService(f.ks.fileSvc, catalog)
		f.ks.resourceCatalog = catalog
	}
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
	t.Setenv("FEISHU_PERSONAL_WORKSPACE_IMPORT_SNAPSHOTS", "1")
	requestsBeforeProcessing := sourceRequests.Load()
	owner, err := f.p.repo.Claim(context.Background(), imported.AttemptID, f.p.lease)
	require.NoError(t, err)
	proof, err := f.p.prepareExactPersonalDocument(context.Background(), owner)
	require.NoError(t, err)
	provider, _, modelCalls := configureOriginalExactConsumer(t, f, proof)
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
	if !authorizationFailure {
		require.NoError(t, f.p.beginExactPersonalReceipt(work, proof))
		work = context.WithValue(work, personalFirstCreateContextKey{}, true)
		work = context.WithValue(work, knowledgeDispatchContextKey{}, op)
		body, err := json.Marshal(payload)
		require.NoError(t, err)
		require.NoError(t, f.ks.processDocument(work, asynq.NewTask(types.TypeDocumentProcess, body)))
		consumer, _ := exactPersonalConsumerFrom(work)
		require.True(t, consumer.indexed)
	}
	var originalIDs []string
	require.NoError(t, f.db.Table("chunks").Where("knowledge_id=?", knowledge.ID).
		Order("id").Pluck("id", &originalIDs).Error)
	if authorizationFailure {
		require.True(t, empty)
		require.Empty(t, originalIDs)
	} else {
		require.NotEmpty(t, originalIDs)
	}
	var originalVectors []struct {
		ID       int64
		SourceID string
	}
	require.NoError(t, f.db.Table("embeddings").Select("id,source_id").
		Where("knowledge_id=?", knowledge.ID).Order("source_id").Scan(&originalVectors).Error)
	if authorizationFailure {
		require.Empty(t, originalVectors)
	} else {
		require.NotEmpty(t, originalVectors)
	}
	if empty {
		require.NoError(t, f.db.Exec("DELETE FROM embeddings WHERE knowledge_id=?", knowledge.ID).Error)
		require.NoError(t, f.db.Exec("DELETE FROM chunks WHERE knowledge_id=?", knowledge.ID).Error)
		require.NoError(t, f.db.Exec("UPDATE knowledges SET storage_size=0 WHERE id=?", knowledge.ID).Error)
	}

	require.NoError(t, f.p.repo.AbortExactExecution(context.Background(), owner, op.ID))
	if authorizationFailure {
		// Reconstruct the historical pre-parser OAuth failure. No output or
		// receipt has been created, and recovery must re-prove the local copy.
		require.NoError(t, f.db.Exec("UPDATE feishu_personal_attempts SET last_error_category=? WHERE id=?",
			"authorization_or_resource_missing", owner.ID).Error)
	}
	observed := mustPersonalAttempt(t, f, owner.ID)
	// The source can be edited while this imported copy awaits recovery.
	// Resume must publish the checked original bytes without replacing chunks.
	f.revision.Store(2)
	var connection types.FeishuConnection
	var grant types.FeishuPersonalGrantEpoch
	require.NoError(t, f.db.Where("user_id='A'").Take(&connection).Error)
	require.NoError(t, f.db.Where("user_id='A'").Take(&grant).Error)
	require.NoError(t, f.db.Where("user_id='A'").Delete(&types.FeishuConnection{}).Error)
	require.NoError(t, f.db.Where("user_id='A'").Delete(&types.FeishuPersonalGrantEpoch{}).Error)
	if options.AssertClosed != nil {
		options.AssertClosed(f, observed, modelCalls)
		return
	}
	require.NoError(t, f.p.prepareExactPersonalResume(context.Background(), observed))
	require.NoError(t, f.p.processFirstCreateDocument(context.Background(), payload))
	require.Equal(t, requestsBeforeProcessing, sourceRequests.Load(),
		"processing and publishing an imported copy must never revisit its source")
	require.NoError(t, f.db.Create(&connection).Error)
	require.NoError(t, f.db.Create(&grant).Error)
	var receipt types.TaskPendingOp
	require.NoError(t, f.db.Where("scope=? AND scope_id=?", types.PersonalExactReceiptScope,
		knowledge.ID).Take(&receipt).Error)
	require.Equal(t, "published", receipt.Op)
	var published types.PersonalExactReceipt
	require.NoError(t, json.Unmarshal(receipt.Payload, &published))
	require.NoError(t, f.p.verifyExactPublishedDatabase(context.Background(), published))
	assertExactPublicationHistory(t, f, imported.ID)
	var retained []string
	require.NoError(t, f.db.Table("chunks").Where("knowledge_id=? AND chunk_type<>'summary'", knowledge.ID).
		Order("id").Pluck("id", &retained).Error)
	if empty {
		require.NotEmpty(t, retained)
	} else {
		require.Equal(t, originalIDs, retained)
	}
	var retainedVectors []struct {
		ID       int64
		SourceID string
	}
	require.NoError(t, f.db.Table("embeddings").Select("id,source_id").
		Where("knowledge_id=? AND source_id IN ?", knowledge.ID, originalIDs).
		Order("source_id").Scan(&retainedVectors).Error)
	if empty {
		require.Empty(t, retainedVectors, "new chunks must not adopt deleted IDs")
	} else {
		require.Equal(t, originalVectors, retainedVectors)
	}
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
	if withImages {
		var bound int64
		require.NoError(t, f.db.Table("resource_bindings").Where("owner_id=? AND relation=?",
			knowledge.ID, types.ResourceRelationExtractedImage).Count(&bound).Error)
		require.Positive(t, bound, "fresh image assets must remain bound after empty-output recovery")
		return
	}
	exerciseAdditionalPublishedSnapshots(t, f, knowledge.ID, files)
	exercisePublishedSnapshotReaderMatrix(t, f, knowledge.ID)
	require.NoError(t, f.s.Disconnect(f.ctx))
	_, err = f.p.exactPublishedReadContext(web, knowledge.ID)
	require.NoError(t, err, "disconnect must preserve the published local snapshot")
}
