package repository

import (
	"context"
	"errors"
	"sort"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// WikiNoWikiSourcePlan is a bounded, source-version-bound decision. It contains
// no source text and cannot create or certify a generated summary.
type WikiNoWikiSourcePlan struct {
	TenantID        uint64         `json:"tenant_id"`
	KnowledgeBaseID string         `json:"knowledge_base_id"`
	Documents       []string       `json:"documents"`
	TextRunes       map[string]int `json:"text_runes"`
	Minimum         int            `json:"minimum_text_runes"`
	Reason          string         `json:"reason"`
	Snapshot        string         `json:"snapshot"`
	Applied         bool           `json:"applied"`
}

// PrepareWikiNoWikiSources lets the locked operator recovery certify only
// currently verified insufficient inputs, without rerunning documents already
// restored by normal workers. Preview is read-only; apply requires its snapshot.
func PrepareWikiNoWikiSources(
	ctx context.Context, db *gorm.DB, tenant uint64, kb, expected string, apply bool,
) (*WikiNoWikiSourcePlan, error) {
	if tenant == 0 || kb == "" || apply && expected == "" {
		return nil, errors.New("exact no-Wiki recovery snapshot required")
	}
	var plan *WikiNoWikiSourcePlan
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var anchor types.KnowledgeBase
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id=? AND tenant_id=?", kb, tenant).First(&anchor).Error; err != nil {
			return err
		}
		active, err := WikiPreservingRecoveryActive(ctx, NewWikiPageRepository(tx), kb)
		if err != nil {
			return err
		}
		if !active {
			return errors.New("active preserving recovery required for no-Wiki certification")
		}
		proof, err := wikiPreservingDependencies(tx, kb)
		if err != nil {
			return err
		}
		if proof.Tenant != tenant || len(proof.NoWikiSources) == 0 {
			return ErrWikiMCPUnverified
		}
		plan = &WikiNoWikiSourcePlan{
			TenantID: tenant, KnowledgeBaseID: kb, TextRunes: proof.NoWikiSources,
			Minimum: types.WikiMinimumTextRunes, Reason: wikiNoWikiReason,
		}
		for id := range proof.NoWikiSources {
			plan.Documents = append(plan.Documents, id)
		}
		sort.Strings(plan.Documents)
		plan.Snapshot = wikiDigest([]any{anchor, proof.Sources, proof.Epochs, proof.NoWikiSources})
		if !apply {
			return nil
		}
		if expected != plan.Snapshot {
			return errors.New("no-Wiki source snapshot changed")
		}
		if err := recordWikiNoWikiSources(tx, proof); err != nil {
			return err
		}
		fresh, err := wikiPreservingDependencies(tx, kb, proof)
		if err != nil {
			return err
		}
		if fresh.DependencySHA256 != proof.DependencySHA256 || wikiDigest(fresh.Epochs) != wikiDigest(proof.Epochs) {
			return ErrWikiMCPInputsChanged
		}
		plan.Applied = true
		return nil
	})
	return plan, err
}
