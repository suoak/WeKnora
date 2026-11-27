package officeguard

import (
	"strings"
	"testing"
)

const compatibilityNotice = "对此文档的权限当前受到限制。只有使用 Microsoft Office 2003 或更高版本才能打开该文档。您可以向文档的作者申请一份可以使用用于 Internet Explorer 的权限管理加载项阅读的文档副本。"

func TestOfficePlaceholder(t *testing.T) {
	for _, tc := range []struct {
		name, kind, text string
		refuse           bool
	}{
		{"notice", "doc", compatibilityNotice, true},
		{"line wrapping", "DOCX", strings.ReplaceAll(compatibilityNotice, " ", "\r\n"), true},
		{"markdown wrapping", ".xlsx", "**" + compatibilityNotice + "**", true},
		{"duplicate notice", "pptx", compatibilityNotice + "\n" + compatibilityNotice, true},
		{"non Office", "txt", compatibilityNotice, false},
		{"one phrase", "doc", "权限当前受到限制", false},
		{"two phrases", "doc", "权限当前受到限制：用于 Internet Explorer 的权限管理加载项", false},
		{"real body", "doc", compatibilityNotice + "\n这是实际的文档正文。", false},
		{"discussion", "docx", "本文分析以下提示的原因：" + compatibilityNotice, false},
		{"empty", "doc", "", false},
		{"large", "doc", strings.Repeat("正文", 9000) + compatibilityNotice, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateParsedText(tc.kind, tc.text)
			if IsEncryptedError(err) != tc.refuse {
				t.Fatalf("refusal=%v want %v", err, tc.refuse)
			}
		})
	}
}
