package core

import (
	"context"
	"errors"
	"testing"
)

func TestApplicationIdentityCannotImpersonatePersonalResource(t *testing.T) {
	c := NewClient(&Config{AppID: "cli_test"})
	app := WithApplicationIdentity(context.Background(), func(context.Context) (string, error) { return "app", nil })
	user := WithUserIdentity(context.Background(), func(context.Context) (string, error) { return "user", nil })
	r := PersonalResource{Type: "docx", Token: "real"}
	if _, err := c.ResolvePersonalResource(app, r); !errors.Is(err, ErrAuth) {
		t.Fatalf("personal resolver accepted app: %v", err)
	}
	if _, _, err := c.DownloadPersonalResource(app, r); !errors.Is(err, ErrAuth) {
		t.Fatalf("personal download accepted app: %v", err)
	}
	if _, err := c.ValidateWorkbookCoverage(app, r, nil); !errors.Is(err, ErrAuth) {
		t.Fatalf("personal coverage accepted app: %v", err)
	}
	if _, err := c.ResolveApplicationResource(user, r); !errors.Is(err, ErrAuth) {
		t.Fatalf("application resolver accepted user: %v", err)
	}
	if _, _, err := c.DownloadApplicationResource(user, r); !errors.Is(err, ErrAuth) {
		t.Fatalf("application download accepted user: %v", err)
	}
	mixed := WithUserIdentity(app, func(context.Context) (string, error) { return "user", nil })
	if _, err := c.ResolveApplicationResource(mixed, r); !errors.Is(err, ErrAuth) {
		t.Fatalf("mixed identity accepted: %v", err)
	}
	if _, err := c.ResolveApplicationResource(app, PersonalResource{Type: "sheet", Token: "real"}); !errors.Is(err, ErrUnsupportedPersonalResource) {
		t.Fatalf("unvalidated sheet accepted: %v", err)
	}
}
