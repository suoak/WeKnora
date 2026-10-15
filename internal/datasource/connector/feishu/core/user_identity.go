package core

import "context"

type userIdentityKey struct{}
type userIdentity struct {
	provider func(context.Context) (string, error)
}

// WithUserIdentity binds every authenticated request (including exports and
// media downloads) to this provider. Errors NEVER fall back to tenant identity.
// The provider must resolve a persisted, owner-bound grant on every request so
// disconnecting an account also stops an in-flight export's next API call.
func WithUserIdentity(ctx context.Context, provider func(context.Context) (string, error)) context.Context {
	if provider == nil {
		provider = func(context.Context) (string, error) { return "", ErrAuth }
	}
	return context.WithValue(ctx, userIdentityKey{}, userIdentity{provider})
}
