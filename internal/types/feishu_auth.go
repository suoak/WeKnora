package types

const (
	FeishuUserOAuth        = "user_oauth"
	FeishuApplicationGrant = "application_grant"
	// Unknown to legacy consumers. Do not publish application attempts as the
	// old document:process task, whose JSON decoder ignores new identity fields.
	TypeFeishuApplicationProcess = "feishu:application:process:v1"
)

func FeishuAuthMode(mode string) string {
	if mode == "" {
		return FeishuUserOAuth
	}
	return mode
}
