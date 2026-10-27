package service

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"
)

// A separately approved production intranet deployment may opt in to ONE HTTP
// callback. This is not the isolated-test exception and grants no source access.
// The operator records approval and pins the private IPv4 address in deployment
// configuration. DNS is checked at configuration and authorization boundaries.
func feishuInternalHTTPCallbackAllowed(cfg *FeishuAppConfig) bool {
	if cfg == nil || cfg.Capabilities == nil || !cfg.Capabilities.PersonalEnabled ||
		os.Getenv("KNOWHUB_ENVIRONMENT") != "production" || os.Getenv("FEISHU_INTERNAL_HTTP_CALLBACK_ENABLED") != "1" ||
		os.Getenv("FEISHU_INTERNAL_HTTP_CALLBACK_APP_ID") != cfg.AppID ||
		os.Getenv("FEISHU_INTERNAL_HTTP_CALLBACK_URL") != cfg.RedirectURI {
		return false
	}
	approval := os.Getenv("FEISHU_INTERNAL_HTTP_CALLBACK_APPROVAL_REF")
	if len(approval) < 8 || len(approval) > 160 || strings.ContainsAny(approval, " \t\r\n") {
		return false
	}
	pinned, err := netip.ParseAddr(os.Getenv("FEISHU_INTERNAL_HTTP_CALLBACK_IPV4"))
	if err != nil || !pinned.Is4() || !pinned.IsPrivate() {
		return false
	}
	u, err := url.Parse(cfg.RedirectURI)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
		u.RawPath != "" ||
		u.Path != "/feishu/callback" ||
		u.Port() != "" {
		return false
	}
	host := u.Hostname()
	if host == "" || u.Host != host || host != strings.ToLower(host) || strings.HasSuffix(host, ".") ||
		!strings.Contains(host, ".") ||
		strings.ContainsAny(host, "%/\\@ ") {
		return false
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return false
	}
	return true
}

func verifyFeishuInternalHTTPCallback(ctx context.Context, cfg *FeishuAppConfig) error {
	return verifyFeishuInternalHTTPCallbackWithLookup(ctx, cfg, net.DefaultResolver.LookupNetIP)
}

func verifyFeishuInternalHTTPCallbackWithLookup(
	ctx context.Context,
	cfg *FeishuAppConfig,
	lookup func(context.Context, string, string) ([]netip.Addr, error),
) error {
	if !feishuInternalHTTPCallbackAllowed(cfg) {
		return nil
	}
	u, _ := url.Parse(cfg.RedirectURI)
	pinned, _ := netip.ParseAddr(os.Getenv("FEISHU_INTERNAL_HTTP_CALLBACK_IPV4"))
	work, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	addresses, err := lookup(work, "ip", u.Hostname())
	if err != nil || len(addresses) == 0 {
		return errors.New("feishu_internal_callback_dns_unconfirmed")
	}
	for _, address := range addresses {
		if address.Unmap() != pinned {
			return errors.New("feishu_internal_callback_address_mismatch")
		}
	}
	return nil
}
