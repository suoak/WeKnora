package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	pgretrieve "github.com/Tencent/WeKnora/internal/application/repository/retriever/postgres"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func personalDeletionWebContext(user string) context.Context {
	ctx := context.WithValue(personalUser(user), types.TenantIDContextKey, uint64(1))
	ctx = types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: user, Role: types.TenantRoleAdmin})
	return types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: user})
}

func TestPersonalDocumentDeletionMetadataAndFence(t *testing.T) {
	for _, state := range []string{"processing", "needs_attention", "stale metadata", "source managed"} {
		t.Run(state, func(t *testing.T) {
			f, proof, execution := exactExecutionFixture(t)
			require.NoError(t, f.p.beginExactPersonalReceipt(execution, proof))
			if state == "needs_attention" {
				require.NoError(t, f.db.Exec(`UPDATE feishu_personal_attempts SET
status='needs_attention',owner_nonce='',lease_until=NULL WHERE id=?`, proof.attempt).Error)
			}
			if state == "source managed" {
				require.NoError(t, f.db.Exec("UPDATE feishu_personal_imports SET status='source_managed' WHERE id=?",
					proof.importID).Error)
			}
			ctx := personalDeletionWebContext("A")
			ctx = types.WithKnowledgeDeletionMetadata(ctx, proof.knowledge)
			row, err := f.ks.GetKnowledgeDeletionMetadata(ctx, proof.knowledge)
			require.NoError(t, err)
			require.Empty(t, row.Description)
			require.Nil(t, row.Profile)
			_, err = f.ks.GetKnowledgeDeletionMetadata(ctx, "unrelated")
			require.Error(t, err)
			_, err = f.ks.GetKnowledgeByIDOnly(context.Background(), proof.knowledge)
			require.Error(t, err, "deletion metadata must not open content reads")
			if state == "source managed" {
				require.Error(t, f.ks.repo.MarkKnowledgeDeleting(ctx, row))
				attempt := mustPersonalAttempt(t, f, proof.attempt)
				require.Equal(t, "processing", attempt.Status)
				require.NotEmpty(t, attempt.OwnerNonce)
				return
			}
			if state == "stale metadata" {
				require.NoError(t, f.db.Exec("UPDATE knowledges SET updated_at=updated_at+interval '1 second' "+
					"WHERE id=?",
					proof.knowledge).Error)
				require.Error(t, f.ks.repo.MarkKnowledgeDeleting(ctx, row))
				attempt := mustPersonalAttempt(t, f, proof.attempt)
				require.Equal(t, "processing", attempt.Status, "failed CAS must roll back the protocol fence")
				require.NotEmpty(t, attempt.OwnerNonce)
				return
			}
			require.NoError(t, f.ks.repo.MarkKnowledgeDeleting(ctx, row))
			require.NoError(t, f.ks.repo.MarkKnowledgeDeleting(ctx, row), "cleanup retries are idempotent")
			attempt := mustPersonalAttempt(t, f, proof.attempt)
			require.Equal(t, "obsolete", attempt.Status)
			require.Empty(t, attempt.OwnerNonce)
			require.Nil(t, attempt.LeaseUntil)
			require.Error(t, f.ks.repo.UpdateKnowledgeColumns(execution, proof.knowledge,
				map[string]interface{}{"description": "stale write"}))
			var ops []types.TaskPendingOp
			require.NoError(t, f.db.Where("scope_id=?", proof.knowledge).Find(&ops).Error)
			for _, op := range ops {
				require.Equal(t, "obsolete", op.Op)
			}
		})
	}
}

func TestPersonalDocumentDeletionMetadataRejectsOtherOwnersAndTenants(t *testing.T) {
	f, proof, execution := exactExecutionFixture(t)
	require.NoError(t, f.p.beginExactPersonalReceipt(execution, proof))
	require.NoError(t, f.db.Exec("CREATE TABLE feishu_private_kbs(kb_id text,owner_id text)").Error)
	require.NoError(t, f.db.Exec("INSERT INTO feishu_private_kbs VALUES('kb','A')").Error)
	types.EnablePrivateKBGuards(f.db)
	for _, user := range []string{"A", "B"} {
		ctx := personalDeletionWebContext(user)
		ctx = types.WithKnowledgeDeletionMetadata(ctx, proof.knowledge)
		_, err := f.ks.GetKnowledgeDeletionMetadata(ctx, proof.knowledge)
		if user == "A" {
			require.NoError(t, err)
			_, err = f.ks.GetKnowledgeDeletionMetadata(types.WithExecutionTenant(ctx, 2), proof.knowledge)
		}
		require.Error(t, err, "no foreign tenant or private-owner projection may be returned")
	}
}

