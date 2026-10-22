package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type ordinaryVerifierM7 struct{ deny bool }

func (v ordinaryVerifierM7) VerifyNewOrdinary(_ context.Context, d *types.Knowledge, version string) (string, error) {
	if v.deny || d.ID != "new-doc" || d.Channel != "web" || version == "" {
		return "", errors.New("unknown/sensitive source")
	}
	return "synthetic-local-rule-v1", nil
}
func TestEnterpriseSnapshotM7OrdinaryInheritance(t *testing.T) {
	for _, scenario := range []string{"ordinary", "unknown", "no-verifier", "changed-membership", "existing-deny"} {
		t.Run(scenario, func(t *testing.T) {
			f := newEnterpriseFixture(t)
			f.approveClean(t)
			var prior types.EnterprisePolicy
			require.NoError(t, f.db.Table("kb_publication_policies").Where("knowledge_base_id='public-kb'").Take(&prior).Error)
			p, e := access.ChangeEnterprisePolicy(enterpriseWebContext("owner"), f.repo, syntheticPublicationAuthority{}, types.EnterprisePolicyChange{Target: types.EnterprisePolicyTarget{Kind: "kb", ID: "public-kb"}, Action: "propose", ExpectedRevision: prior.Revision, Policy: "inherit", ContentClass: "ordinary", SourceACLState: "local", InheritNewOrdinary: true, Reason: "synthetic ordinary inheritance rule"})
			require.NoError(t, e)
			_, e = access.ChangeEnterprisePolicy(enterpriseWebContext("reviewer"), f.repo, syntheticPublicationAuthority{}, types.EnterprisePolicyChange{Target: types.EnterprisePolicyTarget{Kind: "kb", ID: "public-kb"}, Action: "approve", ExpectedRevision: p.Revision, Reason: "independent synthetic rule approval"})
			require.NoError(t, e)
			require.NoError(t, f.db.Exec("ALTER TABLE knowledges ADD COLUMN created_at DATETIME").Error)
			require.NoError(t, f.db.Exec("INSERT INTO knowledges(id,tenant_id,knowledge_base_id,type,parse_status,enable_status,channel,updated_at,created_at) VALUES('new-doc',99,'public-kb','file','completed','enabled','web',?,?)", time.Now(), time.Now().Add(time.Millisecond)).Error)
			require.NoError(t, f.db.Exec("INSERT INTO chunks(id,tenant_id,knowledge_base_id,knowledge_id,content,content_revision,is_enabled,chunk_type,chunk_index,updated_at) VALUES('new-chunk',99,'public-kb','new-doc','PUBLIC new ordinary text',1,true,'text',0,?)", time.Now()).Error)
			var classifier OrdinarySourceVerifier = ordinaryVerifierM7{deny: scenario == "unknown"}
			if scenario == "no-verifier" {
				classifier = nil
			}
			if scenario == "changed-membership" {
				require.NoError(t, f.db.Exec("UPDATE knowledges SET deleted_at=? WHERE id='doc'", time.Now()).Error)
			}
			if scenario == "existing-deny" {
				f.approve(t, "knowledge", "new-doc")
				require.NoError(t, f.db.Exec("UPDATE knowledge_access_policies SET policy='deny' WHERE knowledge_id='new-doc'").Error)
			}
			err := f.repo.InheritNewOrdinary(context.Background(), "new-doc", classifier)
			if scenario != "ordinary" {
				require.Error(t, err)
				var audits int64
				require.NoError(t, f.db.Table("audit_logs").Where("action='enterprise.policy_inherit'").Count(&audits).Error)
				require.Zero(t, audits)
				return
			}
			require.NoError(t, err)
			grant, e := f.reader.Authorize(enterpriseUserContext("A", 1, true), "public-kb")
			require.NoError(t, e)
			require.Len(t, grant.KnowledgeIDs(), 2)
			var audits []types.AuditLog
			require.NoError(t, f.db.Where("action='enterprise.policy_inherit'").Find(&audits).Error)
			require.Len(t, audits, 1)
			require.Empty(t, audits[0].ActorUserID, "automated rule must not impersonate previous human reviewer")
			require.Error(t, f.repo.InheritNewOrdinary(context.Background(), "new-doc", classifier), "duplicate import cannot overwrite policy")
			require.NoError(t, f.db.Exec("UPDATE chunks SET content='changed without approval' WHERE id='new-chunk'").Error)
			require.Error(t, f.reader.Revalidate(enterpriseUserContext("A", 1, true), grant))
		})
	}
}
