package feishu

import (
	"strings"
	"unicode/utf8"
)

// Keep the existing conservative byte budget (including worst-case JSON
// escaping). Prefer whole paragraphs/tables/fences over arbitrary byte cuts.
// Very large blocks still split, with fences and table headers carried over.
func splitMarkdownReply(text string) []string {
	if len(text) <= textReplyChunkBytes {
		return []string{text}
	}
	var parts []string
	var current strings.Builder
	fence, opener, tableHeader := "", "", ""
	lines := strings.SplitAfter(text, "\n")
	flush := func() {
		if current.Len() == 0 {
			return
		}
		part := current.String()
		if fence != "" {
			part += "\n" + fence + "\n"
		}
		parts = append(parts, part)
		current.Reset()
		if fence != "" {
			current.WriteString(opener + "\n")
		} else if tableHeader != "" {
			current.WriteString(tableHeader)
		}
	}
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if fence == "" {
			if !strings.Contains(trimmed, "|") {
				tableHeader = ""
			} else if i+1 < len(lines) && markdownTableSeparator(lines[i+1]) {
				// Move the header and delimiter together to the next message.
				if current.Len()+len(line)+len(lines[i+1])+16 > textReplyChunkBytes {
					tableHeader = ""
					flush()
				}
				tableHeader = line + lines[i+1]
				if len(tableHeader) > textReplyChunkBytes/2 {
					tableHeader = "" // an oversized header must still make progress
				}
			}
		}
		// Reserve room for a synthetic fence terminator. Never split UTF-8.
		for len(line) > 0 {
			space := textReplyChunkBytes - current.Len() - len(fence) - 2
			if len(line) <= space {
				current.WriteString(line)
				break
			}
			if current.Len() > len(opener)+len(tableHeader)+1 {
				flush()
				continue
			}
			cut := space
			for cut > 0 && !utf8.RuneStart(line[cut]) {
				cut--
			}
			if cut <= 0 {
				// An unusually long fence info string cannot be safely repeated.
				fence, opener, tableHeader = "", "", ""
				flush()
				continue
			}
			current.WriteString(line[:cut])
			line = line[cut:]
			flush()
		}
		if fence == "" {
			if marker := markdownFence(trimmed); marker != "" && len(trimmed) < textReplyChunkBytes/4 {
				fence, opener = marker, trimmed
			}
		} else if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, string(fence[0])) == "" {
			fence, opener = "", ""
		}
		// A paragraph boundary is preferable when most of the budget is used.
		if trimmed == "" && fence == "" && current.Len() >= textReplyChunkBytes/2 {
			tableHeader = ""
			flush()
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

func markdownFence(line string) string {
	if !strings.HasPrefix(line, "```") && !strings.HasPrefix(line, "~~~") {
		return ""
	}
	n := 0
	for n < len(line) && line[n] == line[0] {
		n++
	}
	return line[:n]
}

func markdownTableSeparator(line string) bool {
	line = strings.TrimSpace(line)
	return strings.Contains(line, "|") && strings.Contains(line, "-") &&
		strings.Trim(line, "|:- \t\r\n") == ""
}
