package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func pendingExactFixture(
	t *testing.T,
) (*personalRecoveryFixture, *types.FeishuPersonalAttempt, types.KnowledgeDispatch) {
	t.Helper()
	f, a := exactSourceFixture(t, false)
	t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY", "1")
	t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY_KNOWLEDGE_IDS", a.LogicalKnowledgeID)
	require.NoError(t, f.db.Exec("UPDATE task_pending_ops SET op='pending' WHERE scope=? AND scope_id=?",
		types.KnowledgeDispatchScope, a.LogicalKnowledgeID).Error)
	var op types.TaskPendingOp
	require.NoError(t, f.db.Where("scope=? AND scope_id=?",
		types.KnowledgeDispatchScope, a.LogicalKnowledgeID).Take(&op).Error)
	var intent types.KnowledgeDispatch
	require.NoError(t, json.Unmarshal(op.Payload, &intent))
	// The fixture replaces a direct locator with the canonical resource binding;
	// mirror that identity in its durable envelope, as in the production protocol.
	intent.Task.FilePath = types.BuildResourcePath("abcdefghijklmnopqrstuv")
	payload, err := json.Marshal(intent)
	require.NoError(t, err)
	require.NoError(t, f.db.Model(&types.TaskPendingOp{}).Where("id=?", op.ID).Update("payload", payload).Error)
	return f, a, intent
}

func exactPendingSnapshot(t *testing.T, f *personalRecoveryFixture, id string) []byte {
	t.Helper()
	m, r, a, err := f.p.repo.Load(context.Background(), id)
	require.NoError(t, err)
	var ops []types.TaskPendingOp
	require.NoError(t, f.db.Order("id").Find(&ops).Error)
	data, err := json.Marshal([]any{m, r, a, ops})
	require.NoError(t, err)
	return data
}

func exactPreflightRemote(t *testing.T, f *personalRecoveryFixture, revision string, allowed bool) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer access-A" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/open-apis/drive/v1/metas/batch_query":
			_, _ = fmt.Fprintf(w, `{"code":0,"data":{"metas":[{"doc_type":"file",`+
				`"doc_token":"real","latest_modify_time":%q}]}}`, revision)
		case "/open-apis/drive/v1/permissions/real/members/auth":
			_, _ = fmt.Fprintf(w, `{"code":0,"data":{"auth_result":%t}}`, allowed)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	f.s.newClient = func(cfg *core.Config) *core.Client {
		cfg.BaseURL = server.URL
		return core.NewClient(cfg)
	}
}

func TestPersonalRecoveryExactPreflightPreservesPending(t *testing.T) {
	for _, change := range []string{"valid", "acl", "revision", "manifest", "hash", "binding", "grant", "generation"} {
		t.Run(change, func(t *testing.T) {
			f, a, intent := pendingExactFixture(t)
			switch change {
			case "acl":
				exactPreflightRemote(t, f, "1", false)
			case "revision":
				exactPreflightRemote(t, f, "2", true)
			case "manifest":
				require.NoError(t, f.db.Exec("UPDATE feishu_personal_attempts SET manifest_hash='changed'").Error)
			case "hash":
				require.NoError(t, f.db.Exec("UPDATE resources SET content_hash=repeat('a',64)").Error)
			case "binding":
				require.NoError(t, f.db.Exec("UPDATE resource_bindings SET owner_id='other'").Error)
			case "grant":
				require.NoError(t, f.db.Exec("UPDATE feishu_personal_grant_epochs SET epoch='revoked'").Error)
			case "generation":
				require.NoError(t, f.db.Exec("UPDATE feishu_personal_resources SET generation=generation+1").Error)
			}
			before := exactPendingSnapshot(t, f, a.ID)
			ready, err := f.p.firstCreateDispatchable(context.Background(), intent)
			if change == "valid" {
				require.NoError(t, err)
				require.True(t, ready)
			} else {
				require.Error(t, err)
				require.False(t, ready)
				require.Error(t, f.p.processFirstCreateDocument(context.Background(), intent.Task))
				// The reconciler must not turn the same rejected preflight into a
				// terminal attempt or close the durable pending operation.
				require.Error(t, f.p.Reconcile(context.Background()))
			}
			require.JSONEq(t, string(before), string(exactPendingSnapshot(t, f, a.ID)))
			var body types.Knowledge
			require.ErrorIs(t, f.db.Where("id=?", a.LogicalKnowledgeID).First(&body).Error,
				types.ErrLegacySourceRestricted)
		})
	}
}

func TestPersonalRecoveryExactPreflightClaimCAS(t *testing.T) {
	for _, change := range []string{"valid", "generation", "revision", "grant", "binding", "member", "concurrent"} {
		t.Run(change, func(t *testing.T) {
			f, a, _ := pendingExactFixture(t)
			preflight, err := f.p.preflightExactPersonalDocument(context.Background(), a)
			require.NoError(t, err)
			switch change {
			case "generation":
				require.NoError(t, f.db.Exec("UPDATE feishu_personal_resources SET generation=generation+1").Error)
			case "revision":
				require.NoError(t, f.db.Exec("UPDATE feishu_personal_attempts SET revision='2'").Error)
			case "grant":
				require.NoError(t, f.db.Exec("UPDATE feishu_personal_grant_epochs SET epoch='revoked'").Error)
			case "binding":
				require.NoError(t, f.db.Exec("UPDATE resource_bindings SET owner_id='other'").Error)
			case "member":
				require.NoError(t, f.db.Exec("UPDATE tenant_members SET status='inactive'").Error)
			case "concurrent":
				_, err = f.p.claimExactPreflight(context.Background(), preflight)
				require.NoError(t, err)
			}
			before := exactPendingSnapshot(t, f, a.ID)
			owner, err := f.p.claimExactPreflight(context.Background(), preflight)
			if change != "valid" {
				require.Error(t, err)
				require.JSONEq(t, string(before), string(exactPendingSnapshot(t, f, a.ID)))
				return
			}
			require.NoError(t, err)
			require.Equal(t, a.Fence+1, owner.Fence)
			require.NotEmpty(t, owner.OwnerNonce)
			require.NotNil(t, owner.LeaseUntil)
			// Remote revision can change after any successful preflight: the
			// retained post-Claim proof must still reject execution/publication.
			exactPreflightRemote(t, f, "2", true)
			proof, err := f.p.prepareExactPersonalDocument(context.Background(), owner)
			require.Error(t, err)
			require.Nil(t, proof)
		})
	}
}

func TestPersonalRecoveryExactPreflightConcurrentClaims(t *testing.T) {
	f, a, _ := pendingExactFixture(t)
	preflight, err := f.p.preflightExactPersonalDocument(context.Background(), a)
	require.NoError(t, err)
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, claimErr := f.p.claimExactPreflight(context.Background(), preflight)
			results <- claimErr
		}()
	}
	close(start)
	successes := 0
	for range 2 {
		if <-results == nil {
			successes++
		}
	}
	require.Equal(t, 1, successes)
	_, resource, current, err := f.p.repo.Load(context.Background(), a.ID)
	require.NoError(t, err)
	require.Equal(t, a.Fence+1, resource.Generation)
	require.Equal(t, resource.Generation, current.Fence)
}
