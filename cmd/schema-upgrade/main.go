// schema-upgrade runs the official migration chain without starting workers.
package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"

	"github.com/Tencent/WeKnora/internal/database"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func run() error {
	from := flag.Uint("from", 0, "required clean starting version")
	to := flag.Uint("to", 0, "required final version")
	flag.Parse()
	if *from == 0 || *to <= *from {
		return errors.New("explicit increasing from/to versions are required")
	}
	dsn := os.Getenv("KNOWHUB_MIGRATION_DSN")
	if dsn == "" {
		for _, key := range []string{"DB_HOST", "DB_USER", "DB_NAME"} {
			if os.Getenv(key) == "" {
				return errors.New("database configuration is incomplete")
			}
		}
		port := os.Getenv("DB_PORT")
		if port == "" {
			port = "5432"
		}
		u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(os.Getenv("DB_HOST"), port),
			Path: "/" + os.Getenv("DB_NAME"), User: url.UserPassword(os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"))}
		query := u.Query()
		sslmode := os.Getenv("DB_SSLMODE")
		if sslmode == "" {
			sslmode = "disable"
		}
		query.Set("sslmode", sslmode)
		u.RawQuery = query.Encode()
		dsn = u.String()
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return errors.New("cannot open migration database")
	}
	defer db.Close()
	var version uint
	var dirty bool
	if err := db.QueryRow("SELECT version,dirty FROM schema_migrations").Scan(&version, &dirty); err != nil {
		return errors.New("cannot inspect migration state")
	}
	if dirty || (version != *from && version != *to) {
		return fmt.Errorf("unexpected migration state: version=%d dirty=%t", version, dirty)
	}
	if version == *to {
		fmt.Printf("schema=%d dirty=false unchanged=true\n", version)
		return nil
	}
	if err := database.RunMigrations(dsn); err != nil {
		return errors.New("official migration runner failed; inspect private migration logs")
	}
	if err := db.QueryRow("SELECT version,dirty FROM schema_migrations").Scan(&version, &dirty); err != nil {
		return errors.New("cannot verify migration state")
	}
	if version != *to || dirty {
		return errors.New("unexpected final migration state")
	}
	fmt.Printf("schema=%d dirty=false\n", version)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
