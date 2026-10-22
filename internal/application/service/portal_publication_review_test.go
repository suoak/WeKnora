package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type publicationAuthorityM7 struct{}

func (publicationAuthorityM7) AuthorizeEnterprisePolicyChange(_ context.Context, actor string, _ types.EnterprisePolicyTarget, action string) error {
	if actor == "owner" && action == "propose" || actor == "reviewer" && (action == "approve" || action == "revoke") {
		return nil
	}
	return access.ErrForbidden
}
func TestPortalM7FirstConfirmationAtomicAuthorityAndWithdrawal(t *testing.T) {
	f := fixture.New(t)
	resolver := f.Portal(t)
	workflow := &PortalFirstConfirmation{DB: f.DB}
	req := PortalReviewRequest{TenantID: 99, KBID: "public-kb", Action: "propose", Reason: "synthetic first confirmation", InheritNewOrdinary: true, Resources: []PortalReviewResource{{Kind: "space", ID: "99", Revision: 2}, {Kind: "kb", ID: "public-kb", Revision: 2}, {Kind: "knowledge", ID: "public-doc", Revision: 2}}}
	for i := range req.Resources {
		r := &req.Resources[i]
		table, col, ok := (types.EnterprisePolicyTarget{Kind: r.Kind, ID: r.ID}).Storage()
		require.True(t, ok)
		require.NoError(t, f.DB.Table(table).Select("revision").Where(col+"=?", r.ID).Scan(&r.Revision).Error)
	}
	require.Error(t, workflow.Apply(fixture.Web("owner", 1), req), "a Portal role alone cannot approve enterprise publication")
	workflow.Authority = publicationAuthorityM7{}
	require.NoError(t, workflow.Apply(fixture.Web("owner", 1), req))
	for i := range req.Resources {
		req.Resources[i].Revision++
	}
	req.Action = "approve"
	require.Error(t, workflow.Apply(fixture.Web("owner", 1), req))
	var before int64
	require.NoError(t, f.DB.Table("audit_logs").Count(&before).Error)
	stale := req
	stale.Resources = append([]PortalReviewResource(nil), req.Resources...)
	stale.Resources[2].Revision--
	require.Error(t, workflow.Apply(fixture.Web("reviewer", 1), stale))
	var after int64
	require.NoError(t, f.DB.Table("audit_logs").Count(&after).Error)
	require.Equal(t, before, after, "failed bundle rolls back policies AND audit")
	require.NoError(t, workflow.Apply(fixture.Web("reviewer", 1), req))
	_, e := resolver.Resolve(fixture.Key("A", 1, 1, true), []string{"public-kb"})
	require.NoError(t, e)
	withdraw := PortalReviewRequest{TenantID: 99, KBID: "public-kb", Action: "revoke", Reason: "synthetic urgent withdrawal", Resources: []PortalReviewResource{{Kind: "kb", ID: "public-kb", Revision: 0}}}
	require.NoError(t, workflow.Apply(fixture.Web("reviewer", 1), withdraw), "withdrawal does not wait for a whole-KB client snapshot")
	_, e = resolver.Resolve(fixture.Key("A", 1, 1, true), []string{"public-kb"})
	require.Error(t, e)
	require.Error(t, workflow.Apply(fixture.Web("reviewer", 1), req), "stale approval cannot restore withdrawn policy")
	inventory, e := (&portalService{db: f.DB}).PublicationInventory(context.Background(), 99)
	require.NoError(t, e)
	require.NotEmpty(t, inventory)
	for _, row := range inventory {
		require.NotEqual(t, "public", row.RuntimeStatus)
	}
}
