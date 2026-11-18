package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// The server's platform-serving policy keeps normal KB/key/private-owner
// authorization. Explicit document capabilities retain their narrower scope.
func ordinaryLocalSourceRead(ctx context.Context) bool {
	_, _, _, exact := types.ExactDocumentIdentity(ctx)
	return types.LocalSourceContentRead(ctx) && !exact
}
