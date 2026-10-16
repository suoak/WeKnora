package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
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

func jitTestDB(t *testing.T, pg bool) (*userRepository, *gorm.DB, types.User) {
	r, db, legacy := enterpriseTestDB(t, pg)
	dialect := "sqlite"
	if pg {
		dialect = "versioned"
	}
	sql, e := os.ReadFile("../../../migrations/" + dialect + "/003029_sourceid_jit_registration.up.sql")
	require.NoError(t, e)
	require.NoError(t, db.Exec(string(sql)).Error)
	require.NoError(t, db.AutoMigrate(&types.TenantMember{}))
	if !pg {
		raw, _ := db.DB()
		raw.SetMaxOpenConns(1)
	}
	return r, db, legacy
}
func syntheticJITProvision(email string, calls *atomic.Int32) func(context.Context, interfaces.UserRepository) (*types.User, error) {
	return func(ctx context.Context, r interfaces.UserRepository) (*types.User, error) {
		calls.Add(1)
		u := &types.User{ID: uuid.NewString(), Email: email, Username: "synthetic-" + uuid.NewString(), IsActive: true, PasswordHash: "!synthetic-disabled", CreatedAt: time.Now(), UpdatedAt: time.Now()}
		return u, r.CreateUser(ctx, u)
	}
}
func TestSourceIDJITLifecycle(t *testing.T) {
	r, db, legacy := jitTestDB(t, false)
	ctx := context.Background()
	var calls atomic.Int32
	login := func(sub, email string) (*types.User, error) {
		return r.ResolveSourceIDEnterpriseOrJITIdentity(ctx, "synthetic-issuer", sub, "new-employee", email, "synthetic-binding", "synthetic-enterprise", time.Hour, "synthetic-jit", []string{"example.test"}, syntheticJITProvision(email, &calls))
	}
	u, e := login("new-sub", "new@example.test")
	require.NoError(t, e)
	require.NotEqual(t, legacy.ID, u.ID)
	require.Zero(t, u.TenantID)
	require.False(t, u.IsSystemAdmin)
	require.False(t, u.CanAccessAllTenants)
	firstID := u.ID
	u, e = login("new-sub", "changed@example.test")
	require.NoError(t, e)
	require.Equal(t, firstID, u.ID)
	require.EqualValues(t, 1, calls.Load())
	require.NoError(t, db.Model(&types.SourceIDAccountOwnership{}).Where("user_id=?", firstID).Update("valid_until", time.Now().Add(-time.Second)).Error)
	u, e = login("new-sub", "changed@example.test")
	require.NoError(t, e)
	require.Equal(t, firstID, u.ID)
	// Independently verified directory maintenance supersedes JIT renewal.
	require.NoError(t, db.Model(&types.SourceIDAccountOwnership{}).Where("user_id=?", firstID).Update("source_ref", "synthetic-independent-directory").Error)
	u, e = login("new-sub", "changed@example.test")
	require.NoError(t, e)
	require.Equal(t, firstID, u.ID)
	_, e = login("wrong-sub", "new@example.test")
	require.Error(t, e)
	var review types.SourceIDBindingReview
	require.NoError(t, db.Where("user_id=?", firstID).First(&review).Error)
	require.NoError(t, r.ReviewSourceIDBinding(ctx, review.ID, "synthetic-operator", "synthetic-revoke", "revoke"))
	_, e = login("new-sub", "new@example.test")
	require.Error(t, e)
	var n int64
	require.NoError(t, db.Model(&types.User{}).Count(&n).Error)
	require.EqualValues(t, 2, n)
	raw, e := json.Marshal(types.SourceIDJITRegistration{})
	require.NoError(t, e)
	require.Equal(t, "{}", string(raw))
}
func TestSourceIDJITConflictsRollbackAndHistoricalNoFallback(t *testing.T) {
	r, db, legacy := jitTestDB(t, false)
	ctx := context.Background()
	var calls atomic.Int32
	login := func(employee, email string, provision func(context.Context, interfaces.UserRepository) (*types.User, error)) error {
		_, err := r.ResolveSourceIDEnterpriseOrJITIdentity(ctx, "synthetic-issuer", "new-sub", employee, email, "synthetic-binding", "synthetic-enterprise", time.Hour, "synthetic-jit", []string{"example.test"}, provision)
		return err
	}
	require.Error(t, login("unowned-legacy", legacy.Email, syntheticJITProvision(legacy.Email, &calls)))
	require.Zero(t, calls.Load())
	require.Error(t, login("new-employee", "new@example.test", func(c context.Context, p interfaces.UserRepository) (*types.User, error) {
		u, _ := syntheticJITProvision("new@example.test", &calls)(c, p)
		return u, errors.New("synthetic-registration-failure")
	}))
	var n int64
	require.NoError(t, db.Model(&types.User{}).Count(&n).Error)
	require.EqualValues(t, 1, n)
	require.Error(t, login("new-employee", "new@example.test", func(c context.Context, p interfaces.UserRepository) (*types.User, error) {
		u, e := syntheticJITProvision("new@example.test", &calls)(c, p)
		u.IsSystemAdmin = true
		return u, e
	}))
	require.NoError(t, db.Model(&types.SourceIDJITRegistration{}).Count(&n).Error)
	require.Zero(t, n)
	require.NoError(t, db.Delete(&legacy).Error)
	require.Error(t, login("new-employee", legacy.Email, syntheticJITProvision(legacy.Email, &calls)))
	require.Error(t, login("new-employee", "person@evil.example.test", syntheticJITProvision("person@evil.example.test", &calls)))
}
func TestSourceIDJITDisabledCannotRenew(t *testing.T) {
	r, db, _ := jitTestDB(t, false)
	ctx := context.Background()
	var calls atomic.Int32
	login := func() (*types.User, error) {
		return r.ResolveSourceIDEnterpriseOrJITIdentity(ctx, "synthetic-issuer", "sub", "employee", "new@example.test", "binding", "enterprise", time.Hour, "jit", []string{"example.test"}, syntheticJITProvision("new@example.test", &calls))
	}
	u, e := login()
	require.NoError(t, e)
	require.NoError(t, db.Model(&types.SourceIDAccountOwnership{}).Where("user_id=?", u.ID).Update("status", "disabled").Error)
	_, e = login()
	require.Error(t, e)
	require.EqualValues(t, 1, calls.Load())
}
func TestSourceIDPostgresJITConcurrentRegistration(t *testing.T) {
	r, db, _ := jitTestDB(t, true)
	ctx := context.Background()
	var calls atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	ids := make(chan string, 12)
	start := time.Now()
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			u, e := r.ResolveSourceIDEnterpriseOrJITIdentity(ctx, "synthetic-issuer", "sub", "new-employee", "new@example.test", "binding", "enterprise", time.Hour, "jit", []string{"example.test"}, syntheticJITProvision("new@example.test", &calls))
			errs <- e
			if e == nil {
				ids <- u.ID
			}
		}()
	}
	wg.Wait()
	close(errs)
	close(ids)
	for e := range errs {
		require.NoError(t, e)
	}
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		require.Equal(t, first, id)
	}
	require.EqualValues(t, 1, calls.Load())
	var n int64
	require.NoError(t, db.Model(&types.SourceIDJITRegistration{}).Count(&n).Error)
	require.EqualValues(t, 1, n)
	require.NoError(t, db.Model(&types.TenantMember{}).Count(&n).Error)
	require.Zero(t, n)
	t.Logf("synthetic_jit_concurrent=12 registrations=1 duration_ms=%d", time.Since(start).Milliseconds())
}

