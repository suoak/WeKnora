//go:build anydoc && personal_exact_live

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// Uses the original parser, vector writer and atomic publisher's real PG output.
// No success state or receipt is manufactured by this regression matrix.
func exercisePublishedSnapshotReaderMatrix(t *testing.T, f *personalRecoveryFixture, knowledge string) {
	t.Helper()
	var remoteCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		remoteCalls.Add(1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	oldClient := f.s.newClient
	f.s.newClient = func(cfg *core.Config) *core.Client { cfg.BaseURL = server.URL; return core.NewClient(cfg) }
	defer func() { f.s.newClient = oldClient }()
	viewer := types.WithPrincipal(f.userContext("B"), types.Principal{Type: types.PrincipalWebUser, ID: "B"})
	viewer = types.WithExecutionTenant(viewer, 1)
	conditions := []struct {
		name, sql string
		allow     bool
	}{
		{"normal", "", true},
		{"token_expired", "UPDATE feishu_connections SET expires_at=now()-interval '1 day' WHERE user_id='A'", true},
		{"refresh_expired", "UPDATE feishu_connections SET refresh_expires_at=now()-interval '1 day' WHERE user_id='A'", true},
		{"disconnected", "DELETE FROM feishu_connections WHERE user_id='A'", true},
		{"grant_rotated", "UPDATE feishu_personal_grant_epochs SET epoch='new-source-grant' WHERE user_id='A'", true},
		{"grant_deleted", "DELETE FROM feishu_personal_grant_epochs WHERE user_id='A'", true},
		{"importer_disabled", "UPDATE users SET is_active=false WHERE id='A'", true},
		{"sync_needs_attention", "UPDATE feishu_personal_imports SET status='needs_attention' WHERE user_id='A'", true},
		{"viewer_disabled", "UPDATE users SET is_active=false WHERE id='B'", false},
		{"viewer_removed", "DELETE FROM tenant_members WHERE user_id='B'", false},
		{"space_disabled", "UPDATE tenants SET status='disabled' WHERE id=1", false},
		{"receipt_staging", "UPDATE task_pending_ops SET op='staging' WHERE scope='personal_exact_receipt_v1'", false},
		{"receipt_missing", "DELETE FROM task_pending_ops WHERE scope='personal_exact_receipt_v1'", false},
		{"receipt_tampered", "UPDATE task_pending_ops SET payload=jsonb_set(payload::jsonb,'{grant_epoch}','\"forged\"'::jsonb) WHERE scope='personal_exact_receipt_v1'", false},
		{"chunk_tampered", "UPDATE chunks SET content='changed'", false},
		{"vector_tampered", "UPDATE embeddings SET is_enabled=false", false},
		{"file_missing", "UPDATE resources SET physical_path='local://1/missing-published-snapshot.docx'", false},
		{"binding_missing", "DELETE FROM resource_bindings", false},
		{"knowledge_withdrawn", "UPDATE knowledges SET enable_status='disabled'", false},
		{"active_attempt", "UPDATE feishu_personal_attempts SET owner_nonce='active',lease_until=now()+interval '1 minute'", false},
	}
	for _, condition := range conditions {
		t.Run(condition.name, func(t *testing.T) {
			tx := f.db.Begin()
			require.NoError(t, tx.Error)
			defer tx.Rollback()
			// Fault injection in the disposable test schema only; never bypass a
			// production guard or use a forged successful publication fixture.
			require.NoError(t, tx.Exec("SET LOCAL session_replication_role=replica").Error)
			if condition.sql != "" {
				require.NoError(t, tx.Exec(condition.sql).Error)
			}
			personal, recovery := *f.s, *f.p
			personal.db = tx
			recovery.service, recovery.repo = &personal, repository.NewPersonalRecoveryRepository(tx)
			read, err := recovery.exactPublishedReadContext(viewer, knowledge)
			if !condition.allow {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			present, err := types.CheckExactPublishedDocumentRead(read)
			require.NoError(t, err)
			require.True(t, present)
			var count int64
			require.NoError(t, tx.Table("chunks").Where("knowledge_id=?", knowledge).Count(&count).Error)
			require.Positive(t, count)
		})
	}
	t.Run("reader_revoked_during_output", func(t *testing.T) {
		tx := f.db.Begin()
		defer tx.Rollback()
		personal, recovery := *f.s, *f.p
		personal.db = tx
		recovery.service, recovery.repo = &personal, repository.NewPersonalRecoveryRepository(tx)
		read, err := recovery.exactPublishedReadContext(viewer, knowledge)
		require.NoError(t, err)
		err = types.ReadExactPublishedPage(read, knowledge, func(context.Context) error {
			return tx.Exec("UPDATE tenant_members SET status='disabled' WHERE user_id='B'").Error
		})
		require.Error(t, err)
	})
	t.Run("concurrent_reader_revocation_after_pg_selection", func(t *testing.T) {
		read, err := f.p.exactPublishedReadContext(viewer, knowledge)
		require.NoError(t, err)
		selected, release := make(chan struct{}), make(chan struct{})
		var releaseOnce sync.Once
		defer releaseOnce.Do(func() { close(release) })
		done := make(chan error, 1)
		go func() {
			done <- types.ReadExactPublishedPage(read, knowledge, func(selection context.Context) error {
				if _, err := f.ks.repo.GetKnowledgeByID(selection, 1, knowledge); err != nil {
					return err
				}
				close(selected)
				<-release
				return nil
			})
		}()
		select {
		case <-selected:
		case err := <-done:
			t.Fatalf("read did not reach PostgreSQL selection: %v", err)
		case <-time.After(10 * time.Second):
			t.Fatal("read selection timed out")
		}
		require.NoError(t, f.db.Exec("UPDATE tenant_members SET status='disabled' WHERE user_id='B' AND tenant_id=1").Error)
		defer func() {
			require.NoError(t, f.db.Exec("UPDATE tenant_members SET status='active' WHERE user_id='B' AND tenant_id=1").Error)
		}()
		releaseOnce.Do(func() { close(release) })
		require.Error(t, <-done, "content selected before revocation must not pass the final output check")
	})
	t.Run("published_reconcile_after_disconnect", func(t *testing.T) {
		tx := f.db.Begin()
		defer tx.Rollback()
		personal, recovery := *f.s, *f.p
		personal.db = tx
		recovery.service, recovery.repo = &personal, repository.NewPersonalRecoveryRepository(tx)
		var receipt types.TaskPendingOp
		require.NoError(t, tx.Where("scope=? AND scope_id=?", types.PersonalExactReceiptScope, knowledge).Take(&receipt).Error)
		var published types.PersonalExactReceipt
		require.NoError(t, json.Unmarshal(receipt.Payload, &published))
		require.NoError(t, personal.Disconnect(f.ctx))
		m, before, a, err := recovery.repo.Load(viewer, published.AttemptID)
		require.NoError(t, err)
		handled, err := recovery.reconcileExactPublishedSnapshot(context.Background(), m, a)
		require.True(t, handled)
		require.Error(t, err, "source synchronization still requires the importer's grant")
		_, after, attempt, err := recovery.repo.Load(viewer, published.AttemptID)
		require.NoError(t, err)
		require.Equal(t, "indexed", attempt.Status)
		require.Equal(t, before.Generation, after.Generation)
		require.Equal(t, before.ActiveVersionID, after.ActiveVersionID)
		_, err = recovery.exactPublishedReadContext(viewer, knowledge)
		require.NoError(t, err)
	})
	for _, acl := range []bool{false, true} {
		t.Run(fmt.Sprintf("public_services_document_acl_%t", acl), func(t *testing.T) {
			oldPolicy := f.ks.documentACL
			f.ks.documentACL = NewDocumentReadPolicy(acl, nil)
			defer func() { f.ks.documentACL = oldPolicy }()
			row, err := f.ks.GetKnowledgeByID(viewer, knowledge)
			require.NoError(t, err)
			require.Equal(t, knowledge, row.ID)
			page, err := f.ks.ListPagedKnowledgeByKnowledgeBaseID(viewer, "kb", &types.Pagination{Page: 1, PageSize: 10}, types.KnowledgeListFilter{})
			require.NoError(t, err)
			require.NotEmpty(t, page.Data)
			listed, err := f.ks.ListKnowledgeByKnowledgeBaseID(viewer, "kb")
			require.NoError(t, err)
			require.NotEmpty(t, listed)
			file, _, err := f.ks.GetKnowledgeFile(viewer, knowledge)
			require.NoError(t, err)
			data, err := io.ReadAll(file)
			require.NoError(t, err)
			require.NoError(t, file.Close())
			require.NotEmpty(t, data)
			chunks := &chunkService{personalRecovery: f.p, documentACL: NewDocumentReadPolicy(acl, nil), chunkRepository: f.ks.chunkRepo}
			rows, err := chunks.ListChunksByKnowledgeID(viewer, knowledge)
			require.NoError(t, err)
			require.NotEmpty(t, rows)
			_, err = chunks.GetChunkByID(viewer, rows[0].ID)
			require.NoError(t, err)
			_, err = chunks.ListPagedChunksByKnowledgeID(viewer, knowledge, &types.Pagination{Page: 1, PageSize: 10}, nil)
			require.NoError(t, err)
		})
	}
	t.Run("mcp_user_key_live_scope", func(t *testing.T) {
		tx := f.db.Begin()
		defer tx.Rollback()
		require.NoError(t, tx.AutoMigrate(&types.TenantAPIKey{}, &types.APIKeyTenantScope{}, &types.APIKeyKnowledgeBaseScope{}))
		personal, recovery := *f.s, *f.p
		personal.db = tx
		recovery.service, recovery.repo = &personal, repository.NewPersonalRecoveryRepository(tx)
		owner := "B"
		expires := time.Now().Add(time.Hour)
		key := &types.TenantAPIKey{
			OwnerUserID: &owner, ScopeType: types.APIKeyScopeUserMCP,
			Name: "published-reader-test", KeyHash: "published-reader-test-only", ExpiresAt: &expires,
			Capabilities: types.StringArray{string(types.APIKeyCapabilityRetrieve)},
			TenantScopes: []types.APIKeyTenantScope{{
				TenantID: 1, KBScopeMode: types.APIKeyKBScopeSelected,
				KnowledgeBases: []types.APIKeyKnowledgeBaseScope{{TenantID: 1, KnowledgeBaseID: "kb", SourceType: types.APIKeyKBSourceOwned}},
			}},
		}
		require.NoError(t, repository.NewTenantAPIKeyRepository(tx).CreateAPIKey(viewer, key))
		keyed := types.WithTenantAPIKeyScope(viewer, types.TenantAPIKeyScope{
			KeyID: key.ID, ScopeType: types.APIKeyScopeUserMCP,
			Capabilities: key.Capabilities, KnowledgeBaseRestricted: true, KnowledgeBaseIDs: types.StringArray{"kb"},
		})
		read, err := recovery.exactPublishedReadContext(keyed, knowledge)
		require.NoError(t, err)
		require.NoError(t, tx.Model(&types.TenantAPIKey{}).Where("id=?", key.ID).Update("revoked_at", time.Now()).Error)
		_, err = types.CheckExactPublishedDocumentRead(read)
		require.Error(t, err)
		_, err = recovery.exactPublishedReadContext(keyed, knowledge)
		require.Error(t, err)
	})
	t.Run("recovery_switch_does_not_revoke_published_asset", func(t *testing.T) {
		t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY", "0")
		_, err := f.ks.GetKnowledgeByID(viewer, knowledge)
		require.NoError(t, err)
	})
	require.Zero(t, remoteCalls.Load(), "published reads must never call Feishu, including unavailable/deleted sources")
	// Receipt and index identity remain unchanged throughout rolled-back faults.
	var receipt types.TaskPendingOp
	require.NoError(t, f.db.Where("scope=? AND scope_id=?", types.PersonalExactReceiptScope, knowledge).Take(&receipt).Error)
	var published types.PersonalExactReceipt
	require.NoError(t, json.Unmarshal(receipt.Payload, &published))
	require.Equal(t, knowledge, published.KnowledgeID)
	require.NoError(t, f.p.verifyExactPublishedDatabase(viewer, published))
	t.Log(fmt.Sprintf("published snapshot matrix: %d cases; remote Feishu requests=0; original knowledge=%s", len(conditions)+1, knowledge))
}
