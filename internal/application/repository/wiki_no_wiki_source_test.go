package repository

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWikiNoWikiPlainTextKeepsThresholdAndImageInputs(t *testing.T) {
	base := func() map[string]interface{} {
		return map[string]interface{}{
			"content": "示例正文", "chunk_type": "text", "is_enabled": true,
			"context_header": "", "parent_chunk_id": "", "image_info": "", "deleted_at": nil,
		}
	}
	for name, change := range map[string]func(map[string]interface{}){
		"threshold":    func(r map[string]interface{}) { r["content"] = "1234567890" },
		"image":        func(r map[string]interface{}) { r["content"] = "![x](y)" },
		"image_info":   func(r map[string]interface{}) { r["image_info"] = `[{"ocr_text":"evidence"}]` },
		"ocr":          func(r map[string]interface{}) { r["chunk_type"] = "image_ocr" },
		"caption":      func(r map[string]interface{}) { r["chunk_type"] = "image_caption" },
		"header":       func(r map[string]interface{}) { r["context_header"] = "more evidence" },
		"child":        func(r map[string]interface{}) { r["parent_chunk_id"] = "parent" },
		"disabled":     func(r map[string]interface{}) { r["is_enabled"] = false },
		"deleted":      func(r map[string]interface{}) { r["deleted_at"] = "2026-01-01" },
		"unknown_bool": func(r map[string]interface{}) { r["is_enabled"] = "true" },
	} {
		t.Run(name, func(t *testing.T) {
			r := base()
			change(r)
			_, ok := wikiInsufficientPlainText([]map[string]interface{}{r})
			require.False(t, ok)
		})
	}
	r := base()
	r["content"] = "**示例**\n"
	n, ok := wikiInsufficientPlainText([]map[string]interface{}{r})
	require.True(t, ok)
	require.Equal(t, 6, n)
	r["is_enabled"] = int64(1)
	_, ok = wikiInsufficientPlainText([]map[string]interface{}{r})
	require.True(t, ok)
	_, ok = wikiInsufficientPlainText([]map[string]interface{}{r, r})
	require.False(t, ok)
	_, ok = wikiInsufficientPlainText(nil)
	require.False(t, ok)
}
