package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
)

type wikiBacklinkStore interface {
	UpdateBacklink(context.Context, string, string, string, bool) error
}

func wikiWriteSnapshotMatches(ctx context.Context, proposed, current *types.WikiPage) bool {
	if proposed.ReadETag == "" {
		return false
	}
	if proposed.ReadETag == current.DependencyETag() {
		return true
	}
	// Only a server-held generation proof enables this merge. Current's
	// incoming links are preserved by UpdatePage/UpdateAutoLinkedContent.
	_, proved := repository.WikiMCPGenerationTenant(ctx)
	return proved && proposed.ReadBodyETag != "" && proposed.ReadBodyETag == current.BodyDependencyETag()
}
