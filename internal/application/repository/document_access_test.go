package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func documentMigration(t *testing.T, db *gorm.DB, dialect, direction string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("../../../migrations", dialect, "003035_document_access."+direction+".sql"))
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(data)).Error)
}

// Synthetic verifier intentionally has no production role mapping. The actual
// adapter remains NEEDS_DECISION and nil in the application container.
type syntheticDocumentBase struct{ f *enterpriseFixture }

func (b syntheticDocumentBase) CheckDocumentReadBase(ctx context.Context, tenant uint64, kb string, op types.DocumentOperation, public bool) error {
	if tenant != 99 || kb != "public-kb" || op == types.DocumentFile {
		return access.ErrForbidden
	}
	if public {
		return nil
	}
	if key, ok := types.TenantAPIKeyScopeFromContext(ctx); ok && !key.AllowsKnowledgeBase(kb) {
		return access.ErrForbidden
	}
	var n int64
	err := b.f.db.Table("tenant_members").Where("tenant_id=? AND user_id=? AND status='active'", tenant, types.CallerFromContext(ctx).UserID).Count(&n).Error
	if err != nil {
		return err
	}
	if n != 1 {
		return access.ErrForbidden
	}
	return nil
}

type syntheticDocumentAuthority struct{}

func (syntheticDocumentAuthority) AuthorizeDocumentGrantChange(_ context.Context, actor string, _ types.DocumentGrantChange) error {
	if actor != "reviewer" {
		return access.ErrForbidden
	}
	return nil
}

func documentCtx(user string, key uint64) context.Context {
	ctx := enterpriseUserContext(user, key, true)
	scope, _ := types.TenantAPIKeyScopeFromContext(ctx)
	scope.KnowledgeBaseIDs = types.StringArray{"public-kb"}
	return types.WithTenantAPIKeyScope(ctx, scope)
}
func (f *enterpriseFixture) documentPolicy(t *testing.T, doc, policy string) {
	t.Helper()
	target := types.EnterprisePolicyTarget{Kind: "knowledge", ID: doc}
	var old types.EnterprisePolicy
	require.NoError(t, f.db.Table("knowledge_access_policies").Where("knowledge_id=?", doc).Find(&old).Error)
	p, err := access.ChangeEnterprisePolicy(enterpriseWebContext("owner"), f.repo, syntheticPublicationAuthority{}, types.EnterprisePolicyChange{Target: target, Action: "propose", ExpectedRevision: old.Revision, Policy: policy, ContentClass: "mixed", SourceACLState: "local", Reason: "synthetic document review"})
	require.NoError(t, err)
	_, err = access.ChangeEnterprisePolicy(enterpriseWebContext("reviewer"), f.repo, syntheticPublicationAuthority{}, types.EnterprisePolicyChange{Target: target, Action: "approve", ExpectedRevision: p.Revision, Reason: "independent synthetic approval"})
	require.NoError(t, err)
}
func (f *enterpriseFixture) subject(t *testing.T, user string, op types.DocumentOperation, state string, revision int64) (*types.DocumentSubjectGrant, error) {
	t.Helper()
	snapshot, err := f.repo.LoadDocumentReadSnapshot(context.Background(), user, 0, "public-kb")
	require.NoError(t, err)
	return access.ChangeDocumentSubjectGrant(enterpriseWebContext("reviewer"), f.repo, syntheticDocumentAuthority{}, types.DocumentGrantChange{DocumentSubjectGrant: types.DocumentSubjectGrant{KnowledgeID: "doc", UserID: user, Operation: op, State: state, Revision: revision, ResourceVersion: snapshot.Documents[0].CurrentVersion}, Reason: "synthetic exact grant"})
}
func (f *enterpriseFixture) documentReader() *access.DocumentReader {
	return access.NewDocumentReader(true, f.repo, syntheticDocumentBase{f}, f.employees)
}

