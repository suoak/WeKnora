// Migration-only offline entrypoint. It does not initialize application services.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"
)

func main() {
	stage := "configuration"
	result := map[string]any{"ok": false}
	run := func() error {
		mode := os.Getenv("KNOWHUB_MIGRATION_MODE")
		if mode == "" {
			mode = "check"
		}
		dsn, expected, dir := os.Getenv(
			"KNOWHUB_MIGRATION_DSN",
		), os.Getenv(
			"KNOWHUB_MIGRATION_DATABASE",
		), os.Getenv(
			"KNOWHUB_MIGRATION_DIR",
		)
		if (mode != "check" && mode != "up") || dsn == "" || expected == "" || !filepath.IsAbs(dir) {
			return errors.New("invalid configuration")
		}
		files, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
		if err != nil {
			return err
		}
		pending := map[int]bool{}
		for _, file := range files {
			v, err := strconv.Atoi(strings.SplitN(filepath.Base(file), "_", 2)[0])
			if err != nil || v > 3033 || v == 3032 {
				return errors.New("unapproved migration inventory")
			}
			if v > 3023 {
				pending[v] = true
			}
		}
		if len(pending) != 9 {
			return errors.New("migration inventory incomplete")
		}
		stage = "read_only_preflight"
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			return err
		}
		defer func(cleanup func() error) { _ = cleanup() }(db.Close)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return err
		}
		defer func(cleanup func() error) { _ = cleanup() }(tx.Rollback)
		var actual string
		var version uint
		var dirty bool
		if err := tx.QueryRowContext(ctx, "SELECT current_database()").Scan(&actual); err != nil {
			return err
		}
		if actual != expected {
			return errors.New("unexpected database")
		}
		var rows int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations").Scan(&rows); err != nil ||
			rows != 1 {
			return errors.New("migration state is ambiguous")
		}
		if err := tx.QueryRowContext(
			ctx,
			"SELECT version,dirty FROM schema_migrations",
		).
			Scan(
				&version,
				&dirty,
			); err != nil {
			return err
		}
		result["database"], result["version"], result["dirty"], result["mode"] = actual, version, dirty, mode
		result["before_version"], result["before_dirty"] = version, dirty
		if dirty || version < 3023 || version > 3033 || version == 3032 {
			return errors.New("unexpected migration state")
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		if mode == "check" {
			return nil
		}
		stage = "migrate_up"
		m, err := migrate.New("file://"+filepath.ToSlash(dir), dsn)
		if err != nil {
			return err
		}
		defer func(cleanup func() (error, error)) { _, _ = cleanup() }(m.Close)
		// No Force, Down, or automatic dirty recovery path is provided.
		if err := m.Up(); err != nil && err != migrate.ErrNoChange {
			if v, d, readErr := m.Version(); readErr == nil {
				result["version"], result["dirty"] = v, d
			} else {
				delete(result, "version")
				delete(result, "dirty")
				result["current_state_unconfirmed"] = true
			}
			return err
		}
		stage = "postflight"
		version, dirty, err = m.Version()
		if err != nil || dirty || version != 3033 {
			return errors.New("migration result unconfirmed")
		}
		result["version"], result["dirty"] = version, dirty
		return nil
	}
	started := time.Now()
	err := run()
	result["ok"], result["stage"], result["elapsed_ms"] = err == nil, stage, time.Since(started).
		Milliseconds()
	// Driver errors can contain connection strings. Report the failing stage,
	// never the DSN or raw driver message. Diagnose in a controlled environment.
	_ = json.NewEncoder(os.Stdout).Encode(result)
	if err != nil {
		os.Exit(1)
	}
}
