package utils

import "testing"

func TestFeishuCredentialsAreOpaqueToObservability(t *testing.T) {
	for _, path := range []string{
		"/api/v1/system/admin/feishu",
		"/api/v1/system/admin/feishu/test",
		"/api/v1/me/feishu",
		"/api/v1/me/feishu/authorize",
		"/api/v1/me/feishu/callback",
		"/api/v1/auth/oidc/callback",
	} {
		if !IsAuthenticationPath(path) {
			t.Errorf("credential route not opaque: %s", path)
		}
	}
	for _, path := range []string{
		"/api/v1/knowledge-bases/kb/feishu",
		"/api/v1/me/feishu-unrelated",
		"/api/v1/system/admin/feishu-unrelated",
	} {
		if IsAuthenticationPath(path) {
			t.Errorf("ordinary route misclassified: %s", path)
		}
	}
}
