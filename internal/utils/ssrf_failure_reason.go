package utils

import (
	"errors"
	"strings"
)

// SSRFRejectionReason is a finite diagnostic vocabulary. It never returns a
// URL, IP, credential or error text and never changes an admission decision.
func SSRFRejectionReason(err error) string {
	switch {
	case errors.Is(err, ErrSSRFHostNotWhitelisted):
		return "hostname_not_whitelisted"
	case errors.Is(err, ErrSSRFRedirectBlocked):
		return "redirect_policy"
	case !errors.Is(err, ErrSSRFRequestRejected):
		return ""
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "dns resolution failed"):
		return "dns_verification_unavailable"
	case strings.Contains(message, "resolves to restricted ip"):
		return "restricted_dns_address"
	case strings.Contains(message, "restricted") && strings.Contains(message, "port"):
		return "restricted_port"
	case strings.Contains(message, "direct ip address") || strings.Contains(message, "ip-like"):
		return "literal_address"
	case strings.Contains(message, "hostname") && strings.Contains(message, "restricted"):
		return "restricted_hostname"
	default:
		return "target_policy_unverified"
	}
}
