package repository

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"log"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMCPOAuthRepositorySQLLogsAreRedacted(t *testing.T) {
	t.Setenv("SYSTEM_AES_KEY", "0123456789abcdef0123456789abcdef")
	var captured bytes.Buffer
	logging := logger.New(log.New(&captured, "", 0), logger.Config{LogLevel: logger.Info})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logging})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&types.MCPOAuthClient{}))
	require.NoError(t, db.Exec("CREATE TRIGGER reject_client BEFORE INSERT ON mcp_oauth_clients BEGIN SELECT RAISE(ABORT, 'synthetic-secret synthetic-access synthetic-refresh'); END").Error)
	captured.Reset()
	err = NewMCPOAuthRepository(db).SaveClient(context.Background(), &types.MCPOAuthClient{TenantID: 7, ServiceID: "svc", ClientID: "client", ClientSecret: "synthetic-secret"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "synthetic-secret", "exercise a real driver error containing sensitive data")
	require.Contains(t, captured.String(), "mcp_oauth_storage_failed")
	for _, secret := range []string{"synthetic-secret", "synthetic-access", "synthetic-refresh"} {
		require.NotContains(t, captured.String(), secret)
	}
	var count int64
	require.NoError(t, db.Model(&types.MCPOAuthClient{}).Count(&count).Error)
	require.Zero(t, count)
}

type oauthFailingEntropy struct{}

func (oauthFailingEntropy) Read([]byte) (int, error) {
	return 0, errors.New("synthetic encryption failure")
}

// No parallel tests: changing the entropy reader is confined to this synchronous
// SaveClient call. No manager/transport goroutines run in this package test.
func TestMCPOAuthClientFailClosedSQL(t *testing.T) {
	const key = "0123456789abcdef0123456789abcdef"
	for _, mode := range []string{"valid", "missing", "invalid", "entropy", "public", "forged-prefix"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("SYSTEM_AES_KEY", key)
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { _ = sqlDB.Close() })
			require.NoError(t, db.AutoMigrate(&types.MCPOAuthClient{}))
			repo := NewMCPOAuthRepository(db)
			ctx := context.Background()
			require.NoError(t, repo.SaveClient(ctx, &types.MCPOAuthClient{TenantID: 7, ServiceID: "existing", ClientID: "old", ClientSecret: "old-secret"}))
			var original string
			require.NoError(t, db.Raw("SELECT client_secret FROM mcp_oauth_clients WHERE service_id = ?", "existing").Scan(&original).Error)
			secret := "new-secret"
			switch mode {
			case "missing":
				t.Setenv("SYSTEM_AES_KEY", "")
			case "invalid":
				t.Setenv("SYSTEM_AES_KEY", "too-short")
			case "public":
				t.Setenv("SYSTEM_AES_KEY", "")
				secret = ""
			case "forged-prefix":
				secret = "enc:v1:not-real-ciphertext"
			}
			for _, service := range []string{"new", "existing"} {
				row := &types.MCPOAuthClient{ID: "id-" + service, TenantID: 7, ServiceID: service, ClientID: "new", ClientSecret: secret}
				if mode == "entropy" {
					saved := rand.Reader
					// Restore immediately, even on panic; UUID allocation is avoided with ID.
					func() {
						defer func() { rand.Reader = saved }()
						rand.Reader = oauthFailingEntropy{}
						err = repo.SaveClient(ctx, row)
					}()
				} else {
					err = repo.SaveClient(ctx, row)
				}
				var raw string
				var count int64
				require.NoError(t, db.Raw("SELECT count(*) FROM mcp_oauth_clients WHERE service_id = ?", service).Scan(&count).Error)
				if mode != "valid" && mode != "public" {
					require.Error(t, err)
					require.NotContains(t, err.Error(), secret)
					if service == "new" {
						require.Zero(t, count)
					} else {
						require.NoError(t, db.Raw("SELECT client_secret FROM mcp_oauth_clients WHERE service_id = ?", service).Scan(&raw).Error)
						require.Equal(t, original, raw)
					}
				} else {
					require.NoError(t, err)
					require.EqualValues(t, 1, count)
					require.NoError(t, db.Raw("SELECT client_secret FROM mcp_oauth_clients WHERE service_id = ?", service).Scan(&raw).Error)
					if mode == "valid" {
						require.Contains(t, raw, utils.EncPrefix)
						require.NotContains(t, raw, secret)
					} else {
						require.Empty(t, raw)
					}
				}
			}
			t.Setenv("SYSTEM_AES_KEY", key)
			require.NoError(t, repo.SaveClient(ctx, &types.MCPOAuthClient{TenantID: 7, ServiceID: "recovered", ClientID: "recovered", ClientSecret: "recovered-secret"}))
			row, err := repo.GetClient(ctx, 7, "recovered")
			require.NoError(t, err)
			require.Equal(t, "recovered-secret", row.ClientSecret)
			// Read existing ciphertext under the original key; a wrong key must error.
			t.Setenv("SYSTEM_AES_KEY", "fedcba9876543210fedcba9876543210")
			row, err = repo.GetClient(ctx, 7, "recovered")
			require.Error(t, err)
			require.Nil(t, row)
			t.Setenv("SYSTEM_AES_KEY", key)
			row, err = repo.GetClient(ctx, 7, "recovered")
			require.NoError(t, err)
			require.Equal(t, "recovered-secret", row.ClientSecret)
		})
	}
}
