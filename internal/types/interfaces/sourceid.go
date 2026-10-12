package interfaces

import (
	"context"
	"github.com/Tencent/WeKnora/internal/types"
)

// Optional extension implemented by the SQL user repository; generic OIDC and
// local authentication do not require SourceID capabilities.
type SourceIDRepository interface {
	ResolveSourceIDIdentity(context.Context, string, string, string) (*types.User, error)
	CreateSourceIDSession(context.Context, *types.SourceIDSession) error
	CheckSourceIDSession(context.Context, string, string) error
	PersistSourceIDTokens(context.Context, string, *types.AuthToken, *types.AuthToken, string) error
	RevokeSourceIDSession(context.Context, string, string) error
	RevokeSourceIDIndex(context.Context, string, string) error
	SaveOIDCTransaction(context.Context, *types.OIDCTransaction) error
	ConsumeOIDCTransaction(context.Context, string) (*types.OIDCTransaction, error)
}
