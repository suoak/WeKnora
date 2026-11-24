package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func personalRetryOwnerContext(user string, tenant uint64) context.Context {
	ctx := context.WithValue(context.Background(), types.UserIDContextKey, user)
	ctx = types.WithCaller(ctx, types.Caller{UserID: user, TenantID: tenant, Role: types.TenantRoleAdmin})
	return types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: user})
}

func personalRetryState(t *testing.T, f *personalRecoveryFixture, id string) string {
	t.Helper()
	var state string
	require.NoError(t, f.db.Raw(`SELECT jsonb_build_object(
 'attempt',to_jsonb(a),'resource',to_jsonb(r),'import',to_jsonb(m),
 'ops',(SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM task_pending_ops x),
 'audits',(SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM task_dead_letters x))::text
 FROM feishu_personal_attempts a JOIN feishu_personal_resources r ON r.import_id=a.import_id
 JOIN feishu_personal_imports m ON m.id=a.import_id WHERE a.id=?`, id).Scan(&state).Error)
	return state
}

func TestPersonalExactExplicitConsentRetainsProtocolAndConsumesNewClaim(t *testing.T) {
	f, observed := closedExactResumeFixture(t)
	m, resource, _, err := f.p.repo.Load(context.Background(), observed.ID)
	require.NoError(t, err)
	ctx := personalRetryOwnerContext(m.UserID, observed.CallerTenantID)
	var before []types.TaskPendingOp
	require.NoError(t, f.db.Order("id").Find(&before).Error)
	require.NoError(
		t,
		f.db.Exec(
			"UPDATE feishu_personal_grant_epochs SET epoch='renewed-owner-consent' WHERE user_id=?",
			m.UserID,
		).Error,
	)
	check := &personalExactPreflight{observed: observed, closed: true, renewedEpoch: "renewed-owner-consent"}
	_, err = f.p.checkExactPersonalSource(ctx, observed, false, check)
	require.NoError(t, err)
	require.Equal(t, "renewed-owner-consent", check.source.grantEpoch)
	require.Equal(
		t,
		observed.GrantEpoch,
		mustPersonalAttempt(t, f, observed.ID).GrantEpoch,
		"preflight remains read-only",
	)
	require.NoError(
		t,
		f.p.repo.QueueExactResumeWithConsent(
			ctx,
			observed,
			exactSourceReceipt(check.source),
			check.validate,
			"renewed-owner-consent",
		),
	)
	current := mustPersonalAttempt(t, f, observed.ID)
	require.Equal(t, "renewed-owner-consent", current.GrantEpoch)
	require.Equal(t, observed.ID, current.ID)
	require.Equal(t, observed.LogicalKnowledgeID, current.LogicalKnowledgeID)
	require.Equal(t, observed.CandidateVersionID, current.CandidateVersionID)
	require.Equal(t, observed.ManifestHash, current.ManifestHash)
	require.Equal(t, observed.RetryCount+1, current.RetryCount)
	var after []types.TaskPendingOp
	require.NoError(t, f.db.Order("id").Find(&after).Error)
	require.Len(t, after, len(before))
	for i := range before {
		require.Equal(t, before[i].ID, after[i].ID)
		require.Equal(t, before[i].DedupKey, after[i].DedupKey)
	}
	_, next, _, err := f.p.repo.Load(ctx, observed.ID)
	require.NoError(t, err)
	require.Equal(t, resource.ImportID, next.ImportID)
	require.Equal(t, resource.ActiveVersionID, next.ActiveVersionID)
	require.Error(
		t,
		f.p.repo.QueueExactResumeWithConsent(
			ctx,
			observed,
			exactSourceReceipt(check.source),
			check.validate,
			"renewed-owner-consent",
		),
	)
	preflight, err := f.p.preflightExactPersonalDocument(ctx, current)
	require.NoError(t, err)
	owner, err := f.p.claimExactPreflight(ctx, preflight)
	require.NoError(t, err)
	proof, err := f.p.prepareExactPersonalDocument(ctx, owner)
	require.NoError(t, err)
	require.NoError(t, f.p.repo.ActivateExactResume(ctx, owner, exactSourceReceipt(proof)))
	var audit types.TaskDeadLetter
	require.NoError(t, f.db.Where("task_type=?", "knowledge:exact_resume_audit").Take(&audit).Error)
	var record map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(audit.Payload, &record))
	require.JSONEq(t, "true", string(record["explicit_owner_consent"]))
}

