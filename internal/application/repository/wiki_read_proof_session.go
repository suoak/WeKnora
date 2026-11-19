package repository

import (
	"context"
	"database/sql"
	"sync"

	"gorm.io/gorm"
)

type wikiReadProofSessionKey struct{}

const wikiReadProofSessionMaxKBs = 8

type wikiReadProofKey struct {
	pool *sql.DB
	kb   string
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
	session, ok := db.Statement.Context.Value(wikiReadProofSessionKey{}).(*wikiReadProofSession)
	if !ok || session == nil {
		return wikiDependencies(db, kb)
	}
	pool, err := db.DB()
	if err != nil || pool == nil {
		return wikiDependencies(db, kb)
	}
	key := wikiReadProofKey{pool, kb}
	session.mu.Lock()
	prior := session.proofs[key]
	session.mu.Unlock()
	var proof *wikiGenerationProof
	if prior == nil {
		proof, err = wikiDependencies(db, kb)
	} else {
		proof, err = wikiDependencies(db, kb, prior)
	}
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
