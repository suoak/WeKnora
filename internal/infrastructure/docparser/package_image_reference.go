package docparser

import "strings"

// ReplacePackageImageReference replaces only complete image constructs pointing
// at the exact parser-issued target. The caller must authenticate the package
// bytes and provide their complete parsed representation before using it.
// This helper itself conveys no source or read authority.
func ReplacePackageImageReference(markdown, target string) (string, int) {
	var result strings.Builder
	last, count := 0, 0
	for _, span := range scanMarkdownImageTargets(markdown) {
		if strings.TrimSpace(markdown[span.TargetStart:span.TargetEnd]) != target {
			continue
		}
		result.WriteString(markdown[last:span.ImageStart])
		result.WriteString("[Embedded workbook: complete worksheet content follows below]")
		last = span.TargetEnd + 1
		count++
	}
	result.WriteString(markdown[last:])
	return result.String(), count
}
