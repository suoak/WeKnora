package repository

// Design probes against the unchanged f40 repository. These are NOT a new
// production admission mode. Success in a limitation probe documents a gap;
// it does not approve the risk or bypass the enterprise/session gates.
import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSourceIDVerifiedEmailDesignEnterpriseStillRequiresProof(t *testing.T) {
	r, db, _ := enterpriseTestDB(t, false)
	_, err := r.ResolveSourceIDEnterpriseEmailIdentity(context.Background(), "synthetic-issuer", "synthetic-sub", "synthetic-employee", "employee@example.test", "synthetic-binding", "synthetic-ownership", time.Hour)
	require.Error(t, err)
	for _, model := range []interface{}{&types.SourceIDBindingReview{}, &types.ExternalIdentity{}, &types.SourceIDAccountOwnership{}} {
		var n int64
		require.NoError(t, db.Model(model).Count(&n).Error)
		require.Zero(t, n)
	}
}

func TestSourceIDVerifiedEmailDesignReuseBindingWithoutFabricatedProof(t *testing.T) {
	r, db, user := enterpriseTestDB(t, false)
	ctx := context.Background()
	bound, err := r.ResolveSourceIDAutoEmailIdentity(ctx, "synthetic-issuer", "synthetic-sub", "synthetic-employee", " EMPLOYEE@example.test ", "synthetic-email-policy")
	require.NoError(t, err)
	require.Equal(t, user.ID, bound.ID)
	require.Equal(t, user.IsSystemAdmin, bound.IsSystemAdmin)
	require.Equal(t, user.CanAccessAllTenants, bound.CanAccessAllTenants)
	require.Equal(t, user.TenantID, bound.TenantID)
	// Stable identity wins over a subsequent email, including another user's.
	other := types.User{ID: uuid.NewString(), Username: "synthetic-other", Email: "other@example.test", IsActive: true}
	require.NoError(t, db.Create(&other).Error)
	bound, err = r.ResolveSourceIDAutoEmailIdentity(ctx, "synthetic-issuer", "synthetic-sub", "synthetic-employee", other.Email, "synthetic-email-policy")
	require.NoError(t, err)
	require.Equal(t, user.ID, bound.ID)
	var proofCount, auditCount, userCount int64
	require.NoError(t, db.Model(&types.SourceIDAccountOwnership{}).Count(&proofCount).Error)
	require.NoError(t, db.Model(&types.SourceIDBindingAudit{}).Count(&auditCount).Error)
	require.NoError(t, db.Model(&types.User{}).Count(&userCount).Error)
	require.Zero(t, proofCount)
	require.EqualValues(t, 1, auditCount)
	require.EqualValues(t, 2, userCount)
}

func TestSourceIDVerifiedEmailDesignRevocationCannotBecomeNewEmailLink(t *testing.T) {
	r, _, _ := enterpriseTestDB(t, false)
	ctx := context.Background()
	_, err := r.ResolveSourceIDAutoEmailIdentity(ctx, "synthetic-issuer", "synthetic-sub", "synthetic-employee", "employee@example.test", "synthetic-email-policy")
	require.NoError(t, err)
	rows, err := r.ListSourceIDBindingReviews(ctx)
	require.NoError(t, err)
	require.NoError(t, r.ReviewSourceIDBinding(ctx, rows[0].ID, "synthetic-operator", "synthetic-revoke", "revoke"))
	for _, identity := range [][2]string{{"synthetic-sub", "synthetic-employee"}, {"synthetic-new-sub", "synthetic-new-employee"}} {
		_, err = r.ResolveSourceIDAutoEmailIdentity(ctx, "synthetic-issuer", identity[0], identity[1], "employee@example.test", "synthetic-email-policy")
		require.Error(t, err)
	}
}

func TestSourceIDVerifiedEmailDesignUnrecordedReassignmentIsNotDetectable(t *testing.T) {
	r, _, old := enterpriseTestDB(t, false)
	// The DB has no identity history. A verified present-day mailbox owner
	// could be a different employee: neither OIDC nor this lookup proves the
	// historical owner. This expected success documents the residual risk.
	u, err := r.ResolveSourceIDAutoEmailIdentity(context.Background(), "synthetic-issuer", "synthetic-new-owner", "synthetic-reassigned-employee", old.Email, "synthetic-risk-demonstration-only")
	require.NoError(t, err)
	require.Equal(t, old.ID, u.ID)
}

func TestSourceIDVerifiedEmailDesignBypassWouldIgnoreDisabledOwnership(t *testing.T) {
	r, db, user := enterpriseTestDB(t, false)
	row := proof(user)
	row.Status = "disabled"
	_, err := r.SyncSourceIDAccountOwnership(context.Background(), row, "synthetic-operator", true)
	require.NoError(t, err)
	// Calling the old unscoped primitive alone is NOT the proposed adapter:
	// known disabled ownership must be checked before any pending row writes.
	u, err := r.ResolveSourceIDAutoEmailIdentity(context.Background(), row.Issuer, "synthetic-sub", row.EmployeeNo, user.Email, "synthetic-risk-demonstration-only")
	require.NoError(t, err)
	require.Equal(t, user.ID, u.ID)
	var unchanged types.SourceIDAccountOwnership
	require.NoError(t, db.First(&unchanged, "issuer = ? AND employee_no = ?", row.Issuer, row.EmployeeNo).Error)
	require.Equal(t, "disabled", unchanged.Status)
}
