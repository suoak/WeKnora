package repository

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// withWikiDependencies uses a short row lock only around compare + commit.
// LLM calls and remote requests occur before this transaction. PostgreSQL
// serializes even writers that UPDATE the row without using this helper.
func withWikiDependencies(db *gorm.DB, page *types.WikiPage, write func(*gorm.DB) error) error {
	if page.ReadETag == "" {
		return ErrWikiPageConflict
	}
	originalETag, originalVersion := page.ReadETag, page.Version
	err := db.Transaction(func(tx *gorm.DB) error {
		if tx.Name() == "sqlite" {
			// Acquire the SQLite writer reservation before reading dependencies.
			// A zero-row UPDATE reserves the writer without firing page mutation
			// triggers: acquiring a lock must not invalidate provenance or epochs.
			if err := tx.Exec("UPDATE wiki_pages SET version = version WHERE id = ? AND 1 = 0", page.ID).Error; err != nil {
				return err
			}
		}
		var current types.WikiPage
		if err := tx.Clauses(
			clause.Locking{
				Strength: "UPDATE",
			},
		).
			Where(
				"id = ?",
				page.ID,
			).
			First(
				&current,
			).
			Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrWikiPageNotFound
			}
			return err
		}
		if current.ReadETag != page.ReadETag {
			_, proved := WikiMCPGenerationTenant(db.Statement.Context)
			if !proved || page.ReadBodyETag == "" || page.ReadBodyETag != current.BodyDependencyETag() {
				return ErrWikiPageConflict
			}
			// Merge only incoming links and retain every content dependency.
			page.InLinks = append(types.StringArray(nil), current.InLinks...)
		}
		if err := write(tx); err != nil {
			return err
		}
		// Reload canonical database values (not a digest of a partially-selected
		// projection). The next mutation of this object is a new guarded write.
		var saved types.WikiPage
		if err := tx.Where("id = ?", page.ID).First(&saved).Error; err != nil {
			return err
		}
		page.ReadETag = saved.ReadETag
		page.ReadBodyETag = saved.ReadBodyETag
		return nil
	})
	if err != nil {
		page.ReadETag, page.Version = originalETag, originalVersion
	}
	return err
}

// Kept separate so tests can exercise real lock ordering without model mocks.
func (r *wikiPageRepository) guardedWikiWrite(
	ctx context.Context,
	page *types.WikiPage,
	write func(*gorm.DB) error,
) error {
	return withWikiMCPProofWrite(ctx, func() error {
		return withWikiDependencies(r.db.WithContext(ctx), page, func(tx *gorm.DB) error {
			if err := beforeWikiMCPMutation(tx, ctx); err != nil {
				return err
			}
			if err := write(tx); err != nil {
				return err
			}
			var saved types.WikiPage
			if err := tx.Where("id=?", page.ID).First(&saved).Error; err != nil {
				return err
			}
			return sealWikiMCPMutation(tx, ctx, &saved)
		})
	})
}
