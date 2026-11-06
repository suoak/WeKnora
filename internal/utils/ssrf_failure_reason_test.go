package utils

import (
	"errors"
	"fmt"
	"testing"
)

func TestSSRFRejectionReasonIsTypedAndDoesNotExposeTargets(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{
			fmt.Errorf("%w: DNS resolution failed for secret.invalid", ErrSSRFRequestRejected),
			"dns_verification_unavailable",
		},
		{
			fmt.Errorf("%w: private.invalid resolves to restricted IP 10.0.0.1", ErrSSRFRequestRejected),
			"restricted_dns_address",
		},
		{ErrSSRFHostNotWhitelisted, "hostname_not_whitelisted"},
		{ErrSSRFRedirectBlocked, "redirect_policy"},
		{errors.New("provider says SSRF blocked secret.invalid"), ""},
	} {
		if got := SSRFRejectionReason(tc.err); got != tc.want {
			t.Fatalf("expected finite reason %q, got %q", tc.want, got)
		}
	}
}
