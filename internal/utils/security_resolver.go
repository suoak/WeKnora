package utils

import (
	"context"
	"net"
)

// ValidateURLForSSRFWithResolver applies the ordinary URL and IP policy using
// the same server-owned resolver as the caller's pinned connection transport.
func ValidateURLForSSRFWithResolver(ctx context.Context, rawURL string, resolver *net.Resolver) error {
	if resolver == nil {
		return ValidateURLForSSRF(rawURL)
	}
	return validateURLForSSRFWithLookup(rawURL, ssrfContextIPLookup(ctx, resolver.LookupIPAddr))
}

func ssrfContextIPLookup(
	ctx context.Context, lookup func(context.Context, string) ([]net.IPAddr, error),
) func(string) ([]net.IP, error) {
	return func(host string) ([]net.IP, error) {
		addresses, err := lookup(ctx, host)
		if err != nil {
			return nil, err
		}
		ips := make([]net.IP, 0, len(addresses))
		for _, address := range addresses {
			ips = append(ips, address.IP)
		}
		return ips, nil
	}
}

func validateURLForSSRFForResolverOutbound(ctx context.Context, rawURL string, resolver *net.Resolver) error {
	if resolver == nil {
		return validateURLForSSRFForOutbound(rawURL)
	}
	// The origin-wide cache belongs to the process resolver. Sharing its success
	// across resolver identities could admit a different DNS policy's answers.
	return ValidateURLForSSRFWithResolver(ctx, rawURL, resolver)
}
