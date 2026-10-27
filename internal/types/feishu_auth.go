package types

const (
	// FeishuUserOAuth identifies feishu user oauth.
	FeishuUserOAuth = "user_oauth"
	// FeishuApplicationGrant selects author-granted application access, not user OAuth.
	FeishuApplicationGrant = "application_grant"
	// TypeFeishuApplicationProcess identifies type feishu application process.
	// Unknown to legacy consumers. Do not publish application attempts as the
	// old document:process task, whose JSON decoder ignores new identity fields.
	TypeFeishuApplicationProcess = "feishu:application:process:v1"
	// TypeFeishuConnectionCheck verifies only a personal identity, never content.
	TypeFeishuConnectionCheck = "feishu:connection:check:v1"
)

// FeishuAuthMode provides the feishu auth mode operation.
func FeishuAuthMode(mode string) string {
	if mode == "" {
		return FeishuUserOAuth
	}
	return mode
}
