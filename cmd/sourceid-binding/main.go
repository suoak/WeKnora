// Operator-only review tool. Database access is the administrative boundary;
// never expose this CLI through public HTTP routes. No credentials in argv.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type reviewStore interface {
	ListSourceIDBindingReviews(context.Context) ([]types.SourceIDBindingReview, error)
	ReviewSourceIDBinding(context.Context, string, string, string, string) error
}

func run() error {
	action := flag.String("action", "list", "list, approve, revoke, reopen")
	id := flag.String("id", "", "review ID; no employee or email values in arguments")
	reviewer := flag.String("reviewer", "", "authorized reviewer")
	reference := flag.String("reference", "", "account ownership approval reference")
	driver := flag.String("driver", "postgres", "postgres or sqlite")
	apply := flag.Bool("apply", false, "required for mutation")
	flag.Parse()
	if *action != "list" && *action != "approve" && *action != "revoke" && *action != "reopen" {
		return errors.New("invalid action")
	}
	if *action != "list" && (!*apply || *id == "" || *reviewer == "" || *reference == "") {
		return errors.New("mutation requires apply, id, reviewer and reference")
	}
	dsn := os.Getenv("SOURCEID_ENROLL_DSN")
	if dsn == "" {
		return errors.New("SOURCEID_ENROLL_DSN required")
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
		return errors.New("database unavailable")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return errors.New("database unavailable")
	}
	defer func(cleanup func() error) { _ = cleanup() }(sqlDB.Close)
	store, ok := apprepo.NewUserRepository(db).(reviewStore)
	if !ok {
		return errors.New("review store unavailable")
	}
	if *action == "list" {
		rows, err := store.ListSourceIDBindingReviews(context.Background())
		if err != nil {
			return errors.New("review lookup failed")
		}
		// No email, RJGH or subject in console output. IDs locate protected
		// records; administrators verify identity using their approved channel.
		output := []map[string]string{}
		for _, r := range rows {
			output = append(output, map[string]string{"id": r.ID, "user_id": r.UserID, "status": r.Status})
		}
		return json.NewEncoder(os.Stdout).Encode(output)
	}
	if err := store.ReviewSourceIDBinding(context.Background(), *id, *reviewer, *reference, *action); err != nil {
		return errors.New("review rejected; no partial change")
	}
	fmt.Println("review_action_applied=true")
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
