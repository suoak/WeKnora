package service

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestSourceIDPostgresJITEmailUsernameConcurrentIdentity(t *testing.T) {
	dsn := os.Getenv("SOURCEID_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("isolated PostgreSQL fixture not configured")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	ar, err := admin.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = ar.Close() })
	schema := "sourceid_username_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, admin.Exec("CREATE SCHEMA "+schema).Error)
	t.Cleanup(func() { _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error })
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(&types.User{}, &types.Tenant{}, &types.TenantMember{}, &types.AuthToken{}))
	require.NoError(t, db.Exec("DROP INDEX IF EXISTS idx_auth_tokens_session_id").Error)
	require.NoError(t, db.Exec("ALTER TABLE auth_tokens DROP COLUMN session_id").Error)
	for _, name := range []string{
		"003024_sourceid_oidc", "003026_sourceid_binding_review", "003027_sourceid_local_session",
		"003028_sourceid_enterprise_ownership", "003029_sourceid_jit_registration",
	} {
		sql, err := os.ReadFile("../../../migrations/versioned/" + name + ".up.sql")
		require.NoError(t, err)
		require.NoError(t, db.Exec(string(sql)).Error)
	}
	repo := apprepo.NewUserRepository(db)
	store := repo.(sourceIDJITStore)
	s := &userService{config: &config.Config{}, userRepo: repo}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	login := func(employee, email string) (*types.User, error) {
		return store.ResolveSourceIDEnterpriseOrJITIdentity(
			ctx, "synthetic-issuer", "sub-"+employee, employee, email, "binding", "ownership", time.Hour, "jit",
			[]string{"example.test", "other.test"}, s.sourceIDJITProvisioner(email),
		)
	}
	type result struct {
		u *types.User
		e error
	}
	results := make(chan result, 12)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			u, e := login("employee-1", "employee@example.test")
			results <- result{u, e}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	id := ""
	for r := range results {
		require.NoError(t, r.e)
		require.Equal(t, "employee", r.u.Username)
		if id == "" {
			id = r.u.ID
		}
		require.Equal(t, id, r.u.ID)
		require.False(t, r.u.IsSystemAdmin)
		require.False(t, r.u.CanAccessAllTenants)
		require.Zero(t, r.u.TenantID)
	}
	// Different employee, same local part: keep identities separate under contention.
	results = make(chan result, 2)
	start = make(chan struct{})
	for i := 2; i <= 3; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			domain := map[int]string{2: "other.test", 3: "example.test"}[i]
			u, e := login(fmt.Sprintf("employee-%d", i), "employee@"+domain)
			results <- result{u, e}
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	// employee-3 reuses an existing employee's email and must not acquire that account.
	success, rejected := 0, 0
	for r := range results {
		if r.e == nil {
			success++
			require.Equal(t, "employee-2", r.u.Username)
			require.NotEqual(t, id, r.u.ID)
		} else {
			rejected++
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, rejected)
	stable, err := login("employee-1", "employee@example.test")
	require.NoError(t, err)
	require.Equal(t, id, stable.ID)
	require.Equal(t, "employee", stable.Username)
	var n int64
	require.NoError(t, db.Model(&types.User{}).Count(&n).Error)
	require.EqualValues(t, 2, n)
	require.NoError(t, db.Model(&types.SourceIDJITRegistration{}).Count(&n).Error)
	require.EqualValues(t, 2, n)
	require.NoError(t, db.Model(&types.TenantMember{}).Count(&n).Error)
	require.Zero(t, n)
	_, err = store.ResolveSourceIDEnterpriseOrJITIdentity(
		ctx, "synthetic-issuer", "wrong-sub", "employee-1", "employee@example.test", "binding", "ownership",
		time.Hour, "jit", []string{"example.test"}, s.sourceIDJITProvisioner("employee@example.test"),
	)
	require.Error(t, err)
}
