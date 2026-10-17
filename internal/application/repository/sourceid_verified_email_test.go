package repository

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func verifiedEmailDB(t *testing.T, pg bool) (*userRepository, *gorm.DB, types.User) {
	r, db, u := jitTestDB(t, pg)
	dialect := "sqlite"
	if pg {
		dialect = "versioned"
	} else {
		sql, e := os.ReadFile("../../../migrations/sqlite/003027_sourceid_local_session.up.sql")
		require.NoError(t, e)
		require.NoError(t, db.Exec(string(sql)).Error)
	}
	sql, e := os.ReadFile("../../../migrations/" + dialect + "/003030_sourceid_verified_email_policy.up.sql")
	require.NoError(t, e)
	require.NoError(t, db.Exec(string(sql)).Error)
	return r, db, u
}
func verifiedEmailInput() types.SourceIDVerifiedEmailLogin {
	return types.SourceIDVerifiedEmailLogin{Issuer: "synthetic-issuer", Subject: "synthetic-sub", EmployeeNo: "synthetic-employee", Email: " employee@EXAMPLE.test ", PolicyRef: "synthetic-email-policy-v1", BindingPolicyRef: "synthetic-auto-v1", JITPolicyRef: "synthetic-jit", JITDomains: []string{"example.test"}}
}
func verifiedEmailSession() (*types.SourceIDSession, *types.SourceIDLocalSession) {
	now := time.Now()
	id := uuid.NewString()
	return &types.SourceIDSession{ID: id, Issuer: "synthetic-issuer", ExpiresAt: now.Add(time.Hour), CreatedAt: now}, &types.SourceIDLocalSession{ID: id, Subject: "synthetic-sub", PolicyRef: "synthetic-local", AdmissionPolicyRef: "synthetic-sso", AdmissionSource: "oidc_sso_approved", AdmissionCheckedAt: now, AdmissionValidUntil: now.Add(time.Hour), AccessSeconds: 3600, IdleSeconds: 1800, LastSeenAt: now}
}
func TestSourceIDVerifiedEmailAtomicLifecycleAllRoles(t *testing.T) {
	for _, role := range []string{"viewer", "contributor", "owner", "system_admin", "cross_space_admin"} {
		t.Run(role, func(t *testing.T) {
			r, db, u := verifiedEmailDB(t, false)
			ctx := context.Background()
			require.NoError(t, db.Model(&u).Updates(map[string]interface{}{"is_system_admin": role == "system_admin", "can_access_all_tenants": role == "cross_space_admin"}).Error)
			require.NoError(t, db.First(&u, "id=?", u.ID).Error)
			q := verifiedEmailInput()
			s, l := verifiedEmailSession()
			bound, created, e := r.ResolveSourceIDVerifiedEmailSession(ctx, q, s, l, nil)
			require.NoError(t, e)
			require.False(t, created)
			require.Equal(t, u.ID, bound.ID)
			require.Equal(t, u.IsSystemAdmin, bound.IsSystemAdmin)
			require.Equal(t, u.CanAccessAllTenants, bound.CanAccessAllTenants)
			require.Equal(t, u.TenantID, bound.TenantID)
			require.NoError(t, r.CheckSourceIDVerifiedEmailSession(ctx, s.ID, u.ID, q.Issuer, q.EmployeeNo, q.Subject, q.PolicyRef, q.BindingPolicyRef))
			var n int64
			require.NoError(t, db.Model(&types.SourceIDAccountOwnership{}).Count(&n).Error)
			require.Zero(t, n)
			var audit types.SourceIDBindingAudit
			require.NoError(t, db.First(&audit).Error)
			require.Equal(t, "company-verified-email-policy", audit.ReviewedBy)
			q.Email = "changed@example.test"
			s2, l2 := verifiedEmailSession()
			again, _, e := r.ResolveSourceIDVerifiedEmailSession(ctx, q, s2, l2, nil)
			require.NoError(t, e)
			require.Equal(t, u.ID, again.ID)
			require.Error(t, r.CheckSourceIDVerifiedEmailSession(ctx, s.ID, u.ID, q.Issuer, q.EmployeeNo, q.Subject, "changed-policy", q.BindingPolicyRef))
			var review types.SourceIDBindingReview
			require.NoError(t, db.First(&review).Error)
			require.NoError(t, r.ReviewSourceIDBinding(ctx, review.ID, "synthetic-admin", "synthetic-revoke", "revoke"))
			require.Error(t, r.CheckSourceIDSession(ctx, s.ID, u.ID))
			s3, l3 := verifiedEmailSession()
			_, _, e = r.ResolveSourceIDVerifiedEmailSession(ctx, q, s3, l3, nil)
			require.Error(t, e)
		})
	}
}

