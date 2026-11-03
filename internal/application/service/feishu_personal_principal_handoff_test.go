package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func seedUnpublishedApplication(t *testing.T, f *personalRecoveryFixture) (types.FeishuPersonalImport, types.FeishuPersonalAttempt, types.FeishuPersonalResource) {
	t.Helper()
	m := types.FeishuPersonalImport{ID: uuid.NewString(), UserID: "A", AppID: "cli_test", KBID: "kb", ResourceType: "file", DocumentToken: "real",
		AuthMode: types.FeishuApplicationGrant, ApplicationID: "cli_test", Status: "needs_attention", URL: "https://test.feishu.cn/file/real"}
	r := types.FeishuPersonalResource{ImportID: m.ID, LogicalKnowledgeID: uuid.NewString(), Generation: 3}
	a := types.FeishuPersonalAttempt{ID: uuid.NewString(), ImportID: m.ID, LogicalKnowledgeID: r.LogicalKnowledgeID, CandidateVersionID: uuid.NewString(),
		Revision: "1", CallerTenantID: 1, AppConfigVersion: "old-application", AuthMode: types.FeishuApplicationGrant, ApplicationID: "cli_test",
		Status: "needs_attention", LastErrorCategory: "authorization_revalidation_failed", Fence: 2}
	r.CurrentAttemptID = a.ID
	r.CandidateVersionID = a.CandidateVersionID
	require.NoError(t, f.db.Create(&m).Error)
	require.NoError(t, f.db.Create(&r).Error)
	require.NoError(t, f.db.Create(&a).Error)
	require.NoError(t, f.db.Create(&types.TaskPendingOp{TenantID: 1, Scope: types.KnowledgeDispatchScope, ScopeID: a.CandidateVersionID,
		TaskType: types.TypeFeishuApplicationProcess, Op: "attention", DedupKey: "old-" + a.ID, Payload: json.RawMessage(`{}`)}).Error)
	ddl, err := os.ReadFile("../../../migrations/versioned/003045_feishu_principal_history.up.sql")
	require.NoError(t, err)
	require.NoError(t, f.db.Exec(string(ddl)).Error)
	require.NoError(t, f.db.AutoMigrate(&types.AuditLog{}))
	return m, a, r
}

func handoffPersonalRequest(t *testing.T, f *personalRecoveryFixture) (*types.FeishuPersonalImport, *types.FeishuPersonalAttempt) {
	t.Helper()
	cfg, err := f.s.app(f.ctx)
	require.NoError(t, err)
	var epoch types.FeishuPersonalGrantEpoch
	require.NoError(t, f.db.First(&epoch, "user_id=?", "A").Error)
	return &types.FeishuPersonalImport{ID: uuid.NewString(), UserID: "A", AppID: "cli_test", KBID: "kb", ResourceType: "file", DocumentToken: "real",
			OpenID: "open-A", AuthMode: types.FeishuUserOAuth, Status: "accepted", URL: "https://test.feishu.cn/file/real"},
		&types.FeishuPersonalAttempt{AuthMode: types.FeishuUserOAuth, CallerTenantID: 1, AppConfigVersion: cfg.identityVersion(types.FeishuUserOAuth), GrantEpoch: epoch.Epoch, Revision: "1"}
}

