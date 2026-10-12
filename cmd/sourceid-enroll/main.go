// sourceid-enroll imports an independently reviewed employee-to-local-user CSV.
// Dry-run is the default. Connection credentials are read only from an env var.
package main

import (
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

type enrollmentRow struct{ EmployeeNo, UserID string }

func readRows(reader io.Reader) ([]enrollmentRow, error) {
	r := csv.NewReader(io.LimitReader(reader, 1<<20))
	header, err := r.Read()
	if err != nil {
		return nil, errors.New("invalid CSV")
	}
	if len(header) != 2 || header[0] != "employee_no" || header[1] != "user_id" {
		return nil, errors.New("CSV header must be employee_no,user_id")
	}
	rows := []enrollmentRow{}
	employees, users := map[string]bool{}, map[string]bool{}
	for {
		fields, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(fields) != 2 {
			return nil, errors.New("invalid CSV row")
		}
		employee, user := strings.TrimSpace(fields[0]), strings.TrimSpace(fields[1])
		if employee == "" || len(employee) > 512 || len(user) > 36 {
			return nil, errors.New("invalid identity")
		}
		if _, err := uuid.Parse(user); err != nil {
			return nil, errors.New("invalid user ID")
		}
		if employees[employee] || users[user] {
			return nil, errors.New("duplicate employee or local user")
		}
		employees[employee] = true
		users[user] = true
		rows = append(rows, enrollmentRow{employee, user})
	}
	if len(rows) == 0 || len(rows) > 10000 {
		return nil, errors.New("invalid CSV row count")
	}
	return rows, nil
}

var errDryRun = errors.New("dry run complete")

func enroll(db *gorm.DB, rows []enrollmentRow, issuer, reviewer, reference string, apply bool) error {
	if issuer == "" || reviewer == "" || reference == "" {
		return errors.New("issuer, reviewer and review reference required")
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		for _, row := range rows {
			var user types.User
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND is_active = ?", row.UserID, true).First(&user).Error; err != nil {
				return errors.New("local account missing or disabled")
			}
			var identities []types.ExternalIdentity
			if err := tx.Where("provider = ? AND issuer = ? AND (employee_no = ? OR user_id = ?)", "sourceid", issuer, row.EmployeeNo, row.UserID).Find(&identities).Error; err != nil {
				return errors.New("identity lookup failed")
			}
			for _, identity := range identities {
				if identity.EmployeeNo != row.EmployeeNo || identity.UserID != row.UserID {
					return errors.New("existing external identity conflict")
				}
			}
			var existing []types.SourceIDEnrollment
			if err := tx.Where("issuer = ? AND (employee_no = ? OR user_id = ?)", issuer, row.EmployeeNo, row.UserID).Find(&existing).Error; err != nil {
				return errors.New("enrollment lookup failed")
			}
			if len(existing) > 0 {
				if len(existing) != 1 || existing[0].EmployeeNo != row.EmployeeNo || existing[0].UserID != row.UserID {
					return errors.New("existing enrollment conflict")
				}
				continue
			}
			if apply {
				if err := tx.Create(&types.SourceIDEnrollment{ID: uuid.NewString(), Issuer: issuer, EmployeeNo: row.EmployeeNo, UserID: row.UserID, ReviewedBy: reviewer, ReviewReference: reference, CreatedAt: time.Now()}).Error; err != nil {
					return errors.New("enrollment write conflict; batch rolled back")
				}
			}
		}
		if !apply {
			return errDryRun
		}
		return nil
	})
	if errors.Is(err, errDryRun) {
		return nil
	}
	return err
}

func run() error {
	input := flag.String("input", "", "reviewed CSV path")
	issuer := flag.String("issuer", "", "confirmed OIDC issuer")
	reviewer := flag.String("reviewer", "", "reviewer identity")
	reference := flag.String("reference", "", "approval ticket/reference")
	driver := flag.String("driver", "postgres", "postgres or sqlite")
	apply := flag.Bool("apply", false, "write reviewed mappings (default: dry-run)")
	flag.Parse()
	f, err := os.Open(*input)
	if err != nil {
		return errors.New("cannot open input CSV")
	}
	defer f.Close()
	rows, err := readRows(f)
	if err != nil {
		return err
	}
	dsn := os.Getenv("SOURCEID_ENROLL_DSN")
	if dsn == "" {
		return errors.New("SOURCEID_ENROLL_DSN required")
	}
	var dialector gorm.Dialector
	switch *driver {
	case "postgres":
		dialector = postgres.Open(dsn)
	case "sqlite":
		dialector = sqlite.Open(dsn)
	default:
		return errors.New("unsupported database driver")
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Discard})
	if err != nil {
		return errors.New("database connection failed")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return errors.New("database unavailable")
	}
	defer sqlDB.Close()
	if err := enroll(db, rows, *issuer, *reviewer, *reference, *apply); err != nil {
		return err
	}
	fmt.Printf("validated_rows=%d applied=%t\n", len(rows), *apply)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