func TestDocumentM3GrantAuditFailureRollsBack(t *testing.T) {
	f := newEnterpriseFixture(t)
	f.approveClean(t)
	f.documentPolicy(t, "doc", "restricted")
	g, err := f.subject(t, "B", types.DocumentRead, "approved", 0)
	require.NoError(t, err)
	require.NoError(t, f.db.Exec(`DROP TABLE audit_logs`).Error)
	_, err = f.subject(t, "B", types.DocumentRead, "revoked", 0)
	require.Error(t, err)
	current, err := f.repo.LoadDocumentSubjectGrant(context.Background(), "doc", "B", types.DocumentRead)
	require.NoError(t, err)
	require.Equal(t, "approved", current.State)
	require.Equal(t, g.Revision, current.Revision)
}

func TestDocumentM3ExactACLAndCredentialBoundaries(t *testing.T) {
	f := newEnterpriseFixture(t)
	f.approveClean(t)
	r := f.documentReader()
	_, err := r.Authorize(documentCtx("A", 1), "public-kb", "doc", types.DocumentRead)
	require.NoError(t, err)
	_, err = access.NewDocumentReader(false, f.repo, syntheticDocumentBase{f}, f.employees).Authorize(documentCtx("A", 1), "public-kb", "doc", types.DocumentRead)
	require.Error(t, err)
	f.documentPolicy(t, "doc", "restricted")
	_, err = r.Authorize(documentCtx("C", 2), "public-kb", "doc", types.DocumentRead)
	require.Error(t, err)
	_, err = f.subject(t, "B", types.DocumentRead, "approved", 0)
	require.NoError(t, err)
	g, err := r.Authorize(documentCtx("B", 4), "public-kb", "doc", types.DocumentRead)
	require.NoError(t, err)
	rows, err := r.ReadText(documentCtx("B", 4), g, NewChunkRepository(f.db))
	require.NoError(t, err)
	require.Contains(t, rows[0].Content, "ERR_M2_42")
	require.NoError(t, f.db.Exec(`DELETE FROM tenant_portal_configs WHERE tenant_id=99`).Error)
	_, err = r.Authorize(documentCtx("B", 4), "public-kb", "doc", types.DocumentRead)
	require.NoError(t, err, "private explicit grants do not depend on Portal publication/config existence")
	require.NoError(t, f.db.Exec(`INSERT INTO tenant_portal_configs VALUES(99,'published')`).Error)
	otherContext := types.WithCaller(documentCtx("B", 4), types.Caller{TenantID: 2, UserID: "B", Role: types.TenantRoleViewer})
	require.Error(t, g.Revalidate(otherContext), "even independently allowed contexts cannot reuse a lease from another caller workspace")
	_, err = r.Authorize(documentCtx("B", 4), "public-kb", "doc", types.DocumentSearch)
	require.Error(t, err)
	_, err = r.Authorize(documentCtx("B", 4), "public-kb", "doc", types.DocumentFile)
	require.Error(t, err)
	_, err = r.ReadText(documentCtx("A", 1), g, NewChunkRepository(f.db))
	require.Error(t, err)
	_, err = f.subject(t, "B", types.DocumentRead, "revoked", 0)
	require.NoError(t, err)
	_, err = f.subject(t, "B", types.DocumentRead, "approved", 1)
	require.ErrorIs(t, err, types.ErrEnterprisePolicyConflict)
	require.Error(t, g.Revalidate(documentCtx("B", 4)))
	// full_access and machine identities cannot turn document denials into grants.
	ctx := documentCtx("B", 4)
	scope, _ := types.TenantAPIKeyScopeFromContext(ctx)
	scope.FullAccess = true
	_, err = r.Authorize(types.WithTenantAPIKeyScope(ctx, scope), "public-kb", "doc", types.DocumentRead)
	require.Error(t, err)
	f.documentPolicy(t, "doc", "mcp_blocked")
	_, err = f.subject(t, "B", types.DocumentRead, "approved", 2)
	require.NoError(t, err)
	_, err = r.Authorize(ctx, "public-kb", "doc", types.DocumentRead)
	require.Error(t, err)
	_, err = r.Authorize(enterpriseWebContext("B"), "public-kb", "doc", types.DocumentRead)
	require.NoError(t, err)
	f.documentPolicy(t, "doc", "deny")
	_, err = r.Authorize(enterpriseWebContext("B"), "public-kb", "doc", types.DocumentRead)
	require.Error(t, err)
}

