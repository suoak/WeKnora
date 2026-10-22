package access

import (
	"context"
	"reflect"
	"sync"

	"github.com/Tencent/WeKnora/internal/types"
)

type documentReadOutputKey struct{}
type documentReadOutput struct {
	mu         sync.Mutex
	principal  types.Principal
	caller     types.Caller
	credential *types.TenantAPIKeyScope
	checks     []func() error
}

func normalizedReadCredential(ctx context.Context) *types.TenantAPIKeyScope {
	if key, ok := types.TenantAPIKeyScopeFromContext(ctx); ok {
		copy := key.Normalize()
		copy.Capabilities = append(types.StringArray(nil), copy.Capabilities...)
		copy.KnowledgeBaseIDs = append(types.StringArray(nil), copy.KnowledgeBaseIDs...)
		return &copy
	}
	return nil
}

// WithDocumentReadOutput belongs in a buffered, server-certified HTTP boundary.
// No exported context key or request header can substitute for live proofs.
func WithDocumentReadOutput(ctx context.Context) (context.Context, func() error, error) {
	principal, ok := ctx.Value(types.PrincipalContextKey).(types.Principal)
	caller := types.CallerFromContext(ctx)
	if !ok || principal.Type != types.PrincipalWebUser || principal.ID == "" || caller.UserID != principal.ID {
		return nil, nil, ErrUnauthorized
	}
	out := &documentReadOutput{principal: principal, caller: caller, credential: normalizedReadCredential(ctx)}
	guarded := context.WithValue(ctx, documentReadOutputKey{}, out)
	return guarded, func() error {
		if err := out.matches(guarded); err != nil {
			return err
		}
		out.mu.Lock()
		checks := append([]func() error(nil), out.checks...)
		out.mu.Unlock()
		if len(checks) == 0 {
			return ErrForbidden
		}
		for _, check := range checks {
			if err := check(); err != nil {
				return err
			}
		}
		return nil
	}, nil
}
func HasDocumentReadOutput(ctx context.Context) bool {
	out, ok := ctx.Value(documentReadOutputKey{}).(*documentReadOutput)
	return ok && out != nil
}
func (out *documentReadOutput) matches(ctx context.Context) error {
	p, ok := ctx.Value(types.PrincipalContextKey).(types.Principal)
	if !ok || !reflect.DeepEqual(p, out.principal) || !reflect.DeepEqual(types.CallerFromContext(ctx), out.caller) || !reflect.DeepEqual(normalizedReadCredential(ctx), out.credential) {
		return ErrUnauthorized
	}
	return nil
}
func retainDocumentReadOutput(ctx context.Context, check func() error) error {
	out, ok := ctx.Value(documentReadOutputKey{}).(*documentReadOutput)
	if !ok {
		return nil
	} // Direct service callers still perform their final check.
	if out == nil {
		return ErrForbidden
	}
	if err := out.matches(ctx); err != nil {
		return err
	}
	out.mu.Lock()
	out.checks = append(out.checks, check)
	out.mu.Unlock()
	return nil
}
func (g *DocumentReadGrant) RetainForOutput(ctx context.Context) error {
	if g == nil || g.operation != types.DocumentRead {
		return ErrForbidden
	}
	if err := g.Revalidate(ctx); err != nil {
		return err
	}
	return retainDocumentReadOutput(ctx, func() error { return g.Revalidate(ctx) })
}
func (s *DocumentReadScope) RetainForOutput(ctx context.Context) error {
	if err := s.Validate(ctx); err != nil {
		return err
	}
	return retainDocumentReadOutput(ctx, func() error { return s.Validate(ctx) })
}
