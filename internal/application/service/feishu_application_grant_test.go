package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/Tencent/WeKnora/internal/infrastructure/docparser/anydoc"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func applicationTestConfig(t *testing.T, mode, callback string) *FeishuAppConfig {
	t.Helper()
	data, err := json.Marshal(map[string]any{"app_id": "cli_test", "client_secret": "synthetic-only", "auth_mode": mode, "redirect_uri": callback, "enabled": true})
	require.NoError(t, err)
	var cfg FeishuAppConfig
	require.NoError(t, json.Unmarshal(data, &cfg))
	return &cfg
}

func newApplicationGrantFixture(t *testing.T) (*personalRecoveryFixture, *atomic.Bool) {
	t.Helper()
	if !anydoc.Available() {
		t.Skip("application Docx verification requires anydoc native engine")
	}
	t.Setenv("FEISHU_APPLICATION_GRANT_TEST_ONLY", "1")
	t.Setenv("FEISHU_APPLICATION_GRANT_TEST_KB_IDS", "kb,kb2")
	f := newMVPFixture(t)
	require.NoError(t, f.db.Exec("UPDATE users SET is_system_admin=true").Error)
	f.ctx = context.WithValue(f.ctx, types.SystemAdminContextKey, true)
	require.NoError(t, f.s.SaveApp(f.ctx, applicationTestConfig(t, "application_grant", "")))
	require.NoError(t, f.db.Where("1=1").Delete(&types.FeishuConnection{}).Error)
	var buffer bytes.Buffer
	z := zip.NewWriter(&buffer)
	for name, content := range map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":         `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml":   `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Fixture Document</w:t></w:r></w:p><w:sectPr/></w:body></w:document>`,
	} {
		w, err := z.Create(name)
		require.NoError(t, err)
		_, err = w.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, z.Close())
	data := buffer.Bytes()
	revoked := &atomic.Bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal" {
			io.WriteString(w, `{"code":0,"tenant_access_token":"application-test-token","expire":7200}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer application-test-token" {
			w.WriteHeader(403)
			return
		}
		if revoked.Load() {
			w.WriteHeader(403)
			io.WriteString(w, `{"code":1770032}`)
			return
		}
		switch r.URL.Path {
		case "/open-apis/wiki/v2/spaces/get_node":
			io.WriteString(w, `{"code":0,"data":{"node":{"obj_type":"docx","obj_token":"real"}}}`)
		case "/open-apis/drive/v1/metas/batch_query":
			fmt.Fprintf(w, `{"code":0,"data":{"metas":[{"doc_type":"docx","doc_token":"real","title":"application-test","latest_modify_time":"%d"}]}}`, f.revision.Load())
		case "/open-apis/drive/v1/export_tasks":
			if f.block != nil {
				f.block()
			}
			io.WriteString(w, `{"code":0,"data":{"ticket":"ticket"}}`)
		case "/open-apis/drive/v1/export_tasks/ticket":
			io.WriteString(w, `{"code":0,"data":{"result":{"job_status":0,"file_token":"download-token","file_name":"test.docx"}}}`)
		case "/open-apis/drive/v1/export_tasks/file/download-token/download":
			f.downloads.Add(1)
			w.Write(data)
		case "/open-apis/docx/v1/documents/real/blocks":
			io.WriteString(w, `{"code":0,"data":{"items":[{"block_type":1},{"block_type":2}],"has_more":false}}`)
		case "/open-apis/docx/v1/documents/real/raw_content":
			io.WriteString(w, `{"code":0,"data":{"content":"Fixture Document"}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(server.Close)
	f.s.newClient = func(cfg *core.Config) *core.Client { cfg.BaseURL = server.URL; return core.NewClient(cfg) }
	return f, revoked
}

func TestApplicationGrantRevokedBetweenExportAndDownload(t *testing.T) {
	f, revoked := newApplicationGrantFixture(t)
	m := f.prepare("https://test.feishu.cn/docx/real")
	f.block = func() { revoked.Store(true) }
	require.Error(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
	require.Zero(t, f.downloads.Load(), "cached export ticket must not bypass live source ACL")
	_, _, a, err := f.p.repo.Load(f.ctx, m.AttemptID)
	require.NoError(t, err)
	require.Equal(t, "needs_attention", a.Status)
	var count int64
	require.NoError(t, f.db.Model(&types.Knowledge{}).Count(&count).Error)
	require.Zero(t, count)
	f.block = nil
	revoked.Store(false)
	retry := f.prepare("https://test.feishu.cn/docx/real")
	require.Equal(t, m.LogicalKnowledgeID, retry.LogicalKnowledgeID)
	require.Equal(t, m.AttemptID, retry.AttemptID)
	require.NoError(t, f.p.Process(f.ctx, f.payload(retry.AttemptID)))
}

type applicationTestKBs struct {
	interfaces.KnowledgeBaseService
	values map[string]*types.KnowledgeBase
}

func (v applicationTestKBs) GetKnowledgeBaseByID(_ context.Context, id string) (*types.KnowledgeBase, error) {
	if kb, ok := v.values[id]; ok {
		return kb, nil
	}
	return nil, access.ErrNotFound
}

func TestApplicationGrantSameSourceDifferentKBs(t *testing.T) {
	f, _ := newApplicationGrantFixture(t)
	require.NoError(t, f.db.Exec("INSERT INTO knowledge_bases(id,tenant_id,creator_id) VALUES('kb2',1,'A')").Error)
	kbs := applicationTestKBs{values: map[string]*types.KnowledgeBase{"kb": {ID: "kb", TenantID: 1, CreatorID: "A"}, "kb2": {ID: "kb2", TenantID: 1, CreatorID: "A"}}}
	f.s.kbs = kbs
	f.ks.kbService = kbs
	first := f.prepare("https://test.feishu.cn/docx/real")
	caller := types.Caller{TenantID: 1, UserID: "A", Role: types.TenantRoleAdmin}
	grant, err := access.ResolveKB(f.ctx, access.KBRequest{Caller: caller}, kbs.values["kb2"], types.OrgRoleEditor, nil, nil)
	require.NoError(t, err)
	ctx := grant.Context(f.ctx)
	second, err := f.s.Import(ctx, "kb2", "https://test.feishu.cn/docx/real")
	require.NoError(t, err)
	require.NotEqual(t, first.LogicalKnowledgeID, second.LogicalKnowledgeID)
	require.NoError(t, f.p.Process(f.ctx, f.payload(first.AttemptID)))
	require.NoError(t, f.p.Process(ctx, f.payload(second.AttemptID)))
	var count int64
	require.NoError(t, f.db.Model(&types.Knowledge{}).Count(&count).Error)
	require.EqualValues(t, 2, count)
}

func TestApplicationGrantCommitUnknownRetainsOriginalKnowledge(t *testing.T) {
	for _, mode := range []int32{1, 2} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			f, _ := newApplicationGrantFixture(t)
			m := f.prepare("https://test.feishu.cn/docx/real")
			pool, err := f.db.DB()
			require.NoError(t, err)
			fault := &atomic.Int32{}
			fault.Store(mode)
			faultDB, err := gorm.Open(postgres.New(postgres.Config{Conn: mvpBridgeFaultPool{pool, fault}}), &gorm.Config{})
			require.NoError(t, err)
			f.s.db = faultDB
			f.p.repo = repository.NewPersonalRecoveryRepository(faultDB)
			f.p.repo.FirstCreateOnly = true
			f.p.repo.ApplicationGrantValidator = f.p.validateApplicationIdentityTx
			err = f.p.Process(f.ctx, f.payload(m.AttemptID))
			if mode == 1 {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.NoError(t, f.db.Exec("UPDATE feishu_personal_attempts SET retry_after=clock_timestamp()-interval '1 second' WHERE id=?", m.AttemptID).Error)
				require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
			}
			var k types.Knowledge
			require.NoError(t, f.db.Where("id=?", m.LogicalKnowledgeID).First(&k).Error)
			var count int64
			require.NoError(t, f.db.Model(&types.Knowledge{}).Count(&count).Error)
			require.EqualValues(t, 1, count)
			file, err := f.ks.fileSvc.GetFile(f.ctx, k.FilePath)
			require.NoError(t, err)
			require.NoError(t, file.Close())
		})
	}
}