func TestSourceIDVerifiedEmailRejectsExceptionsNoJITFallback(t *testing.T) {
	for _, scenario := range []string{"duplicate", "soft_deleted", "deleted_duplicate", "inactive", "disabled_ownership", "expired_ownership", "wrong_employee", "known_transfer", "subject_conflict", "revoked", "session_insert_failure", "session_id_failure"} {
		t.Run(scenario, func(t *testing.T) {
			r, db, u := verifiedEmailDB(t, false)
			ctx := context.Background()
			q := verifiedEmailInput()
			q.JITEnabled = true
			s, l := verifiedEmailSession()
			var calls atomic.Int32
			switch scenario {
			case "duplicate", "deleted_duplicate":
				other := types.User{ID: uuid.NewString(), Username: "synthetic-other", Email: " employee@example.test ", IsActive: true}
				require.NoError(t, db.Create(&other).Error)
				if scenario == "deleted_duplicate" {
					require.NoError(t, db.Delete(&other).Error)
				}
			case "soft_deleted":
				require.NoError(t, db.Delete(&u).Error)
			case "inactive":
				require.NoError(t, db.Model(&u).Update("is_active", false).Error)
			case "disabled_ownership", "expired_ownership", "wrong_employee":
				p := proof(u)
				if scenario == "disabled_ownership" {
					p.Status = "disabled"
				}
				if scenario == "wrong_employee" {
					p.EmployeeNo = "synthetic-other"
				}
				_, e := r.SyncSourceIDAccountOwnership(ctx, p, "synthetic-operator", true)
				require.NoError(t, e)
				if scenario == "expired_ownership" {
					require.NoError(t, db.Model(&types.SourceIDAccountOwnership{}).Where("user_id=?", u.ID).Update("valid_until", time.Now().Add(-time.Minute)).Error)
				}
			case "known_transfer":
				require.NoError(t, r.RecordSourceIDLinkBlock(ctx, types.SourceIDLinkBlock{Issuer: q.Issuer, Email: u.Email, Reason: "email_reassigned", Actor: "synthetic-security", Reference: "synthetic-case"}))
			case "subject_conflict", "revoked":
				_, _, e := r.ResolveSourceIDVerifiedEmailSession(ctx, q, s, l, nil)
				require.NoError(t, e)
				var row types.SourceIDBindingReview
				require.NoError(t, db.First(&row).Error)
				if scenario == "revoked" {
					require.NoError(t, r.ReviewSourceIDBinding(ctx, row.ID, "synthetic-admin", "synthetic-case", "revoke"))
				} else {
					q.Subject = "synthetic-other-sub"
				}
				s, l = verifiedEmailSession()
				l.Subject = q.Subject
			case "session_id_failure":
				l.ID = uuid.NewString()
			case "session_insert_failure":
				require.NoError(t, db.Exec("CREATE TRIGGER synthetic_fail_session BEFORE INSERT ON source_id_local_link_policies BEGIN SELECT RAISE(ABORT,'synthetic rollback'); END").Error)
			}
			var before int64
			require.NoError(t, db.Model(&types.ExternalIdentity{}).Count(&before).Error)
			_, _, e := r.ResolveSourceIDVerifiedEmailSession(ctx, q, s, l, syntheticJITProvision("employee@example.test", &calls))
			require.Error(t, e)
			require.Zero(t, calls.Load())
			var after int64
			require.NoError(t, db.Model(&types.ExternalIdentity{}).Count(&after).Error)
			require.Equal(t, before, after)
			if scenario == "session_insert_failure" {
				for _, m := range []interface{}{&types.SourceIDBindingReview{}, &types.SourceIDBindingAudit{}, &types.SourceIDLinkDecision{}, &types.SourceIDSession{}, &types.SourceIDLocalSession{}} {
					require.NoError(t, db.Model(m).Count(&after).Error)
					require.Zero(t, after)
				}
			}
		})
	}
}