func TestPersonalDocumentDeletionWorkerRemovesUnpublishedArtifacts(t *testing.T) {
	f, proof, execution := exactExecutionFixture(t)
	require.NoError(t, f.p.beginExactPersonalReceipt(execution, proof))
	require.NoError(t, f.db.AutoMigrate(&types.KnowledgeTagRelation{}, &types.KnowledgeTag{}, &types.ChunkRevision{}))
	require.NoError(t, f.db.Exec("CREATE TABLE feishu_private_kbs(kb_id text,owner_id text)").Error)
	require.NoError(t, f.db.Exec("INSERT INTO feishu_private_kbs VALUES('kb','A')").Error)
	types.EnablePrivateKBGuards(f.db)
	f.ks.personalRecovery = f.p
	f.ks.chunkRepo = repository.NewChunkRepository(f.db)
	f.ks.graphEngine = &documentGraphSpy{}
	f.ks.tenantRepo = &documentTenantSpy{}
	ctx := personalDeletionWebContext("A")
	require.NoError(t, f.db.WithContext(ctx).Create(&types.Chunk{
		ID: "unpublished", TenantID: 1, KnowledgeID: proof.knowledge, KnowledgeBaseID: proof.kb,
		Content: "retained partial text", IsEnabled: false,
	}).Error)
	payload := types.KnowledgeListDeletePayload{
		TenantID: 1, KnowledgeBaseID: proof.kb,
		KnowledgeIDs: []string{proof.knowledge}, Initiator: types.TaskInitiator{UserID: "A"},
	}
	require.NoError(t, f.ks.ProcessKnowledgeListDelete(context.Background(), deleteDocumentTask(t, payload)))
	require.NoError(t, f.ks.ProcessKnowledgeListDelete(context.Background(), deleteDocumentTask(t, payload)))
	var count int64
	require.NoError(t, f.db.WithContext(ctx).Model(&types.Knowledge{}).
		Where("id=?", proof.knowledge).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, f.db.WithContext(ctx).Model(&types.Chunk{}).
		Where("knowledge_id=?", proof.knowledge).Count(&count).Error)
	require.Zero(t, count)
	imports, err := f.s.ListImports(ctx, proof.kb)
	require.NoError(t, err, "deleted history must not trigger source reads during catalog polling")
	require.Empty(t, imports)
}

func TestPersonalDocumentDeleteActorUsesLiveMembership(t *testing.T) {
	f := newMVPFixture(t)
	f.ks.personalRecovery = f.p
	ctx := types.TaskInitiator{UserID: "A"}.Apply(context.Background())
	ctx, err := f.ks.restoreKnowledgeDeletionActor(ctx, 1, types.TaskInitiator{UserID: "A"})
	require.NoError(t, err)
	require.Equal(t, "A", types.PrivateKBReadUser(ctx))
	require.NoError(t, f.db.Exec("UPDATE tenant_members SET status='removed' WHERE user_id='A'").Error)
	_, err = f.ks.restoreKnowledgeDeletionActor(context.Background(), 1,
		types.TaskInitiator{UserID: "A", Role: types.TenantRoleAdmin})
	require.Error(t, err, "a saved admin role is not current authorization")
}

// Inject bounded DB/authorization latency into the actual protocol commit
// check. Per-row checks keep the resource lock longer than the lease and starve
// the heartbeat; one pre/post check permits both the batch and renewal to commit.
func TestPersonalDocumentBatchDoesNotStarveLeaseRenewal(t *testing.T) {
	f, proof, execution := exactExecutionFixture(t)
	owner := mustPersonalAttempt(t, f, proof.attempt)
	require.NoError(t, f.db.Exec("DROP TABLE embeddings").Error)
	require.NoError(t, f.db.Exec(`CREATE TABLE embeddings (
id bigserial PRIMARY KEY,created_at timestamptz,updated_at timestamptz,
source_id text,source_type int,chunk_id text,knowledge_id text,knowledge_base_id text,
tag_id text,content text,dimension int,embedding text,is_enabled boolean,
UNIQUE(source_id,source_type))`).Error)
	locked := make(chan struct{})
	renewed := make(chan error, 1)
	checks := 0
	ctx, err := types.WithExactDocumentScope(execution, types.ExactDocumentScope{
		TenantID: proof.tenant, KnowledgeID: proof.knowledge, KBID: proof.kb,
		Read: func(context.Context) error {
			_, err := types.CheckExactDocumentRead(execution, proof.knowledge, proof.kb)
			return err
		},
		Commit: func(tx *gorm.DB) error {
			time.Sleep(100 * time.Millisecond)
			err := f.p.commitExactPersonalDocument(tx, proof)
			checks++
			if checks == 1 {
				close(locked)
			}
			return err
		},
	})
	require.NoError(t, err)
	require.NoError(t, f.db.Exec(`UPDATE feishu_personal_attempts
SET lease_until=clock_timestamp()+interval '3 seconds' WHERE id=?`, proof.attempt).Error)
	go func() {
		select {
		case <-locked:
		case <-time.After(10 * time.Second):
			renewed <- fmt.Errorf("batch never acquired the execution lock")
			return
		}
		work, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		renewed <- f.p.mutate(work, owner, 3*time.Second, nil)
	}()
	var infos []*types.IndexInfo
	for i := range 40 {
		infos = append(infos, &types.IndexInfo{
			SourceID:    fmt.Sprintf("source-%d", i),
			KnowledgeID: proof.knowledge, KnowledgeBaseID: proof.kb, Content: "synthetic input",
		})
	}
	err = pgretrieve.NewPostgresRetrieveEngineRepository(f.db).BatchSave(ctx, infos, nil)
	select {
	case renewalErr := <-renewed:
		require.NoError(t, renewalErr)
	case <-time.After(12 * time.Second):
		t.Fatal("lease renewal remained blocked after batch completion")
	}
	require.NoError(t, err)
	var enabled int64
	require.NoError(t, f.db.Table("embeddings").Where("is_enabled").Count(&enabled).Error)
	require.Zero(t, enabled, "staging vectors must remain unpublished")
}