func TestApplicationGrantNoConnectionStableFirstCreate(t *testing.T) {
	f, _ := newApplicationGrantFixture(t)
	m := f.prepare("https://test.feishu.cn/wiki/node")
	_, _, attempt, err := f.p.repo.Load(f.ctx, m.AttemptID)
	require.NoError(t, err)
	require.Empty(t, attempt.GrantEpoch)
	require.Equal(t, "cli_test", attempt.ApplicationID)
	require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
	var k types.Knowledge
	require.NoError(t, f.db.Where("id = ?", m.LogicalKnowledgeID).First(&k).Error)
	repeat := f.prepare("https://test.feishu.cn/docx/real?ignored=1")
	require.Equal(t, m.LogicalKnowledgeID, repeat.LogicalKnowledgeID)
	require.Equal(t, m.AttemptID, repeat.AttemptID)
	var count int64
	require.NoError(t, f.db.Model(&types.Knowledge{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, f.db.Model(&types.FeishuConnection{}).Count(&count).Error)
	require.Zero(t, count)
	var intent types.TaskPendingOp
	require.NoError(t, f.db.Where("scope_id = ?", k.ID).First(&intent).Error)
	require.Equal(t, types.TypeFeishuApplicationProcess, intent.TaskType)
	var envelope types.KnowledgeDispatch
	require.NoError(t, json.Unmarshal(intent.Payload, &envelope))
	require.Equal(t, 5, envelope.PayloadVersion)
	require.NotEqual(t, types.ParseStatusCompleted, k.ParseStatus)
	require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
	require.EqualValues(t, 1, f.downloads.Load())
}

func TestApplicationGrantRevocationAndConfigChangePreserveKnowledge(t *testing.T) {
	for _, cause := range []string{"source_acl", "config", "system_admin", "kb_allowlist"} {
		t.Run(cause, func(t *testing.T) {
			f, revoked := newApplicationGrantFixture(t)
			m := f.prepare("https://test.feishu.cn/docx/real")
			require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
			var k types.Knowledge
			require.NoError(t, f.db.Where("id = ?", m.LogicalKnowledgeID).First(&k).Error)
			switch cause {
			case "source_acl":
				revoked.Store(true)
			case "config":
				require.NoError(t, f.s.SaveApp(f.ctx, applicationTestConfig(t, "application_grant", "")))
			case "system_admin":
				require.NoError(t, f.db.Exec("UPDATE users SET is_system_admin=false WHERE id='A'").Error)
			case "kb_allowlist":
				t.Setenv("FEISHU_APPLICATION_GRANT_TEST_KB_IDS", "other")
			}
			_ = f.p.Reconcile(context.Background())
			_, _, a, err := f.p.repo.Load(f.ctx, m.AttemptID)
			require.NoError(t, err)
			require.Equal(t, "needs_attention", a.Status)
			require.NoError(t, f.db.Where("id = ?", k.ID).First(&types.Knowledge{}).Error)
			file, err := f.ks.fileSvc.GetFile(f.ctx, k.FilePath)
			require.NoError(t, err)
			require.NoError(t, file.Close())
		})
	}
}

func TestApplicationGrantModeConfusionAndOldTaskRejected(t *testing.T) {
	f, _ := newApplicationGrantFixture(t)
	m := f.prepare("https://test.feishu.cn/docx/real")
	payload := f.payload(m.AttemptID)
	payload.FeishuAuthMode = ""
	payload.FeishuApplicationID = ""
	require.ErrorIs(t, f.p.Process(f.ctx, payload), repository.ErrPersonalIdentity)
	b, err := json.Marshal(f.payload(m.AttemptID))
	require.NoError(t, err)
	require.ErrorIs(t, f.ks.ProcessDocument(f.ctx, asynq.NewTask(types.TypeDocumentProcess, b)), repository.ErrPersonalIdentity)
	// An exact old-protocol mux has no route for this type; it must not process K.
	old := asynq.NewServeMux()
	calls := 0
	old.HandleFunc(types.TypeDocumentProcess, func(context.Context, *asynq.Task) error { calls++; return nil })
	require.Error(t, old.ProcessTask(f.ctx, asynq.NewTask(types.TypeFeishuApplicationProcess, b)))
	require.Zero(t, calls)
	require.Error(t, f.db.Exec("UPDATE feishu_personal_attempts SET grant_epoch='fake-user-grant' WHERE id=?", m.AttemptID).Error)
	require.Error(t, f.db.Exec("UPDATE feishu_personal_imports SET open_id='fake-user' WHERE id=?", m.ID).Error)
}

func TestApplicationGrantConcurrentSameResourceAndChangedRevision(t *testing.T) {
	f, _ := newApplicationGrantFixture(t)
	var wg sync.WaitGroup
	ids := make(chan string, 6)
	errs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, err := f.s.Import(f.ctx, "kb", "https://test.feishu.cn/docx/real")
			if err != nil {
				errs <- err
			} else {
				ids <- m.LogicalKnowledgeID
			}
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	stable := ""
	for id := range ids {
		if stable == "" {
			stable = id
		}
		require.Equal(t, stable, id)
	}
	m := f.prepare("https://test.feishu.cn/docx/real")
	require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
	other := context.WithValue(f.userContext("B"), types.SystemAdminContextKey, true)
	_, err := f.s.Import(other, "kb", "https://test.feishu.cn/docx/real")
	require.ErrorIs(t, err, repository.ErrPersonalAlreadyImported)
	f.revision.Store(2)
	_, err = f.s.Import(f.ctx, "kb", "https://test.feishu.cn/docx/real")
	require.ErrorIs(t, err, repository.ErrPersonalUpdateDisabled)
	var n int64
	require.NoError(t, f.db.Model(&types.Knowledge{}).Count(&n).Error)
	require.EqualValues(t, 1, n)
}

func TestApplicationGrantConfigurationWithoutOAuthCallback(t *testing.T) {
	f := newPersonalRecoveryFixture(t)
	admin := context.WithValue(f.ctx, types.SystemAdminContextKey, true)
	require.NoError(t, f.s.SaveApp(admin, applicationTestConfig(t, "application_grant", "")))
	got, err := f.s.AdminConfig(admin)
	require.NoError(t, err)
	require.Equal(t, "application_grant", got["auth_mode"])
	require.Equal(t, "", got["redirect_uri"])
}

func TestApplicationGrantUnknownIdentityRejected(t *testing.T) {
	f := newPersonalRecoveryFixture(t)
	admin := context.WithValue(f.ctx, types.SystemAdminContextKey, true)
	require.Error(t, f.s.SaveApp(admin, applicationTestConfig(t, "administrator_fallback", "https://test.invalid/feishu/callback")))
}

func TestApplicationGrantDefaultAdmissionClosed(t *testing.T) {
	f := newPersonalRecoveryFixture(t)
	admin := context.WithValue(f.ctx, types.SystemAdminContextKey, true)
	require.NoError(t, f.s.SaveApp(admin, applicationTestConfig(t, "application_grant", "")))
	require.False(t, f.s.Status(f.ctx)["available"].(bool))
	_, err := f.s.Import(f.ctx, "kb", "https://test.feishu.cn/docx/real")
	require.Error(t, err)
}

func TestApplicationGrantNeverStartsPersonalOAuth(t *testing.T) {
	f := newPersonalRecoveryFixture(t)
	admin := context.WithValue(f.ctx, types.SystemAdminContextKey, true)
	require.NoError(t, f.s.SaveApp(admin, applicationTestConfig(t, "application_grant", "")))
	_, err := f.s.Authorize(f.ctx)
	require.Error(t, err)
	var count int64
	require.NoError(t, f.db.Model(&types.FeishuOAuthState{}).Count(&count).Error)
	require.Zero(t, count)
}

// Explicit opt-in real provider test. It proves durable first creation, not
// embedding/index completion. Credentials and downloaded content stay private.
func TestApplicationGrantRealDocxFirstCreate(t *testing.T) {
	path := os.Getenv("FEISHU_APPLICATION_REAL_TEST_CONFIG")
	if path == "" {
		t.Skip("real application test configuration not provided")
	}
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var input struct {
		AppID  string `json:"app_id"`
		Secret string `json:"secret"`
		URL    string `json:"url"`
	}
	require.NoError(t, json.Unmarshal(raw, &input))
	require.NotEmpty(t, input.Secret)
	f, _ := newApplicationGrantFixture(t)
	f.s.newClient = nil
	require.NoError(t, f.s.SaveApp(f.ctx, &FeishuAppConfig{AuthMode: types.FeishuApplicationGrant, AppID: input.AppID, Secret: input.Secret, Enabled: true}))
	m, err := f.s.Import(f.ctx, "kb", input.URL)
	require.NoError(t, err)
	require.Equal(t, "docx", m.ResourceType)
	require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
	var k types.Knowledge
	require.NoError(t, f.db.Where("id = ?", m.LogicalKnowledgeID).First(&k).Error)
	require.Equal(t, m.LogicalKnowledgeID, k.ID)
	require.NotEqual(t, types.ParseStatusCompleted, k.ParseStatus)
	second, err := f.s.Import(f.ctx, "kb", input.URL)
	require.NoError(t, err)
	require.Equal(t, k.ID, second.LogicalKnowledgeID)
	var count int64
	require.NoError(t, f.db.Model(&types.Knowledge{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, f.db.Model(&types.FeishuConnection{}).Count(&count).Error)
	require.Zero(t, count)
	t.Logf("Real application source accepted on stable Knowledge ID %s; parse/index completion NOT_EXECUTED", k.ID)
}