func TestSourceIDVerifiedEmailJITAndBlockedAccess(t *testing.T) {
	r, db, _ := verifiedEmailDB(t, false)
	ctx := context.Background()
	q := verifiedEmailInput()
	q.Email = "new@example.test"
	q.JITEnabled = true
	var calls atomic.Int32
	s, l := verifiedEmailSession()
	u, created, e := r.ResolveSourceIDVerifiedEmailSession(ctx, q, s, l, syntheticJITProvision(q.Email, &calls))
	require.NoError(t, e)
	require.True(t, created)
	require.Zero(t, u.TenantID)
	require.False(t, u.IsSystemAdmin)
	require.False(t, u.CanAccessAllTenants)
	a := &types.AuthToken{ID: uuid.NewString(), UserID: u.ID, SessionID: s.ID, Token: "synthetic-access", TokenType: "access_token", ExpiresAt: time.Now().Add(time.Minute)}
	require.NoError(t, r.PersistSourceIDTokens(ctx, s.ID, a, nil, ""))
	refresh := &types.AuthToken{ID: uuid.NewString(), UserID: u.ID, SessionID: s.ID, Token: "synthetic-refresh", TokenType: "refresh_token", ExpiresAt: time.Now().Add(time.Minute)}
	require.Error(t, r.PersistSourceIDTokens(ctx, s.ID, a, refresh, ""))
	q.Email = "changed@example.test"
	s2, l2 := verifiedEmailSession()
	again, created, e := r.ResolveSourceIDVerifiedEmailSession(ctx, q, s2, l2, syntheticJITProvision(q.Email, &calls))
	require.NoError(t, e)
	require.False(t, created)
	require.Equal(t, u.ID, again.ID)
	require.EqualValues(t, 1, calls.Load())
	require.NoError(t, r.RecordSourceIDLinkBlock(ctx, types.SourceIDLinkBlock{Issuer: q.Issuer, UserID: u.ID, Reason: "identity_disabled", Actor: "synthetic-security", Reference: "synthetic-case"}))
	require.Error(t, r.CheckSourceIDSession(ctx, s.ID, u.ID))
	var token types.AuthToken
	require.NoError(t, db.First(&token, "id=?", a.ID).Error)
	require.True(t, token.IsRevoked)
	require.Error(t, r.CheckSourceIDVerifiedEmailSession(ctx, s2.ID, u.ID, q.Issuer, q.EmployeeNo, q.Subject, q.PolicyRef, q.BindingPolicyRef))
}

func TestSourceIDVerifiedEmailJITFailureRollsBack(t *testing.T) {
	r, db, _ := verifiedEmailDB(t, false)
	q := verifiedEmailInput()
	q.Email = "new@example.test"
	q.JITEnabled = true
	s, l := verifiedEmailSession()
	var before int64
	require.NoError(t, db.Model(&types.User{}).Count(&before).Error)
	_, _, err := r.ResolveSourceIDVerifiedEmailSession(context.Background(), q, s, l, func(ctx context.Context, repo interfaces.UserRepository) (*types.User, error) {
		u := &types.User{ID: uuid.NewString(), Email: q.Email, Username: "synthetic", IsActive: true}
		require.NoError(t, repo.CreateUser(ctx, u))
		return nil, errors.New("synthetic failure")
	})
	require.Error(t, err)
	var after int64
	require.NoError(t, db.Model(&types.User{}).Count(&after).Error)
	require.Equal(t, before, after)
}

