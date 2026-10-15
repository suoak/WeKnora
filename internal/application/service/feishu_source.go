package service

func personalFeishuSource(channel string, metadata map[string]string) string {
	if channel == "feishu_personal" {
		return metadata["source_url"]
	}
	return ""
}
