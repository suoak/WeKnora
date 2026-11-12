package service

import (
	"context"
	"encoding/base64"
	fileService "github.com/Tencent/WeKnora/internal/application/service/file"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Real users, memberships, Keys, KBs and documents in an isolated SQL database.
// Only the asynchronous worker is replaced: draft creation needs no model.
func TestPersonalContentRealDatabaseIsolationAndIdempotency(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "content.db")), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { pool.Close() })
	require.NoError(t, db.AutoMigrate(&types.User{}, &types.Tenant{}, &types.TenantMember{}, &types.KnowledgeBase{}, &types.TenantAPIKey{}, &types.APIKeyTenantScope{}, &types.APIKeyKnowledgeBaseScope{}, &types.Knowledge{}))
	require.NoError(t, db.Exec("CREATE TABLE feishu_private_kbs(kb_id TEXT PRIMARY KEY,owner_id TEXT)").Error)
	require.NoError(t, db.Create(&types.Tenant{ID: 1, Name: "space", Status: "active", StorageEngineConfig: &types.StorageEngineConfig{DefaultProvider: "local"}}).Error)
	for _, u := range []string{"A", "B"} {
		require.NoError(t, db.Create(&types.User{ID: u, Username: u, Email: u + "@synthetic.invalid", IsActive: true}).Error)
		require.NoError(t, db.Create(&types.TenantMember{UserID: u, TenantID: 1, Role: types.TenantRoleContributor, Status: types.TenantMemberStatusActive}).Error)
		owner := u
		require.NoError(t, db.Create(&types.TenantAPIKey{OwnerUserID: &owner, ScopeType: types.APIKeyScopeUserMCP, Name: u, KeyHash: u, PersonalScopeMode: "dynamic_all", Capabilities: types.StringArray{"retrieve", "add_content"}}).Error)
		require.NoError(t, db.Create(&types.KnowledgeBase{ID: u + "-kb", TenantID: 1, Name: u, Type: "document", CreatorID: u}).Error)
	}
	types.EnablePrivateKBGuards(db)
	kbs := repository.NewKnowledgeBaseRepository(db)
	kr := repository.NewKnowledgeRepository(db)
	keys := repository.NewTenantAPIKeyRepository(db).(interfaces.UserMCPAPIKeyRepository)
	svc := &PersonalContent{DB: db, Keys: keys, Members: NewTenantMemberService(repository.NewTenantMemberRepository(db), nil, nil, nil), KBs: kbs, Tenants: NewTenantService(repository.NewTenantRepository(db), nil), Employees: &repository.KnowHubPersonalUserVerifier{DB: db}, Knowledge: &knowledgeService{repo: kr, kbService: &knowledgeBaseService{repo: kbs}}}
	ctxFor := func(user string, key uint64) context.Context {
		ctx := context.WithValue(context.Background(), types.UserIDContextKey, user)
		ctx = types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: user, Role: types.TenantRoleContributor})
		ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: user})
		return types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{KeyID: key, ScopeType: types.APIKeyScopeUserMCP, Capabilities: types.StringArray{"retrieve", "add_content"}})
	}
	a, b := ctxFor("A", 1), ctxFor("B", 2)
	request := PersonalContentRequest{KBID: "A-kb", Title: "new knowledge", Content: "synthetic markdown", IdempotencyKey: "request-0001"}
	rows, err := svc.ListWritable(a)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "A-kb", rows[0].ID)
	_, err = svc.Add(b, request)
	require.Error(t, err, "same workspace must not authorize B")
	row, err := svc.Add(a, request)
	require.NoError(t, err)
	require.NotEmpty(t, row.ID)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			replay, e := svc.Add(a, request)
			require.NoError(t, e)
			if replay != nil {
				require.Equal(t, row.ID, replay.ID)
			}
		}()
	}
	wg.Wait()
	var count int64
	require.NoError(t, db.Table("knowledges").Count(&count).Error)
	require.EqualValues(t, 1, count)
	changed := request
	changed.Content = "different"
	_, err = svc.Add(a, changed)
	require.ErrorIs(t, err, ErrPersonalContentConflict)
	quarantine := t.TempDir()
	t.Setenv("TMPDIR", quarantine)
	t.Setenv("TMP", quarantine)
	t.Setenv("TEMP", quarantine)
	svc.Scanner = personalTestScanner(func(context.Context, []byte) error { return nil })
	storage := t.TempDir()
	t.Setenv("LOCAL_STORAGE_BASE_DIR", storage)
	files := fileService.NewLocalFileService(storage, "")
	svc.Knowledge.(*knowledgeService).fileSvc = files
	svc.Knowledge.(*knowledgeService).task = &createKnowledgeTaskEnqueuerStub{}
	upload := PersonalUploadRequest{KBID: "A-kb", Filename: "upload.txt", ContentBase64: base64.StdEncoding.EncodeToString([]byte("file knowledge")), IdempotencyKey: "upload-0001"}
	_, err = svc.Upload(b, upload)
	require.Error(t, err)
	svc.Scanner = nil // Explicitly deferred antivirus: type and authorization checks still apply.
	uploaded, err := svc.Upload(a, upload)
	require.NoError(t, err)
	require.NotNil(t, uploaded)
	status, err := svc.Status(a, uploaded.ID)
	require.NoError(t, err)
	require.Equal(t, uploaded.ID, status.ID)
	_, err = svc.Status(b, uploaded.ID)
	require.Error(t, err)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			again, e := svc.Upload(a, upload)
			require.NoError(t, e)
			if again != nil {
				require.Equal(t, uploaded.ID, again.ID)
			}
		}()
	}
	wg.Wait()
	require.NoError(t, db.Exec("CREATE TRIGGER reject_upload BEFORE INSERT ON knowledges WHEN NEW.file_name='reject.txt' BEGIN SELECT RAISE(ABORT,'synthetic insert failure'); END").Error)
	rejected := upload
	rejected.Filename = "reject.txt"
	rejected.IdempotencyKey = "upload-rejected-0001"
	_, err = svc.Upload(a, rejected)
	require.Error(t, err)
	var rejectedFiles []string
	require.NoError(t, filepath.Walk(storage, func(p string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if !info.IsDir() && info.Name() == "reject.txt" {
			rejectedFiles = append(rejectedFiles, p)
		}
		return nil
	}))
	require.Empty(t, rejectedFiles, "known failed INSERT cleans the saved file")
	entries, err := os.ReadDir(quarantine)
	require.NoError(t, err)
	require.Empty(t, entries, "quarantine files removed after all replays")
	bad := upload
	bad.Filename = "other.txt"
	_, err = svc.Upload(a, bad)
	require.ErrorIs(t, err, ErrPersonalContentConflict)
	require.NoError(t, db.Table("knowledges").Count(&count).Error)
	require.EqualValues(t, 2, count)
	require.NoError(t, db.Exec("UPDATE tenant_members SET status='suspended' WHERE user_id='A'").Error)
	_, err = svc.Add(a, request)
	require.Error(t, err, "replay must recheck membership")
	require.NoError(t, db.Exec("UPDATE tenant_members SET status='active' WHERE user_id='A'").Error)
	require.NoError(t, db.Exec("UPDATE tenant_api_keys SET capabilities='[\"retrieve\"]' WHERE id=1").Error)
	_, err = svc.Add(a, request)
	require.Error(t, err, "replay must recheck capability")
}
