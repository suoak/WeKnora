package repository

import (
	"context"
	"database/sql"
	"sync"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

type wikiReadProofSessionKey struct{}

const wikiReadProofSessionMaxKBs = 8

type wikiReadProofKey struct {
	pool      *sql.DB
	kb        string
	selection string
	local     bool
}
type wikiReadProofSession struct {
	mu     sync.Mutex
	proofs map[wikiReadProofKey]*wikiGenerationProof
}

// WithWikiReadProofSession reuses verified input hashes within one read tool,
// never authorization facts. Every use still reloads live sources and epochs
// through the existing previous-proof verifier. Nothing survives the request.
func WithWikiReadProofSession(ctx context.Context) context.Context {
	return context.WithValue(
		ctx,
		wikiReadProofSessionKey{},
		&wikiReadProofSession{proofs: map[wikiReadProofKey]*wikiGenerationProof{}},
	)
}

func wikiReadDependencies(db *gorm.DB, kb string) (*wikiGenerationProof, error) {
	return wikiReadSelectedDependencies(db, kb, nil)
}

// Producer selections and local-read mode are separate proof identities. Only
// content digests are reused; the existing verifier reloads source authority,
// document input stamps and trigger coverage before and after every use.
func wikiReadSelectedDependencies(db *gorm.DB, kb string, selected map[string]bool) (*wikiGenerationProof, error) {
	verify := func(prior *wikiGenerationProof) (*wikiGenerationProof, error) {
		if selected != nil {
			if prior != nil {
				return wikiDependenciesSelected(db, kb, nil, selected, prior)
			}
			return wikiDependenciesSelected(db, kb, nil, selected)
		}
		if prior != nil {
			return wikiDependencies(db, kb, prior)
		}
		return wikiDependencies(db, kb)
	}
	session, ok := db.Statement.Context.Value(wikiReadProofSessionKey{}).(*wikiReadProofSession)
	if !ok || session == nil {
		return verify(nil)
	}
	pool, err := db.DB()
	if err != nil || pool == nil {
		return verify(nil)
	}
	selection := ""
	if selected != nil {
		selection = wikiDigest(wikiSelectionIDs(selected))
	}
	key := wikiReadProofKey{pool: pool, kb: kb, selection: selection, local: types.LocalSourceContentRead(db.Statement.Context)}
	session.mu.Lock()
	prior := session.proofs[key]
	session.mu.Unlock()
	proof, err := verify(prior)
	if err != nil {
		return nil, err
	}
	session.mu.Lock()
	if _, present := session.proofs[key]; present || len(session.proofs) < wikiReadProofSessionMaxKBs {
		session.proofs[key] = proof
	}
	session.mu.Unlock()
	return proof, nil
}
