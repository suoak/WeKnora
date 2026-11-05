package service

import (
	"context"
	"time"
)

type wikiOwnedClaimReleaser interface {
	ReleaseWikiClaimsIfOwned(context.Context, uint64, string, map[int64]time.Time) (int64, error)
}

func releaseAbortedWikiClaims(
	ctx context.Context, repo wikiOwnedClaimReleaser, payload WikiIngestPayload, claims map[int64]time.Time,
) (int64, error) {
	// Losing a generation lease must cancel model calls and page publication.
	// It must not cancel metadata-only CAS cleanup of the original SQL leases.
	// Exact timestamps prevent late cleanup from releasing a replacement owner.
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), wikiIngestCleanupTimeout)
	defer cancel()
	return repo.ReleaseWikiClaimsIfOwned(cleanup, payload.TenantID, payload.KnowledgeBaseID, claims)
}
