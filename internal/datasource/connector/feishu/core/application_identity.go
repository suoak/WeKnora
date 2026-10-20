package core

import "context"

type applicationTablesKey struct{}

// WithApplicationTables enables the candidate table adapter for this request.
// It does not supply an identity and never overrides identity validation.
func WithApplicationTables(ctx context.Context) context.Context {
	return context.WithValue(ctx, applicationTablesKey{}, true)
}

func applicationResourceAllowed(ctx context.Context, kind string) bool {
	if kind == "docx" {
		return true
	}
	enabled, _ := ctx.Value(applicationTablesKey{}).(bool)
	return enabled && (kind == "sheet" || kind == "bitable")
}

type applicationIdentityKey struct{}
type applicationIdentity struct {
	provider func(context.Context) (string, error)
	validate func(context.Context) error
}

// WithApplicationIdentity is explicit, request-scoped, and never a fallback
// for a failed user identity. The provider revalidates the persisted principal.
func WithApplicationIdentity(ctx context.Context, provider func(context.Context) (string, error)) context.Context {
	if provider == nil {
		provider = func(context.Context) (string, error) { return "", ErrAuth }
	}
	return context.WithValue(ctx, applicationIdentityKey{}, applicationIdentity{provider: provider})
}

// WithApplicationIdentityValidation adds a local, non-Feishu authorization
// check after waiting for an HTTP slot. The full provider may perform source
// ACL requests; it must run without holding that shared slot.
func WithApplicationIdentityValidation(ctx context.Context, provider func(context.Context) (string, error), validate func(context.Context) error) context.Context {
	ctx = WithApplicationIdentity(ctx, provider)
	entry := ctx.Value(applicationIdentityKey{}).(applicationIdentity)
	if validate == nil {
		validate = func(context.Context) error { return ErrAuth }
	}
	entry.validate = validate
	return context.WithValue(ctx, applicationIdentityKey{}, entry)
}

func requireApplicationIdentity(ctx context.Context) error {
	_, app := ctx.Value(applicationIdentityKey{}).(applicationIdentity)
	_, user := ctx.Value(userIdentityKey{}).(userIdentity)
	if !app || user {
		return ErrAuth
	}
	return nil
}

// Only the proven Docx path is opened. Wiki is resolved before checking the
// underlying type. Other types remain independently gated, not empty success.
func (c *Client) ResolveApplicationResource(ctx context.Context, r PersonalResource) (PersonalResource, error) {
	if err := requireApplicationIdentity(ctx); err != nil {
		return r, err
	}
	if r.Type != "wiki" && !applicationResourceAllowed(ctx, r.Type) {
		return r, ErrUnsupportedPersonalResource
	}
	resolved, err := c.resolveResource(ctx, r)
	if err == nil && !applicationResourceAllowed(ctx, resolved.Type) {
		err = ErrUnsupportedPersonalResource
	}
	return resolved, err
}

func (c *Client) DownloadApplicationResource(ctx context.Context, r PersonalResource) ([]byte, string, error) {
	if err := requireApplicationIdentity(ctx); err != nil {
		return nil, "", err
	}
	if !applicationResourceAllowed(ctx, r.Type) {
		return nil, "", ErrUnsupportedPersonalResource
	}
	if r.Type == "sheet" || r.Type == "bitable" {
		return c.downloadApplicationTable(ctx, r)
	}
	return c.downloadResource(ctx, r)
}