func TestPersonalExactExplicitConsentRefusesIdentityRevocationAndTampering(t *testing.T) {
	for _, change := range []string{
		"other-user", "other-tenant", "machine", "account", "revoked-epoch", "receipt", "live-publication", "membership",
	} {
		t.Run(change, func(t *testing.T) {
			f, observed := closedExactResumeFixture(t)
			m, _, _, err := f.p.repo.Load(context.Background(), observed.ID)
			require.NoError(t, err)
			ctx := personalRetryOwnerContext(m.UserID, observed.CallerTenantID)
			require.NoError(
				t,
				f.db.Exec(
					"UPDATE feishu_personal_grant_epochs SET epoch='new-consent' WHERE user_id=?",
					m.UserID,
				).Error,
			)
			check := &personalExactPreflight{observed: observed, closed: true, renewedEpoch: "new-consent"}
			_, err = f.p.checkExactPersonalSource(ctx, observed, false, check)
			require.NoError(t, err)
			receipt := exactSourceReceipt(check.source)
			switch change {
			case "other-user":
				ctx = personalRetryOwnerContext("B", observed.CallerTenantID)
			case "other-tenant":
				ctx = personalRetryOwnerContext(m.UserID, observed.CallerTenantID+1)
			case "machine":
				ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalAPITenant, ID: "machine"})
			case "account":
				require.NoError(
					t,
					f.db.Exec("UPDATE feishu_connections SET open_id='different' WHERE user_id=?", m.UserID).Error,
				)
			case "revoked-epoch":
				require.NoError(
					t,
					f.db.Exec(
						"UPDATE feishu_personal_grant_epochs SET epoch='revoked' WHERE user_id=?",
						m.UserID,
					).Error,
				)
			case "receipt":
				receipt.FileSHA256 = "wrong"
			case "live-publication":
				require.NoError(
					t,
					f.db.Exec(
						"UPDATE feishu_personal_resources SET active_version_id='published' WHERE import_id=?",
						m.ID,
					).Error,
				)
			case "membership":
				require.NoError(t, f.db.Exec("UPDATE tenant_members SET role='viewer' WHERE user_id=?", m.UserID).Error)
			}
			before := personalRetryState(t, f, observed.ID)
			validate := func(tx *gorm.DB, a *types.FeishuPersonalAttempt) error {
				if err := f.s.requireImportWriter(tx, m.UserID, m.KBID, a.CallerTenantID); err != nil {
					return err
				}
				return check.validate(tx, a)
			}
			require.Error(t, f.p.repo.QueueExactResumeWithConsent(ctx, observed, receipt, validate, "new-consent"))
			require.JSONEq(t, before, personalRetryState(t, f, observed.ID))
		})
	}
}