func TestSourceIDPostgresVerifiedEmailConcurrentFirstAndJIT(t *testing.T) {
	for _, jit := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing", true: "jit"}[jit], func(t *testing.T) {
			r, db, legacy := verifiedEmailDB(t, true)
			q := verifiedEmailInput()
			q.JITEnabled = jit
			if jit {
				q.Email = "new@example.test"
			}
			var calls, success atomic.Int32
			var wg sync.WaitGroup
			ids := make(chan string, 12)
			for range 12 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					s, l := verifiedEmailSession()
					u, _, e := r.ResolveSourceIDVerifiedEmailSession(context.Background(), q, s, l, syntheticJITProvision(q.Email, &calls))
					if e == nil {
						success.Add(1)
						ids <- u.ID
					}
				}()
			}
			wg.Wait()
			close(ids)
			require.EqualValues(t, 12, success.Load())
			first := ""
			for id := range ids {
				if first == "" {
					first = id
				}
				require.Equal(t, first, id)
			}
			if !jit {
				require.Equal(t, legacy.ID, first)
				require.Zero(t, calls.Load())
			} else {
				require.NotEqual(t, legacy.ID, first)
				require.EqualValues(t, 1, calls.Load())
			}
			for _, m := range []interface{}{&types.SourceIDBindingReview{}, &types.ExternalIdentity{}, &types.SourceIDEnrollment{}, &types.SourceIDLinkDecision{}} {
				var n int64
				require.NoError(t, db.Model(m).Count(&n).Error)
				require.EqualValues(t, 1, n)
			}
			var n int64
			require.NoError(t, db.Model(&types.SourceIDSession{}).Count(&n).Error)
			require.EqualValues(t, 12, n)
		})
	}
}

func TestSourceIDPostgresVerifiedEmailRevokeRace(t *testing.T) {
	r, db, u := verifiedEmailDB(t, true)
	ctx := context.Background()
	q := verifiedEmailInput()
	s, l := verifiedEmailSession()
	_, _, e := r.ResolveSourceIDVerifiedEmailSession(ctx, q, s, l, nil)
	require.NoError(t, e)
	var review types.SourceIDBindingReview
	require.NoError(t, db.First(&review).Error)
	var wg sync.WaitGroup
	start := make(chan struct{})
	fail := make(chan error, 1)
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		s, l := verifiedEmailSession()
		_, _, _ = r.ResolveSourceIDVerifiedEmailSession(ctx, q, s, l, nil)
	}()
	go func() {
		defer wg.Done()
		<-start
		fail <- r.ReviewSourceIDBinding(ctx, review.ID, "synthetic-security", "synthetic-revoke", "revoke")
	}()
	close(start)
	wg.Wait()
	require.NoError(t, <-fail)
	var n int64
	require.NoError(t, db.Model(&types.SourceIDSession{}).Where("user_id=? AND NOT revoked", u.ID).Count(&n).Error)
	require.Zero(t, n)
}

func TestSourceIDPostgresVerifiedEmailJITCannotRaceExistingLink(t *testing.T) {
	r, db, u := verifiedEmailDB(t, true)
	var calls, success atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, jit := range []bool{false, true} {
		wg.Add(1)
		go func(jit bool) {
			defer wg.Done()
			<-start
			q := verifiedEmailInput()
			q.JITEnabled = jit
			if jit {
				q.EmployeeNo = "synthetic-other-employee"
				q.Subject = "synthetic-other-sub"
			}
			s, l := verifiedEmailSession()
			l.Subject = q.Subject
			bound, _, e := r.ResolveSourceIDVerifiedEmailSession(context.Background(), q, s, l, syntheticJITProvision("employee@example.test", &calls))
			if e == nil && bound.ID == u.ID {
				success.Add(1)
			}
		}(jit)
	}
	close(start)
	wg.Wait()
	require.EqualValues(t, 1, success.Load())
	require.Zero(t, calls.Load())
	var n int64
	require.NoError(t, db.Model(&types.User{}).Count(&n).Error)
	require.EqualValues(t, 1, n)
	require.NoError(t, db.Model(&types.ExternalIdentity{}).Count(&n).Error)
	require.EqualValues(t, 1, n)
}

