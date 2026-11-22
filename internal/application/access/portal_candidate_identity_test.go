package access

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func candidateIdentityFixture() (context.Context, *PortalKBTarget) {
	p := types.Principal{Type: types.PrincipalWebUser, ID: "employee"}
	c := types.Caller{TenantID: 1, UserID: p.ID, Role: types.TenantRoleViewer}
	ctx := context.WithValue(context.Background(), types.PrincipalContextKey, p)
	ctx = types.WithCaller(ctx, c)
	ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{
		KeyID: 7, ScopeType: types.APIKeyScopeUserMCP,
		Capabilities: types.StringArray{
			string(types.APIKeyCapabilityRetrieve), string(types.APIKeyCapabilityEnterpriseRetrieve),
		},
	})
	snapshot := &types.EnterpriseReadSnapshot{
		CandidateEpoch: "user/key/kb/portal/model/roles/v1", UserID: p.ID,
		KBID: "kb", Documents: []types.EnterpriseDocumentState{
			{ID: "doc", CurrentVersion: "published-epoch-v1:immutable"},
		},
	}
	t := &PortalKBTarget{
		descriptor: PortalKBDescriptor{TenantID: 1, ID: "kb", EmbeddingModelID: "model"},
		principal:  p, caller: c, credential: normalizedReadCredential(ctx),
		public: &EnterpriseReadGrant{snapshot: snapshot, documents: []string{"doc"}},
	}
	return ctx, t
}

func TestPortalCandidateIdentitySeparatesAuthorityAndPhysicalVersions(t *testing.T) {
	ctx, target := candidateIdentityFixture()
	baseline, ok := PortalCandidateIdentity(ctx, []*PortalKBTarget{target})
	require.True(t, ok)
	mutations := []func(*PortalKBTarget){
		func(t *PortalKBTarget) { t.public.snapshot.CandidateEpoch = "new-key-or-portal-incarnation" },
		func(t *PortalKBTarget) { t.public.snapshot.Documents[0].CurrentVersion = "new-content-epoch" },
		func(t *PortalKBTarget) { t.public.snapshot.KB.Revision++ },
		func(t *PortalKBTarget) { t.public.snapshot.KB.SourceACLState = "revoked" },
		func(t *PortalKBTarget) { t.descriptor.EmbeddingModelID = "other-model" },
		func(t *PortalKBTarget) { t.descriptor.TenantID = 2 },
		func(t *PortalKBTarget) { t.public.documents = append(t.public.documents, "new-document") },
	}
	for _, mutate := range mutations {
		otherCtx, other := candidateIdentityFixture()
		mutate(other)
		actual, ok := PortalCandidateIdentity(otherCtx, []*PortalKBTarget{other})
		require.True(t, ok)
		require.NotEqual(t, baseline, actual)
	}
	otherCtx, other := candidateIdentityFixture()
	key, _ := types.TenantAPIKeyScopeFromContext(otherCtx)
	key.KeyID++
	otherCtx = types.WithTenantAPIKeyScope(otherCtx, key)
	other.credential = normalizedReadCredential(otherCtx)
	actual, ok := PortalCandidateIdentity(otherCtx, []*PortalKBTarget{other})
	require.True(t, ok)
	require.NotEqual(t, baseline, actual)
	otherCtx, other = candidateIdentityFixture()
	other.public.snapshot.CandidateEpoch = ""
	_, ok = PortalCandidateIdentity(otherCtx, []*PortalKBTarget{other})
	require.False(t, ok, "unsupported stores must stay independent")
}

func TestPortalCandidateStampCannotChangeAuthorizationComparison(t *testing.T) {
	a := &types.EnterpriseReadSnapshot{UserID: "employee", CandidateEpoch: "old"}
	b := clonePortalSnapshot(a)
	b.CandidateEpoch = "benign-last-used-update"
	require.True(t, samePortalSnapshot(a, b))
	now := time.Now()
	b.Credential.RevokedAt = &now
	require.False(t, samePortalSnapshot(a, b), "revocation facts remain authoritative")
}
