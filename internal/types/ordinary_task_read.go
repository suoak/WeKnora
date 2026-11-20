package types

import (
	"context"

	"gorm.io/gorm"
)

type ordinaryTaskReadKey struct{}

type ordinaryTaskRead struct {
	tenant uint64
	id, kb string
	check  func(*gorm.DB) error
}

// WithOrdinaryTaskDocumentRead is issued only by a trusted worker after checking
// a server-loaded ordinary upload. It grants no whole-KB or sibling-document
// read, creates no principal, and rechecks the live binding on every use.
func WithOrdinaryTaskDocumentRead(
	ctx context.Context, tenant uint64, id, kb string, check func(*gorm.DB) error,
) (context.Context, error) {
	if tenant == 0 || id == "" || kb == "" || check == nil {
		return ctx, ErrLegacySourceRestricted
	}
	return context.WithValue(ctx, ordinaryTaskReadKey{}, ordinaryTaskRead{tenant, id, kb, check}), nil
}

func checkOrdinaryTaskDocumentRead(db *gorm.DB, id, kb string) (bool, error) {
	scope, ok := db.Statement.Context.Value(ordinaryTaskReadKey{}).(ordinaryTaskRead)
	if !ok || id == "" || id != scope.id || kb != scope.kb {
		return false, nil
	}
	if scope.tenant == 0 || scope.check == nil {
		return true, ErrLegacySourceRestricted
	}
	if err := scope.check(db); err != nil {
		return true, err
	}
	recordLegacyDocumentRead(db.Statement.Context, id, kb)
	return true, nil
}

// CheckOrdinaryTaskKBConfiguration permits only a repository configuration
// projection. It is deliberately separate from CheckLegacySourceRead: the task
// must never acquire a KB body/profile or Wiki read through this scope.
func CheckOrdinaryTaskKBConfiguration(db *gorm.DB, kb string) (bool, error) {
	scope, ok := db.Statement.Context.Value(ordinaryTaskReadKey{}).(ordinaryTaskRead)
	if !ok || kb == "" || kb != scope.kb {
		return false, nil
	}
	if scope.tenant == 0 || scope.check == nil {
		return true, ErrLegacySourceRestricted
	}
	return true, scope.check(db)
}
