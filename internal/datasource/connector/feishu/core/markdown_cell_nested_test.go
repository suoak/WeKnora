package core

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNativeTablePreservesNestedCellTextWithoutDuplicates(t *testing.T) {
	blocks := []DocxBlock{
		tableBlk("table", 1, "cell"),
		{BlockID: "cell", BlockType: BlockTypeTableCell, Children: []string{"callout", "after"}},
		{BlockID: "callout", BlockType: BlockTypeCallout, Children: []string{"item"}},
		{BlockID: "item", BlockType: BlockTypeBullet, Bullet: txt("初始化参数"), Children: []string{"nested"}},
		{BlockID: "nested", BlockType: BlockTypeText, Text: txt("appKey 必填")},
		{BlockID: "after", BlockType: BlockTypeText, Text: txt("最后启动")},
	}
	md, _, err := blocksToMarkdown(t.Context(), nil, blocks)
	require.NoError(t, err)
	require.Contains(t, string(md), "| 初始化参数 appKey 必填 最后启动 |")
	for _, text := range []string{"初始化参数", "appKey 必填", "最后启动"} {
		require.Equal(t, 1, strings.Count(string(md), text))
	}
}

func TestNativeTableNestedCellReferencesAreBounded(t *testing.T) {
	blocks := []DocxBlock{
		tableBlk("table", 1, "cell"),
		{BlockID: "cell", BlockType: BlockTypeTableCell, Children: []string{"container", "leaf", "missing"}},
		{BlockID: "container", BlockType: BlockTypeCallout, Children: []string{"leaf", "cell", "container"}},
		{BlockID: "leaf", BlockType: BlockTypeText, Text: txt("保留一次")},
	}
	md, _, err := blocksToMarkdown(t.Context(), nil, blocks)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(md), "保留一次"))
}

func TestNativeTableDoesNotConsumeMediaSubtrees(t *testing.T) {
	for _, kind := range []int{BlockTypeFile, BlockTypeImage} {
		blocks := []DocxBlock{
			tableBlk("table", 1, "cell"),
			{BlockID: "cell", BlockType: BlockTypeTableCell, Children: []string{"media"}},
			{BlockID: "media", BlockType: kind, Children: []string{"caption"}},
			{BlockID: "caption", BlockType: BlockTypeText, Text: txt("媒体说明")},
		}
		md, _, err := blocksToMarkdown(t.Context(), nil, blocks)
		require.NoError(t, err)
		require.NotContains(t, string(md), "| 媒体说明 |")
		require.Equal(t, 1, strings.Count(string(md), "媒体说明"))
	}
}
