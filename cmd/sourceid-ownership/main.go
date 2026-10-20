// Operator-only streaming ownership synchronizer. Never expose as public API.
package main

import (
	"bufio"
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

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type syncStore interface {
	SyncSourceIDAccountOwnership(
		context.Context,
		types.SourceIDAccountOwnership,
		string,
		bool,
	) (bool, error)
}
type accountResolver interface {
	ResolveSourceIDOwnershipAccount(context.Context, string, string, string) (string, error)
}
type protectedRow struct {
	Issuer       string    `json:"issuer"`
	EmployeeNo   string    `json:"employee_no"`
	UserID       string    `json:"user_id"`
	Email        string    `json:"email"`
	Status       string    `json:"status"`
	Revision     int64     `json:"revision"`
	PolicyRef    string    `json:"policy_ref"`
	OwnershipRef string    `json:"ownership_ref"`
	SourceRef    string    `json:"source_ref"`
	VerifiedAt   time.Time `json:"verified_at"`
	ValidUntil   time.Time `json:"valid_until"`
}
type summary struct {
	Rows      int  `json:"rows"`
	Changed   int  `json:"changed"`
	Unchanged int  `json:"unchanged"`
	Rejected  int  `json:"rejected"`
	Applied   bool `json:"applied"`
}

// 64KiB is a single-record input safety bound, never a directory size or
// business population limit. Rows stream independently; partial failures are
// counted and return nonzero; successful rows can be safely replayed.
func process(
	ctx context.Context,
	in io.Reader,
	out io.Writer,
	store syncStore,
	actor string,
	apply bool,
) error {
	s := bufio.NewScanner(in)
	s.Buffer(make([]byte, 4096), 65536)
	result := summary{Applied: apply}
	// One latest event per identity/account per batch. Identical duplicates are
	// idempotent; conflicting duplicates fail equally in dry-run and apply.
	seen := map[string]string{}
	for s.Scan() {
		if ctx.Err() != nil {
			return errors.New("ownership sync cancelled")
		}
		result.Rows++
		var row protectedRow
		d := json.NewDecoder(bytes.NewReader(s.Bytes()))
		d.DisallowUnknownFields()
		if d.Decode(&row) != nil || d.Decode(new(interface{})) != io.EOF {
			result.Rejected++
			continue
		}
		if row.UserID == "" {
			resolver, ok := store.(accountResolver)
			if !ok {
				result.Rejected++
				continue
			}
			id, err := resolver.ResolveSourceIDOwnershipAccount(ctx, row.Issuer, row.EmployeeNo, row.Email)
			if err != nil {
				result.Rejected++
				continue
			}
			row.UserID = id
		}
		canonical, _ := json.Marshal(row)
		keys := []string{
			"employee:" + row.Issuer + "\x00" + row.EmployeeNo,
			"user:" + row.Issuer + "\x00" + row.UserID,
			"email:" + row.Issuer + "\x00" + strings.ToLower(strings.TrimSpace(row.Email)),
		}
		duplicate, conflict := false, false
		for _, key := range keys {
			if old, ok := seen[key]; ok {
				if old != string(canonical) {
					conflict = true
				} else {
					duplicate = true
				}
			}
		}
		if conflict {
			result.Rejected++
			continue
		}
		if duplicate {
			result.Unchanged++
			continue
		}
		changed, err := store.SyncSourceIDAccountOwnership(
			ctx,
			types.SourceIDAccountOwnership{
				Issuer:       row.Issuer,
				EmployeeNo:   row.EmployeeNo,
				UserID:       row.UserID,
				Email:        row.Email,
				Status:       row.Status,
				Revision:     row.Revision,
				PolicyRef:    row.PolicyRef,
				OwnershipRef: row.OwnershipRef,
				SourceRef:    row.SourceRef,
				VerifiedAt:   row.VerifiedAt,
				ValidUntil:   row.ValidUntil,
			},
			actor,
			apply,
		)
		if err != nil {
			result.Rejected++
		} else if changed {
			result.Changed++
		} else {
			result.Unchanged++
		}
		if err == nil {
			for _, key := range keys {
				seen[key] = string(canonical)
			}
		}
	}
	if s.Err() != nil {
		result.Rejected++
	}
	if json.NewEncoder(out).Encode(result) != nil {
		return errors.New("ownership summary unavailable")
	}
	if result.Rejected > 0 {
		return errors.New("ownership rows rejected")
	}
	return nil
}

func run() error {
	apply := flag.Bool("apply", false, "apply approved incremental rows; default dry-run")
	actor := flag.String("actor", "", "authorized operator reference; no employee data")
	driver := flag.String("driver", "postgres", "postgres or sqlite")
	flag.Parse()
	dsn := os.Getenv("SOURCEID_OWNERSHIP_DSN")
	if dsn == "" || *actor == "" {
		return errors.New("protected database and operator required")
	}
	var dialect gorm.Dialector
	switch *driver {
	case "postgres":
		dialect = postgres.Open(dsn)
	case "sqlite":
		dialect = sqlite.Open(dsn)
	default:
		return errors.New("unsupported driver")
	}
	db, err := gorm.Open(dialect, &gorm.Config{Logger: logger.Discard})
	if err != nil {
		return errors.New("ownership database unavailable")
	}
	raw, err := db.DB()
	if err != nil {
		return errors.New("ownership database unavailable")
	}
	defer func(cleanup func() error) { _ = cleanup() }(raw.Close)
	store, ok := apprepo.NewUserRepository(db).(syncStore)
	if !ok {
		return errors.New("ownership store unavailable")
	}
	return process(context.Background(), os.Stdin, os.Stdout, store, *actor, *apply)
}

func main() {
	if run() != nil {
		fmt.Fprintln(os.Stderr, "ownership_sync_failed=true")
		os.Exit(1)
	}
}
