package officeguard

import (
	"strings"
	"unicode"

	werrors "github.com/Tencent/WeKnora/internal/errors"
)

// ValidateParsedText is a second defense, independent of binary inspection.
// Refuse only an entire known compatibility notice with no remaining body.
// Quoting the notice in an ordinary document must remain usable.
func ValidateParsedText(fileType, text string) error {
	switch strings.TrimPrefix(strings.ToLower(fileType), ".") {
	case "doc", "docx", "docm", "xls", "xlsx", "xlsm", "ppt", "pptx", "pptm", "wps", "et", "dps":
	default:
		return nil
	}
	if len(text) > 16<<10 {
		return nil
	}
	normalize := func(s string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
				return -1
			}
			return unicode.ToLower(r)
		}, s)
	}
	const notice = "对此文档的权限当前受到限制。只有使用 Microsoft Office 2003 或更高版本才能打开该文档。您可以向文档的作者申请一份可以使用用于 Internet Explorer 的权限管理加载项阅读的文档副本。"
	value := normalize(text)
	template := normalize(notice)
	if value != "" && strings.Contains(value, template) && strings.ReplaceAll(value, template, "") == "" {
		return &werrors.AppError{Code: werrors.ErrOfficeEncrypted, Message: DRMMessage, HTTPCode: 400}
	}
	return nil
}
