package service

import (
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPersonalPublishedViewerUsesCurrentKBPermission(t *testing.T) {
	f := newPersonalRecoveryFixture(t)
	receipt := types.PersonalExactReceipt{TenantID: 1, KnowledgeBaseID: "kb"}
	viewer := types.WithPrincipal(f.userContext("B"), types.Principal{Type: types.PrincipalWebUser, ID: "B"})
	viewer = types.WithCaller(viewer, types.Caller{UserID: "B", TenantID: 1, Role: types.TenantRoleViewer})
	require.NoError(t, f.db.Exec("UPDATE tenant_members SET role='viewer' WHERE user_id='B'").Error)
	_, err := f.p.checkExactPublishedViewer(viewer, receipt)
	require.NoError(t, err)
	// Independent requests use the application's connection pool. PostgreSQL
	// cannot multiplex active query results on a single shared transaction.
	var pending sync.WaitGroup
	failures := make(chan error, 32)
	for range 32 {
		pending.Add(1)
		go func() {
			defer pending.Done()
			_, failure := f.p.checkExactPublishedViewer(viewer, receipt)
			failures <- failure
		}()
	}
	pending.Wait()
	close(failures)
	for failure := range failures {
		require.NoError(t, failure)
	}

	require.NoError(t, f.db.Exec("INSERT INTO users(id,is_active) VALUES('C',true)").Error)
	require.NoError(t, f.db.Exec("INSERT INTO tenants VALUES(2,'active',NULL)").Error)
	require.NoError(t, f.db.Exec("INSERT INTO tenant_members(user_id,tenant_id,role,status) VALUES('C',2,'viewer','active')").Error)
	stranger := types.WithCaller(personalUser("C"), types.Caller{UserID: "C", TenantID: 2, Role: types.TenantRoleViewer})
	stranger = types.WithPrincipal(stranger, types.Principal{Type: types.PrincipalWebUser, ID: "C"})
	_, err = f.p.checkExactPublishedViewer(stranger, receipt)
	require.Error(t, err, "an active user in an unrelated space cannot read this KB")

	require.NoError(t, f.db.Exec("CREATE TABLE feishu_private_kbs(kb_id text PRIMARY KEY,owner_id text NOT NULL)").Error)
	f.s.db = types.EnablePrivateKBGuards(f.db)
	require.NoError(t, f.db.Exec("INSERT INTO feishu_private_kbs VALUES('kb','A')").Error)
	_, err = f.p.checkExactPublishedViewer(viewer, receipt)
	require.Error(t, err, "space membership does not grant another user's private KB")
	_, err = f.p.checkExactPublishedViewer(f.ctx, receipt)
	require.NoError(t, err)

	require.NoError(t, f.db.Exec("DELETE FROM feishu_private_kbs WHERE kb_id='kb'").Error)
	require.NoError(t, f.db.Exec("ALTER TABLE knowledge_bases ADD COLUMN is_temporary boolean NOT NULL DEFAULT false").Error)
	require.NoError(t, f.db.Exec("UPDATE knowledge_bases SET is_temporary=true WHERE id='kb'").Error)
	_, err = f.p.checkExactPublishedViewer(viewer, receipt)
	require.Error(t, err, "publication cannot turn a temporary KB into a durable shared asset")
}
