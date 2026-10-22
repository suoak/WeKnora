package service

import (
	"context"
	"testing"

	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPortalM7ConfiguredOwnerAdminBusinessFlow(t *testing.T) {
	f := fixture.New(t)
	resolver := f.Portal(t)
	require.NoError(t, f.DB.Exec("INSERT INTO tenant_members(user_id,tenant_id,role,status) VALUES('owner',99,'owner','active')").Error)
	require.NoError(t, f.DB.Exec("UPDATE users SET is_system_admin=true WHERE id='reviewer'").Error)
	s := &portalService{db: f.DB}
	owner := fixture.Web("owner", 99)
	admin := context.WithValue(fixture.Web("reviewer", 1), types.SystemAdminContextKey, true)
	snap, err := s.PublicationReviewSnapshot(owner, 99, "public-kb")
	require.NoError(t, err)
	req := PortalReviewRequest{TenantID: 99, KBID: "public-kb", Action: "propose", Reason: "synthetic owner inspected ordinary scope", Resources: snap.Resources, InheritNewOrdinary: true}
	require.Error(t, s.ChangePublication(owner, req), "zero config denies even actual owner")
	require.NoError(t, ConfigurePortalPublication(s, PortalPolicyAuthorityConfig{Enabled: true, AllowSystemAdminReview: true, ContractRef: "isolated-owner-admin-v1"}))
	require.NoError(t, s.ChangePublication(owner, req))
	_, err = resolver.Resolve(fixture.Key("A", 1, 1, true), []string{"public-kb"})
	require.Error(t, err, "proposal alone must remove public grant")
	snap, err = s.PublicationReviewSnapshot(admin, 99, "public-kb")
	require.NoError(t, err)
	req.Action = "approve"
	req.Resources = snap.Resources
	require.Error(t, s.ChangePublication(owner, req), "owner alone is not reviewer")
	require.Error(t, s.ChangePublication(fixture.Web("reviewer", 1), req), "DB SystemAdmin alone is not authenticated JWT admin")
	require.Error(t, s.ChangePublication(fixture.Key("reviewer", 1, 1, true), req), "API key cannot approve")
	require.NoError(t, f.DB.Exec("UPDATE tenant_members SET status='suspended' WHERE user_id='owner' AND tenant_id=99").Error)
	require.Error(t, s.ChangePublication(admin, req), "departed content owner cannot leave an approvable proposal")
	require.NoError(t, f.DB.Exec("UPDATE tenant_members SET status='active' WHERE user_id='owner' AND tenant_id=99").Error)
	require.NoError(t, s.ChangePublication(admin, req))
	targets, err := resolver.Resolve(fixture.Key("A", 1, 1, true), nil)
	require.NoError(t, err)
	require.Len(t, targets, 3)
	history, err := s.PublicationHistory(owner, 99, "public-kb")
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(history), 2)
	require.Equal(t, "reviewer", history[0].ActorUserID)
	require.Equal(t, "enterprise.policy_approve", history[0].Action)
	_, err = s.PublicationReviewSnapshot(fixture.Web("C", 99), 99, "public-kb")
	require.Error(t, err, "ordinary member cannot manage scope")
	_, err = s.PublicationReviewSnapshot(owner, 99, "private-home")
	require.Error(t, err, "cross-tenant KB metadata must not leak")
	req.Action = "revoke"
	req.Resources = []PortalReviewResource{{Kind: "kb", ID: "public-kb", Revision: 0}}
	require.NoError(t, s.ChangePublication(owner, req), "actual owner can withdraw without admin or stale-CAS wait")
	_, err = resolver.Resolve(fixture.Key("A", 1, 1, true), []string{"public-kb"})
	require.Error(t, err)
	req.Action = "approve"
	req.Resources = snap.Resources
	require.Error(t, s.ChangePublication(admin, req), "stale approve cannot undo withdrawal")
}

func TestPortalM7ReviewVersionsAndPublishedBoundary(t *testing.T) {
	f := fixture.New(t)
	f.Portal(t)
	require.NoError(t, f.DB.Exec("INSERT INTO tenant_members(user_id,tenant_id,role,status) VALUES('owner',99,'owner','active')").Error)
	s := &portalService{db: f.DB, publicationConfig: PortalPolicyAuthorityConfig{Enabled: true, AllowSystemAdminReview: true, ContractRef: "isolated"}}
	ctx := fixture.Web("owner", 99)
	snap, err := s.PublicationReviewSnapshot(ctx, 99, "public-kb")
	require.NoError(t, err)
	req := PortalReviewRequest{TenantID: 99, KBID: "public-kb", Action: "propose", Reason: "synthetic", Resources: snap.Resources}
	require.NoError(t, f.DB.Exec("UPDATE chunks SET content='new content without updated_at' WHERE knowledge_id='public-doc'").Error)
	require.ErrorIs(t, s.ChangePublication(ctx, req), types.ErrEnterprisePolicyConflict)
	snap, err = s.PublicationReviewSnapshot(ctx, 99, "public-kb")
	require.NoError(t, err)
	req.Resources = snap.Resources
	require.NoError(t, f.DB.Exec("UPDATE tenant_portal_configs SET status='draft' WHERE tenant_id=99").Error)
	require.Error(t, s.ChangePublication(ctx, req), "cannot first-confirm unpublished Portal")
}
