package repository

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func endpointSQLFixture(t *testing.T) (*gorm.DB, *mcpEndpointRepository, *types.MCPEndpoint) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "isolated-endpoints.sqlite")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.AutoMigrate(&types.MCPEndpoint{}); err != nil {
		t.Fatal(err)
	}
	r := &mcpEndpointRepository{db: db}
	ep := &types.MCPEndpoint{ID: "synthetic-endpoint", TenantID: 1, Name: "initial", Enabled: true, TokenHash: strings.Repeat("a", 64), TokenHint: "synthetic", Tools: types.StringArray{"search_knowledge"}, KnowledgeBaseIDs: types.StringArray{"KB-A"}, RateLimitPerMinute: 60}
	if err := r.Create(context.Background(), ep); err != nil {
		t.Fatal(err)
	}
	ep, err = r.GetByID(context.Background(), ep.ID)
	if err != nil {
		t.Fatal(err)
	}
	return db, r, ep
}

func TestMCPEndpointSQLWhitelistCASAndTouch(t *testing.T) {
	db, r, ep := endpointSQLFixture(t)
	// Parameter-free SQL only: no Statement.Vars or Explain interpolation.
	type write struct {
		SQL         string
		Rows        int64
		Transaction bool
		Failed      bool
	}
	var mu sync.Mutex
	var evidence []write
	if err := db.Callback().Update().After("gorm:update").Before("gorm:commit_or_rollback_transaction").Register("qa:parameter_free_endpoint_sql", func(tx *gorm.DB) {
		mu.Lock()
		defer mu.Unlock()
		evidence = append(evidence, write{SQL: tx.Statement.SQL.String(), Rows: tx.RowsAffected, Transaction: tx.Statement.ConnPool != db.Statement.ConnPool, Failed: tx.Error != nil})
	}); err != nil {
		t.Fatal(err)
	}
	old := *ep
	// Force a future clock to prove every mutation advances beyond a stored
	// version even when wall clock time cannot advance it.
	future := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	if err := db.Model(ep).UpdateColumn("updated_at", future).Error; err != nil {
		t.Fatal(err)
	}
	ep, _ = r.GetByID(context.Background(), ep.ID)
	old = *ep
	ep.Enabled = false
	ep.Description = ""
	ep.Tools = types.StringArray{}
	ep.KnowledgeBaseIDs = types.StringArray{}
	ep.RateLimitPerMinute = 0
	if err := r.Update(context.Background(), ep); err != nil {
		t.Fatal(err)
	}
	if !ep.UpdatedAt.After(future) {
		t.Fatal("CAS version did not advance beyond future timestamp")
	}
	loaded, err := r.GetByID(context.Background(), ep.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Enabled || len(loaded.Tools) != 0 || len(loaded.KnowledgeBaseIDs) != 0 || loaded.RateLimitPerMinute != 0 {
		t.Fatal("false/zero/empty fields silently omitted")
	}
	if loaded.TokenHash != old.TokenHash || loaded.TokenHint != old.TokenHint {
		t.Fatal("config write changed credentials")
	}
	old.Name = "stale restore"
	if err := r.Update(context.Background(), &old); err == nil {
		t.Fatal("stale config accepted")
	}
	beforeTouch := *loaded
	if err := r.TouchLastUsed(context.Background(), ep.ID); err != nil {
		t.Fatal(err)
	}
	loaded, _ = r.GetByID(context.Background(), ep.ID)
	if !loaded.UpdatedAt.Equal(beforeTouch.UpdatedAt) || loaded.TokenHash != beforeTouch.TokenHash || loaded.LastUsedAt == nil {
		t.Fatal("touch modified credential/config version")
	}
	if err := r.RotateToken(context.Background(), loaded, strings.Repeat("b", 64), "new-hint"); err != nil {
		t.Fatal(err)
	}
	if loaded.Enabled || len(loaded.KnowledgeBaseIDs) != 0 || len(loaded.Tools) != 0 {
		t.Fatal("rotation restored config")
	}
	if err := r.Delete(context.Background(), 1, ep.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.TouchLastUsed(context.Background(), ep.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.Update(context.Background(), loaded); err == nil {
		t.Fatal("deleted row updated")
	}
	var raw types.MCPEndpoint
	if err := db.Unscoped().First(&raw, "id = ?", ep.ID).Error; err != nil || !raw.DeletedAt.Valid {
		t.Fatal("touch/config resurrected row")
	}
	mu.Lock()
	defer mu.Unlock()
	for _, e := range evidence {
		if strings.Contains(e.SQL, old.TokenHash) || strings.Contains(e.SQL, strings.Repeat("b", 64)) {
			t.Fatal("SQL evidence interpolated credentials")
		}
		t.Logf("SQL=%s RowsAffected=%d transaction=%v error=%v", e.SQL, e.Rows, e.Transaction, e.Failed)
	}
}

func TestMCPEndpointMissingAndForeignConditionalWrites(t *testing.T) {
	_, r, ep := endpointSQLFixture(t)
	foreign := *ep
	foreign.TenantID = 2
	if err := r.Update(context.Background(), &foreign); err == nil {
		t.Fatal("cross-tenant update")
	}
	if err := r.RotateToken(context.Background(), &foreign, strings.Repeat("b", 64), "new"); err == nil {
		t.Fatal("cross-tenant rotate")
	}
	if err := r.Delete(context.Background(), 2, ep.ID); err == nil {
		t.Fatal("cross-tenant delete")
	}
	missing := *ep
	missing.ID = "missing"
	if err := r.Update(context.Background(), &missing); err == nil {
		t.Fatal("RowsAffected zero not rejected")
	}
	if err := r.Delete(context.Background(), 1, "missing"); err == nil {
		t.Fatal("missing delete not rejected")
	}
	current, err := r.GetByID(context.Background(), ep.ID)
	if err != nil || current.TokenHash != ep.TokenHash {
		t.Fatal("foreign operation changed row")
	}
}

// Real GORM PostgreSQL dialect and transaction callbacks, isolated SQL driver.
// This verifies emitted SQL/RowsAffected/commit propagation, NOT a PostgreSQL server.
func TestMCPEndpointPostgresConditionalWriteFailures(t *testing.T) {
	for _, operation := range []string{"config", "rotate", "delete"} {
		for _, failure := range []string{"zero-rows", "execute", "commit"} {
			t.Run(operation+"/"+failure, func(t *testing.T) {
				dbSQL, mock, err := sqlmock.New()
				if err != nil {
					t.Fatal(err)
				}
				defer dbSQL.Close()
				db, err := gorm.Open(postgres.New(postgres.Config{Conn: dbSQL}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
				if err != nil {
					t.Fatal(err)
				}
				r := &mcpEndpointRepository{db: db}
				ep := &types.MCPEndpoint{ID: "synthetic", TenantID: 1, Name: "name", TokenHash: strings.Repeat("a", 64), UpdatedAt: time.Now().UTC().Truncate(time.Microsecond)}
				before := *ep
				mock.ExpectBegin()
				query := `UPDATE "mcp_endpoints" SET .+ WHERE .*tenant_id = .*id = .*deleted_at IS NULL`
				if operation != "delete" {
					query += `.*updated_at = .*token_hash = `
				}
				exec := mock.ExpectExec(query)
				if failure == "execute" {
					exec.WillReturnError(errors.New("synthetic-secret database error"))
					mock.ExpectRollback()
				} else {
					rows := int64(1)
					if failure == "zero-rows" {
						rows = 0
					}
					exec.WillReturnResult(sqlmock.NewResult(0, rows))
					if failure == "commit" {
						mock.ExpectCommit().WillReturnError(errors.New("synthetic-secret commit error"))
					} else {
						mock.ExpectCommit()
					}
				}
				switch operation {
				case "config":
					err = r.Update(context.Background(), ep)
				case "rotate":
					err = r.RotateToken(context.Background(), ep, strings.Repeat("b", 64), "new")
				case "delete":
					err = r.Delete(context.Background(), 1, ep.ID)
				}
				if err == nil || strings.Contains(err.Error(), "synthetic-secret") {
					t.Fatal("failure not safely propagated")
				}
				var app *apperrors.AppError
				if !errors.As(err, &app) || (failure == "zero-rows" && app.Code != apperrors.ErrConflict) {
					t.Fatal("wrong safe error class")
				}
				if ep.TokenHash != before.TokenHash || !ep.UpdatedAt.Equal(before.UpdatedAt) {
					t.Fatal("failed write published new credential/version")
				}
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Fatal(err)
				}
				t.Logf("postgres dialect operation=%s failure=%s safely rejected", operation, failure)
			})
		}
	}
}

func TestMCPEndpointExplicitTransactionRollback(t *testing.T) {
	db, r, ep := endpointSQLFixture(t)
	ctx := context.Background()
	err := db.Transaction(func(tx *gorm.DB) error {
		txRepo := &mcpEndpointRepository{db: tx}
		loaded, err := txRepo.GetByID(ctx, ep.ID)
		if err != nil {
			return err
		}
		loaded.Name = "must roll back"
		if err := txRepo.Update(ctx, loaded); err != nil {
			return err
		}
		return errors.New("synthetic transaction abort")
	})
	if err == nil {
		t.Fatal("transaction did not abort")
	}
	loaded, err := r.GetByID(ctx, ep.ID)
	if err != nil || loaded.Name != ep.Name || !loaded.UpdatedAt.Equal(ep.UpdatedAt) {
		t.Fatal("rollback did not preserve SQL row")
	}
}
