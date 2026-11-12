package types

import (
	"context"

	"gorm.io/gorm"
)

type wikiVerifiedReadKey struct{}
type wikiVerifiedRead struct {
	KB             string
	Guard          WikiVerifiedReadGuard
	PageIDs        []string
	PageRestricted bool
}

func WithWikiVerifiedPageReadGuard(ctx context.Context, kb string, pages []string, guard WikiVerifiedReadGuard) context.Context {
	return context.WithValue(ctx, wikiVerifiedReadKey{}, wikiVerifiedRead{KB: kb, Guard: guard, PageIDs: append([]string(nil), pages...), PageRestricted: true})
}

func CheckWikiVerifiedPageRead(db *gorm.DB, page *WikiPage) error {
	scope, ok := db.Statement.Context.Value(wikiVerifiedReadKey{}).(wikiVerifiedRead)
	if !ok || !scope.PageRestricted {
		return nil
	}
	if scope.Guard == nil {
		return ErrLegacySourceRestricted
	}
	return scope.Guard(db, "", page.KnowledgeBaseID, page)
}

// WikiVerifiedReadGuard is issued only after the worker has captured and
// isolated a complete input closure. It is never serialized or accepted from
// request headers. Private KB checks still run before it is consumed.
type WikiVerifiedReadGuard func(*gorm.DB, string, string, *WikiPage) error

func WithWikiVerifiedReadGuard(ctx context.Context, kb string, guard WikiVerifiedReadGuard) context.Context {
	return context.WithValue(ctx, wikiVerifiedReadKey{}, wikiVerifiedRead{KB: kb, Guard: guard})
}

func CheckWikiVerifiedRead(db *gorm.DB, knowledge, kb string, page *WikiPage) (bool, error) {
	scope, ok := db.Statement.Context.Value(wikiVerifiedReadKey{}).(wikiVerifiedRead)
	if !ok || scope.Guard == nil {
		return false, nil
	}
	return true, scope.Guard(db, knowledge, kb, page)
}
