package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type employeeAdmissionM7 struct {
	outcome types.SourceIDAdmissionOutcome
	calls   int
}

func (a *employeeAdmissionM7) Resolve(_ context.Context, r types.SourceIDAdmissionRequest) (types.SourceIDAdmissionDecision, error) {
	a.calls++
	if r.AuthorizationVerified || len(r.VerifiedClaims) > 0 || r.Stage != "enterprise_mcp_recheck" || r.Subject != "subject" {
		return types.SourceIDAdmissionDecision{}, fmt.Errorf("untrusted authorization input")
	}
	return types.SourceIDAdmissionDecision{Outcome: a.outcome, Source: "controlled-employee-recheck", ContractRef: "employment-v1", CheckedAt: time.Now().Add(-time.Second), ValidUntil: time.Now().Add(time.Minute), RecheckSupported: true}, nil
}
func TestSourceIDM7LiveEmployeeProof(t *testing.T) {
	db, e := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, e)
	require.NoError(t, db.AutoMigrate(&types.User{}, &types.ExternalIdentity{}, &types.SourceIDAccountOwnership{}))
	require.NoError(t, db.Create(&types.User{ID: "employee", Username: "employee", Email: "synthetic@example.invalid", IsActive: true, IsSystemAdmin: true}).Error)
	require.NoError(t, db.Create(&types.ExternalIdentity{ID: "binding", Provider: "sourceid", Issuer: "https://sid.example.invalid", Subject: "subject", EmployeeNo: "synthetic-employee", UserID: "employee"}).Error)
	proof := types.SourceIDAccountOwnership{ID: "proof", Issuer: "https://sid.example.invalid", EmployeeNo: "synthetic-employee", UserID: "employee", Status: "active", Revision: 1, PolicyRef: "ownership-v1", SourceRef: "test-source", OwnershipRef: "test-evidence", VerifiedAt: time.Now().Add(-time.Minute), ValidUntil: time.Now().Add(time.Hour)}
	require.NoError(t, db.Create(&proof).Error)
	a := &employeeAdmissionM7{outcome: types.SourceIDAdmissionAllow}
	v := &SourceIDEmployeeVerifier{DB: db, Issuer: proof.Issuer, OwnershipPolicy: "ownership-v1", AdmissionContract: "employment-v1", MaxOwnershipAge: time.Hour, Admission: a}
	check := func(want bool) {
		t.Helper()
		ok, e := v.IsEnterpriseEmployee(context.Background(), "employee")
		require.NoError(t, e)
		require.Equal(t, want, ok)
	}
	check(true)
	missing, e := v.IsEnterpriseEmployee(context.Background(), "unbound")
	require.NoError(t, e)
	require.False(t, missing)
	require.NoError(t, db.Model(&types.User{}).Where("id=?", "employee").Update("is_system_admin", false).Error)
	check(true)
	a.outcome = types.SourceIDAdmissionDeny
	check(false) // left/service accounts denied by authoritative recheck
	a.outcome = types.SourceIDAdmissionUnknown
	check(false)
	a.outcome = types.SourceIDAdmissionAllow
	require.NoError(t, db.Model(&types.User{}).Where("id=?", "employee").Update("is_active", false).Error)
	check(false)
	require.NoError(t, db.Model(&types.User{}).Where("id=?", "employee").Update("is_active", true).Error)
	require.NoError(t, db.Model(&proof).Update("status", "disabled").Error)
	check(false)
	require.NoError(t, db.Model(&proof).Updates(map[string]any{"status": "active", "valid_until": time.Now().Add(-time.Second)}).Error)
	check(false)
	v.Admission = nil
	ok, e := v.IsEnterpriseEmployee(context.Background(), "employee")
	require.False(t, ok)
	require.Error(t, e)
}