// Includes the external-only credential representation and safety lock.
// A bounded synthetic sample, not an end-to-end SID or production SLA claim.
func TestSourceIDPostgresJITDistinctRegistrationCapacity(t *testing.T) {
	r, db, _ := jitTestDB(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	errs := make(chan error, 24)
	latencies := make(chan int64, 24)
	start := time.Now()
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			began := time.Now()
			email := fmt.Sprintf("jit-capacity-%d@example.test", i)
			_, err := r.ResolveSourceIDEnterpriseOrJITIdentity(ctx, "synthetic-issuer", fmt.Sprintf("sub-%d", i), fmt.Sprintf("employee-%d", i), email, "binding", "enterprise", time.Hour, "jit", []string{"example.test"}, func(c context.Context, repo interfaces.UserRepository) (*types.User, error) {
				u := &types.User{ID: uuid.NewString(), Email: email, Username: uuid.NewString(), PasswordHash: "!sourceid-jit-no-local-password", IsActive: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}
				return u, repo.CreateUser(c, u)
			})
			errs <- err
			latencies <- time.Since(began).Milliseconds()
		}(i)
	}
	wg.Wait()
	close(errs)
	close(latencies)
	for err := range errs {
		require.NoError(t, err)
	}
	var n int64
	require.NoError(t, db.Model(&types.SourceIDJITRegistration{}).Count(&n).Error)
	require.EqualValues(t, 24, n)
	require.NoError(t, db.Model(&types.User{}).Where("is_system_admin=false AND can_access_all_tenants=false AND tenant_id IS NULL").Count(&n).Error)
	require.EqualValues(t, 24, n)
	times := make([]int64, 0, 24)
	for ms := range latencies {
		times = append(times, ms)
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	t.Logf("synthetic_jit_distinct=24 concurrent=8 duration_ms=%d p95_ms=%d", time.Since(start).Milliseconds(), times[22])
}