func TestFeishuV2PostgresBatchRetryUsesFreshSameAccountConsent(t *testing.T) {
	f := v2PostgresFixture(t)
	require.NoError(t, f.db.AutoMigrate(&types.TaskDeadLetter{}))
	view, err := f.s.QuickImport(f.ctx, "kb", "retry-new-consent", []string{"https://test.feishu.cn/file/real"})
	require.NoError(t, err)
	item := view.Items[0]
	require.NoError(
		t,
		f.db.Model(&item).Updates(map[string]any{"status": "blocked", "error_category": "authorization_invalid"}).Error,
	)
	require.NoError(t, f.db.Exec("UPDATE feishu_personal_grant_epochs SET epoch='renewed' WHERE user_id='A'").Error)
	require.NoError(t, f.s.RetryBatch(f.ctx, view.Batch.ID))
	var batch types.FeishuBatch
	require.NoError(t, f.db.Where("id=?", view.Batch.ID).Take(&batch).Error)
	require.NotEqual(t, view.Batch.ConnectionID, batch.ConnectionID)
	require.NoError(t, f.db.Where("id=?", item.ID).Take(&item).Error)
	require.Equal(t, "pending", item.Status)
	require.Equal(t, 1, item.RetryCount)
	var audit int64
	require.NoError(
		t,
		f.db.Model(&types.TaskDeadLetter{}).Where("task_type=?", "feishu:batch_retry_consent").Count(&audit).Error,
	)
	require.EqualValues(t, 1, audit)
	require.NoError(t, f.s.ReconcileBatches(context.Background()))
	require.NoError(t, f.db.Where("id=?", item.ID).Take(&item).Error)
	require.NotEmpty(t, item.ImportID)
}

func TestPersonalExactExplicitRetryKeepsServerAdmissionGates(t *testing.T) {
	f, a := closedExactResumeFixture(t)
	require.True(t, f.p.canResumePersonalExact(a))
	t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY_KNOWLEDGE_IDS", "")
	t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY_AUTOMATIC", "0")
	require.False(t, f.p.canResumePersonalExact(a))
	t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY_KNOWLEDGE_IDS", a.LogicalKnowledgeID)
	a.RetryCount = repository.PersonalExactResumeMaxAttempts
	require.False(t, f.p.canResumePersonalExact(a))
}

func TestPersonalExactExplicitBatchItemUsesCurrentAuthorization(t *testing.T) {
	f, observed := closedExactResumeFixture(t)
	m, _, _, err := f.p.repo.Load(context.Background(), observed.ID)
	require.NoError(t, err)
	ctx := personalRetryOwnerContext(m.UserID, observed.CallerTenantID)
	require.NoError(t, f.db.Exec("UPDATE knowledges SET parse_status='failed' WHERE id=?", observed.LogicalKnowledgeID).Error)
	require.NoError(
		t,
		f.db.Exec(
			"UPDATE feishu_personal_grant_epochs SET epoch='current-explicit-consent' WHERE user_id=?",
			m.UserID,
		).Error,
	)
	ref := types.FeishuCredentialRef{
		Version: 1, Kind: types.FeishuUserOAuth, OwnerID: m.UserID, AppID: m.AppID,
		OpenID: m.OpenID, GrantEpoch: "current-explicit-consent", ConfigVersion: observed.AppConfigVersion,
	}
	ref.ConnectionID = connectionID(ref.Kind, ref.OwnerID, ref.AppID, ref.OpenID, ref.GrantEpoch)
	batch := &types.FeishuBatch{
		ID: "exact-retry-batch", OwnerID: m.UserID, TenantID: observed.CallerTenantID, KBID: m.KBID,
		AuthMode: types.FeishuUserOAuth, ConfigVersion: observed.AppConfigVersion, ConnectionID: ref.ConnectionID,
	}
	require.NoError(t, f.s.retryExactBatchItem(ctx, batch, &types.FeishuBatchItem{ImportID: m.ID}, ref))
	current := mustPersonalAttempt(t, f, observed.ID)
	require.Equal(t, "index_pending", current.Status)
	require.Equal(t, ref.GrantEpoch, current.GrantEpoch)
	require.Equal(t, observed.RetryCount+1, current.RetryCount)
	require.Equal(t, observed.ID, current.ID)
	require.Equal(t, observed.LogicalKnowledgeID, current.LogicalKnowledgeID)
	require.Empty(t, current.OwnerNonce, "delivery cannot impersonate a consumer Claim")
	require.Nil(t, current.LeaseUntil)
}
