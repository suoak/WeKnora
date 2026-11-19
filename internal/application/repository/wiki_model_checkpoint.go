package repository

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

// WikiModelCheckpointReady rechecks current inputs before a private result is reused.
// Publication still performs its own complete source, page and ownership checks.
func WikiModelCheckpointReady(ctx context.Context, repo interfaces.WikiPageRepository, kb, digest string) bool {
	r, ok := repo.(*wikiPageRepository)
	p, proved := ctx.Value(wikiGenerationProofKey{}).(*wikiGenerationProof)
	if !ok || !proved || p == nil || ctx.Err() != nil || p.KB != kb || p.DependencySHA256 != digest {
		return false
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var fresh *wikiGenerationProof
		var err error
		if len(p.SelectedSources) > 0 {
			fresh, err = wikiDependenciesSelected(tx, kb, nil, p.SelectedSources, p)
		} else {
			fresh, err = wikiDependencies(tx, kb, p)
		}
		if err != nil {
			return err
		}
		if fresh.Tenant != p.Tenant || fresh.DependencySHA256 != digest ||
			wikiDigest(fresh.Epochs) != wikiDigest(p.Epochs) {
			return ErrWikiMCPInputsChanged
		}
		return nil
	})
	return err == nil
}