func TestDocumentM3DatabaseEpochCoversRawWritersAndMissingTriggers(t *testing.T) {
	for _, sql := range []string{
		`UPDATE chunks SET content='synthetic changed without timestamp' WHERE id='chunk'`,
		`UPDATE knowledges SET channel='api' WHERE id='doc'`,
		`UPDATE chunks SET is_enabled=false WHERE id='chunk'`,
		`INSERT INTO chunks SELECT 'new-chunk',tenant_id,knowledge_base_id,knowledge_id,content,content_revision,is_enabled,chunk_type,chunk_index,updated_at,deleted_at FROM chunks WHERE id='chunk'`,
		`DELETE FROM chunks WHERE id='chunk'`,
		`UPDATE knowledges SET deleted_at='2026-10-21' WHERE id='doc'`,
		`DROP TRIGGER document_epoch_chunks_update`,
	} {
		t.Run(strings.Fields(sql)[0]+sql[:min(35, len(sql))], func(t *testing.T) {
			f := newEnterpriseFixture(t)
			f.approveClean(t)
			r := f.documentReader()
			ctx := documentCtx("A", 1)
			g, err := r.Authorize(ctx, "public-kb", "doc", types.DocumentRead)
			require.NoError(t, err)
			require.NoError(t, f.db.Exec(sql).Error)
			require.Error(t, g.Revalidate(ctx))
		})
	}
}

func TestDocumentM3LiveDenialsAndMigrationReset(t *testing.T) {
	for _, sql := range []string{
		`UPDATE users SET is_active=false WHERE id='A'`,
		`UPDATE tenant_api_keys SET revoked_at=CURRENT_TIMESTAMP WHERE id=1`,
		`UPDATE tenant_api_keys SET expires_at='2000-01-01 00:00:00' WHERE id=1`,
		`UPDATE tenant_api_keys SET capabilities='[]' WHERE id=1`,
		`UPDATE knowledge_access_policies SET source_acl_state='verified' WHERE knowledge_id='doc'`,
		`UPDATE knowledge_access_policies SET source_acl_state='revoked' WHERE knowledge_id='doc'`,
		`UPDATE knowledge_access_policies SET state='revoked' WHERE knowledge_id='doc'`,
		`UPDATE tenant_portal_configs SET status='archived' WHERE tenant_id=99`,
		`DELETE FROM knowledge_access_policies WHERE knowledge_id='doc'`,
	} {
		t.Run(sql, func(t *testing.T) {
			f := newEnterpriseFixture(t)
			f.approveClean(t)
			r := f.documentReader()
			ctx := documentCtx("A", 1)
			g, err := r.Authorize(ctx, "public-kb", "doc", types.DocumentRead)
			require.NoError(t, err)
			require.NoError(t, f.db.Exec(sql).Error)
			require.Error(t, g.Revalidate(ctx))
			_, err = r.Authorize(ctx, "public-kb", "doc", types.DocumentRead)
			require.Error(t, err)
		})
	}
	f := newEnterpriseFixture(t)
	f.approveClean(t)
	r := f.documentReader()
	ctx := documentCtx("A", 1)
	g, err := r.Authorize(ctx, "public-kb", "doc", types.DocumentRead)
	require.NoError(t, err)
	documentMigration(t, f.db, "sqlite", "down")
	documentMigration(t, f.db, "sqlite", "up")
	require.Error(t, g.Revalidate(ctx), "down/up must never resurrect previous reviews")
}