func TestUnpublishedPersonalHandoffPreservesIDsHistoryAndDeduplicates(t *testing.T) {
	f := v2PostgresFixture(t)
	old, attempt, resource := seedUnpublishedApplication(t, f)
	t.Setenv("FEISHU_PERSONAL_UNPUBLISHED_HANDOFF_IDS", old.ID)
	var wg sync.WaitGroup
	ids := make(chan string, 2)
	errs := make(chan error, 2)
	for n := 0; n < 2; n++ {
		m, a := handoffPersonalRequest(t, f)
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, next, err := f.p.repo.Prepare(f.ctx, m, a)
			if err != nil {
				errs <- err
				return
			}
			ids <- next.ID
			if r.LogicalKnowledgeID != resource.LogicalKnowledgeID {
				errs <- repository.ErrPersonalIdentity
			}
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var all []string
	for id := range ids {
		all = append(all, id)
	}
	require.Len(t, all, 2)
	require.Equal(t, all[0], all[1])
	var stored types.FeishuPersonalImport
	require.NoError(t, f.db.First(&stored, "id=?", old.ID).Error)
	require.Equal(t, types.FeishuUserOAuth, stored.AuthMode)
	require.Empty(t, stored.KnowledgeID)
	var historical types.FeishuPersonalAttempt
	require.NoError(t, f.db.First(&historical, "id=?", attempt.ID).Error)
	require.Equal(t, types.FeishuApplicationGrant, historical.AuthMode)
	require.Equal(t, attempt.ApplicationID, historical.ApplicationID)
	require.Equal(t, attempt.CandidateVersionID, historical.CandidateVersionID)
	require.Equal(t, "obsolete", historical.Status)
	var count int64
	require.NoError(t, f.db.Table("feishu_import_principal_history").Where("import_id=?", old.ID).Count(&count).Error)
	require.EqualValues(t, 2, count)
	require.NoError(t, f.db.Model(&types.AuditLog{}).Where("action=?", "feishu.unpublished_principal_handoff").Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, f.db.Model(&types.FeishuPersonalImport{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	// Historical principal is no longer authority to insert another old-mode attempt.
	forged := attempt
	forged.ID = uuid.NewString()
	forged.CandidateVersionID = uuid.NewString()
	require.Error(t, f.db.Create(&forged).Error)
	// A rollback must fail rather than alter the preserved historical identities.
	ddl, err := os.ReadFile("../../../migrations/versioned/003045_feishu_principal_history.down.sql")
	require.NoError(t, err)
	require.Error(t, f.db.Exec(string(ddl)).Error)
}

func TestUnpublishedPersonalHandoffRefusesUnsafeStateWithoutMutation(t *testing.T) {
	for _, name := range []string{"unapproved", "other_owner", "held", "leased", "partial_chunks", "published", "claimed_outbox", "revoked_epoch"} {
		t.Run(name, func(t *testing.T) {
			f := v2PostgresFixture(t)
			old, attempt, resource := seedUnpublishedApplication(t, f)
			t.Setenv("FEISHU_PERSONAL_UNPUBLISHED_HANDOFF_IDS", old.ID)
			m, a := handoffPersonalRequest(t, f)
			switch name {
			case "unapproved":
				t.Setenv("FEISHU_PERSONAL_UNPUBLISHED_HANDOFF_IDS", "")
			case "other_owner":
				m.UserID = "B"
				m.OpenID = "open-B"
			case "held":
				require.NoError(t, f.db.Model(&old).Update("status", "hold").Error)
			case "leased":
				require.NoError(t, f.db.Model(&attempt).Update("lease_until", time.Now().Add(time.Minute)).Error)
			case "partial_chunks":
				require.NoError(t, f.db.Create(&types.Chunk{ID: uuid.NewString(), KnowledgeID: resource.LogicalKnowledgeID, KnowledgeBaseID: "kb", TenantID: 1}).Error)
			case "published":
				require.NoError(t, f.db.Model(&resource).Update("active_version_id", attempt.CandidateVersionID).Error)
			case "claimed_outbox":
				require.NoError(t, f.db.Model(&types.TaskPendingOp{}).Where("scope_id=?", attempt.CandidateVersionID).Update("claimed_at", time.Now()).Error)
			case "revoked_epoch":
				a.GrantEpoch = "revoked"
			}
			var before string
			require.NoError(t, f.db.Raw("SELECT row_to_json(i)::text FROM feishu_personal_imports i WHERE id=?", old.ID).Scan(&before).Error)
			_, _, err := f.p.repo.Prepare(f.ctx, m, a)
			require.Error(t, err)
			var after string
			require.NoError(t, f.db.Raw("SELECT row_to_json(i)::text FROM feishu_personal_imports i WHERE id=?", old.ID).Scan(&after).Error)
			require.JSONEq(t, before, after)
			var count int64
			require.NoError(t, f.db.Model(&types.AuditLog{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestUnpublishedPersonalHandoffUsesOnlyCurrentPersonalPermissions(t *testing.T) {
	f := v2PostgresFixture(t)
	old, _, _ := seedUnpublishedApplication(t, f)
	t.Setenv("FEISHU_PERSONAL_UNPUBLISHED_HANDOFF_IDS", old.ID)
	m, a := handoffPersonalRequest(t, f)
	for _, denied := range []string{"view", "copy", "export", "none"} {
		t.Run(denied, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "Bearer access-A", r.Header.Get("Authorization"))
				if r.Method == http.MethodGet {
					_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]bool{"auth_result": r.URL.Query().Get("action") != denied}})
					return
				}
				_, _ = io.WriteString(w, `{"code":0,"data":{"metas":[{"doc_type":"file","doc_token":"real","title":"test.pdf","latest_modify_time":"1"}]}}`)
			}))
			defer server.Close()
			f.s.newClient = func(cfg *core.Config) *core.Client { cfg.BaseURL = server.URL; return core.NewClient(cfg) }
			remote, err := f.p.authorize(context.Background(), m, a)
			require.NoError(t, err)
			err = f.p.preflightPersonalHandoff(remote, m, core.PersonalResource{Type: "file", Token: "real", Modified: "1"})
			if denied == "none" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
