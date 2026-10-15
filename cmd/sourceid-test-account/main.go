// Operator CLI only: no HTTP listener, configuration loading, migrations or SID calls.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/accountprep"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func run() error {
	// Invalid flag values may contain an accidentally pasted email or DSN.
	// Never let the standard parser echo them into ordinary console logs.
	flag.CommandLine.SetOutput(io.Discard)
	in := accountprep.Input{}
	flag.BoolVar(&in.Apply, "apply", false, "explicit approved mutation; default dry-run")
	flag.BoolVar(&in.Cleanup, "detach", false, "detach only this operation's membership; never delete user")
	flag.BoolVar(&in.ContributorApproved, "contributor-approved", false, "explicit contributor approval")
	flag.Uint64Var(&in.TenantID, "tenant", 0, "existing approved test space ID")
	flag.StringVar(&in.OperationID, "operation", "", "UUID recorded in protected audit")
	flag.StringVar(&in.OperatorID, "operator", "", "active owner user ID")
	flag.StringVar(&in.Approval, "approval", "", "real approval reference; required even for planning")
	role := flag.String("role", "viewer", "viewer or explicitly approved contributor")
	driver := flag.String("driver", "postgres", "postgres or synthetic sqlite")
	host := flag.String("test-host", "", "must explicitly identify 172.30.232.42 for PostgreSQL")
	protectedJSON := flag.Bool("protected-json-stdin", false, "protected launcher supplies email and database DSN via stdin, never argv")
	flag.Parse()
	if flag.NArg() != 0 {
		return errors.New("positional_input_forbidden")
	}
	// A protected launcher passes a single hidden-input line through a pipe.
	// Refuse a visible terminal rather than echoing an employee email.
	stat, err := os.Stdin.Stat()
	if err != nil || stat.Mode()&os.ModeCharDevice != 0 {
		return errors.New("protected_stdin_pipe_required")
	}
	limit := int64(256)
	if *protectedJSON {
		limit = 8192
	}
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return errors.New("protected_input_invalid")
	}
	dsn := os.Getenv("SOURCEID_TEST_ACCOUNT_DSN")
	if *protectedJSON {
		var protected struct {
			Email string `json:"email"`
			DSN   string `json:"database_dsn"`
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if dec.Decode(&protected) != nil || dec.Decode(new(any)) != io.EOF {
			return errors.New("protected_input_invalid")
		}
		in.Email, dsn = protected.Email, protected.DSN
	} else {
		in.Email = strings.TrimSpace(string(raw))
	}
	clear(raw)
	in.Role = types.TenantRole(*role)
	if dsn == "" {
		return errors.New("protected_database_configuration_required")
	}
	var dialect gorm.Dialector
	switch *driver {
	case "postgres":
		if *host != "172.30.232.42" {
			return errors.New("approved_test_host_required")
		}
		dialect = postgres.Open(dsn)
	case "sqlite":
		if !strings.HasSuffix(in.Email, ".test") && !strings.HasSuffix(in.Email, ".invalid") {
			return errors.New("sqlite_requires_synthetic_email")
		}
		dialect = sqlite.Open(dsn)
	default:
		return errors.New("unsupported_database")
	}
	db, err := gorm.Open(dialect, &gorm.Config{Logger: logger.Discard})
	if err != nil {
		return errors.New("database_unavailable")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return errors.New("database_unavailable")
	}
	defer sqlDB.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := accountprep.Run(ctx, db, in)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}

func main() {
	defer func() {
		if recover() != nil {
			fmt.Fprintln(os.Stderr, "preparation_failed")
			os.Exit(1)
		}
	}()
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
