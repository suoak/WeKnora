package repository

import (
	"context"
	"slices"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UpdateBacklink changes only the derived incoming-link set under a row lock.
// It never writes a caller's stale body, status, sources or other metadata.
func (r *wikiPageRepository) UpdateBacklink(ctx context.Context, kb, target, source string, add bool) error {
	return withWikiMCPProofWrite(ctx, func() error {
		return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := beforeWikiMCPMutation(tx, ctx); err != nil {
				return err
			}
			var page types.WikiPage
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("knowledge_base_id=? AND slug=?", kb, target).First(&page).Error; err != nil {
				return err
			}
			contains := slices.Contains(page.InLinks, source)
			if contains == add {
				return nil
			}
			if add {
				page.InLinks = append(page.InLinks, source)
			} else {
				page.InLinks = slices.DeleteFunc(page.InLinks, func(v string) bool { return v == source })
			}
			if err := tx.Model(&page).Updates(map[string]interface{}{
				"in_links": page.InLinks, "updated_at": time.Now(),
			}).Error; err != nil {
				return err
			}
			return sealWikiMCPMutation(tx, ctx, &page)
		})
	})
}