func TestSourceIDPostgresVerifiedEmailBlocksConcurrentEmailInsert(t *testing.T) {
	r, db, u := verifiedEmailDB(t, true)
	q := verifiedEmailInput()
	entered := make(chan struct{})
	release := make(chan struct{})
	loginDone := make(chan error, 1)
	insertDone := make(chan error, 1)
	var once sync.Once
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("synthetic_verified_first_review", func(tx *gorm.DB) {
		if tx.Statement.Table == "source_id_binding_reviews" {
			once.Do(func() { close(entered); <-release })
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Create().Remove("synthetic_verified_first_review") })
	go func() {
		s, l := verifiedEmailSession()
		_, _, e := r.ResolveSourceIDVerifiedEmailSession(context.Background(), q, s, l, nil)
		loginDone <- e
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("first review not reached")
	}
	go func() {
		insertDone <- db.Create(&types.User{ID: uuid.NewString(), Username: "synthetic-competing-registration", Email: "EMPLOYEE@example.test", IsActive: true}).Error
	}()
	select {
	case <-insertDone:
		close(release)
		t.Fatal("users lock failed to serialize concurrent email insertion")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-loginDone)
	require.NoError(t, <-insertDone)
	// The independent later insert cannot retarget the established employee.
	s, l := verifiedEmailSession()
	bound, _, e := r.ResolveSourceIDVerifiedEmailSession(context.Background(), q, s, l, nil)
	require.NoError(t, e)
	require.Equal(t, u.ID, bound.ID)
}

func TestSourceIDVerifiedEmailMissingMetadataFailsAtomically(t *testing.T) {
	r, db, _ := verifiedEmailDB(t, false)
	require.NoError(t, db.Exec("DROP TABLE source_id_local_link_policies").Error)
	s, l := verifiedEmailSession()
	_, _, e := r.ResolveSourceIDVerifiedEmailSession(context.Background(), verifiedEmailInput(), s, l, nil)
	require.Error(t, e)
	var n int64
	require.NoError(t, db.Model(&types.ExternalIdentity{}).Count(&n).Error)
	require.Zero(t, n)
	// Pre-3030 trusted mode can still see an unmarked local session as legacy.
	marker, e := r.SourceIDSessionLinkPolicy(context.Background(), uuid.NewString())
	require.NoError(t, e)
	require.Nil(t, marker)
}

func TestSourceIDPostgresVerifiedEmailDirectoryDisableRace(t *testing.T) {
	r, db, u := verifiedEmailDB(t, true)
	ctx := context.Background()
	q := verifiedEmailInput()
	p := proof(u)
	_, e := r.SyncSourceIDAccountOwnership(ctx, p, "synthetic-directory", true)
	require.NoError(t, e)
	s, l := verifiedEmailSession()
	_, _, e = r.ResolveSourceIDVerifiedEmailSession(ctx, q, s, l, nil)
	require.NoError(t, e)
	var wg sync.WaitGroup
	done := make(chan error, 1)
	start := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		s, l := verifiedEmailSession()
		_, _, _ = r.ResolveSourceIDVerifiedEmailSession(ctx, q, s, l, nil)
	}()
	go func() {
		defer wg.Done()
		<-start
		p.Revision++
		p.Status = "disabled"
		_, e := r.SyncSourceIDAccountOwnership(ctx, p, "synthetic-directory", true)
		done <- e
	}()
	close(start)
	wg.Wait()
	require.NoError(t, <-done)
	var n int64
	require.NoError(t, db.Model(&types.SourceIDSession{}).Where("user_id=? AND NOT revoked", u.ID).Count(&n).Error)
	require.Zero(t, n)
	require.Error(t, r.CheckSourceIDVerifiedEmailSession(ctx, s.ID, u.ID, q.Issuer, q.EmployeeNo, q.Subject, q.PolicyRef, q.BindingPolicyRef))
}
