package repository

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

// SourceIDEmployeeVerifier reuses the protected SID binding and ownership
// records. Ownership alone is not employment: the approved admission adapter
// must recheck current employee eligibility, including service-account exclusion.
// No local role, email suffix, header or successful historical login substitutes
// for this independently configured contract. The zero value denies access.
type SourceIDEmployeeVerifier struct {
	DB                                         *gorm.DB
	Issuer, OwnershipPolicy, AdmissionContract string
	MaxOwnershipAge                            time.Duration
	Admission                                  interfaces.SourceIDEmployeeAdmissionResolver
}

func (v *SourceIDEmployeeVerifier) IsEnterpriseEmployee(ctx context.Context, user string) (bool, error) {
	if v == nil || v.DB == nil || v.Admission == nil || v.Issuer == "" || v.OwnershipPolicy == "" || v.AdmissionContract == "" || v.MaxOwnershipAge <= 0 || user == "" {
		return false, access.ErrForbidden
	}
	var rows []types.ExternalIdentity
	err := v.DB.WithContext(ctx).Table("external_identities e").Select("e.*").
		Joins("JOIN users u ON u.id=e.user_id AND u.deleted_at IS NULL AND u.is_active=true").
		Where("e.user_id=? AND e.issuer=? AND e.provider='sourceid'", user, v.Issuer).Limit(2).Find(&rows).Error
	if err != nil {
		return false, access.ErrForbidden
	}
	if len(rows) != 1 || rows[0].Subject == "" || rows[0].EmployeeNo == "" {
		return false, nil
	}
	b := rows[0]
	var proof types.SourceIDAccountOwnership
	if err := v.DB.WithContext(ctx).Where("issuer=? AND employee_no=?", v.Issuer, b.EmployeeNo).Take(&proof).Error; err != nil {
		return false, access.ErrForbidden
	}
	if !ownershipUsable(&proof, user, v.OwnershipPolicy, v.MaxOwnershipAge) {
		return false, nil
	}
	// Never manufacture verified authorization claims for a long-lived API Key.
	decision, err := v.Admission.Resolve(ctx, types.SourceIDAdmissionRequest{Issuer: v.Issuer, Subject: b.Subject, EmployeeNo: b.EmployeeNo, Stage: "enterprise_mcp_recheck"})
	now := time.Now()
	if err != nil {
		return false, access.ErrForbidden
	}
	return decision.Outcome == types.SourceIDAdmissionAllow && decision.ContractRef == v.AdmissionContract && decision.Source != "" && decision.RecheckSupported && !decision.CheckedAt.IsZero() && !decision.CheckedAt.After(now) && decision.ValidUntil.After(now), nil
}
