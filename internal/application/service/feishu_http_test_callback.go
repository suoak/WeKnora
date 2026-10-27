package service

import (
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"time"
)

func feishuNonLoopbackHTTP(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "http" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1"
}

// A deployment-only exception for one isolated test app and one exact callback.
// It never changes the default validator or permits domains, CIDRs, arbitrary
// paths or an indefinite allowance. Rechecked at each OAuth/token operation.
func feishuTestHTTPCallbackAllowed(cfg *FeishuAppConfig) bool {
	if cfg == nil || cfg.Capabilities == nil ||
		os.Getenv("FEISHU_TEST_HTTP_CALLBACK_ENABLED") != "1" ||
		os.Getenv("KNOWHUB_ENVIRONMENT") != "isolated-test" ||
		os.Getenv("FEISHU_TEST_HTTP_CALLBACK_APP_ID") != cfg.AppID ||
		os.Getenv("FEISHU_TEST_HTTP_CALLBACK_URL") != cfg.RedirectURI {
		return false
	}
	u, err := url.Parse(cfg.RedirectURI)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.ForceQuery ||
		u.Fragment != "" || u.RawPath != "" || u.Path != "/feishu/callback" {
		return false
	}
	ip, err := netip.ParseAddr(u.Hostname())
	if err != nil || !ip.Is4() || !ip.IsPrivate() {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	expires, err := time.Parse(time.RFC3339, os.Getenv("FEISHU_TEST_HTTP_CALLBACK_EXPIRES_AT"))
	now := time.Now()
	return err == nil && expires.After(now) && !expires.After(now.Add(24*time.Hour))
}
