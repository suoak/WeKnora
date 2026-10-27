package core

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type discoveryPageFunc func(context.Context, string, string, string, string, int) (ResourcePage, error)

func (f discoveryPageFunc) ReadResourcePage(
	ctx context.Context, kind, parent, space, page string, size int,
) (ResourcePage, error) {
	return f(ctx, kind, parent, space, page, size)
}

func TestDiscoveryFrontierPersistsOnePageWithoutReplayingPriorDirectory(t *testing.T) {
	frontier, err := NewDiscoveryFrontier("drive", "root", "")
	require.NoError(t, err)
	var positions []string
	read := discoveryPageFunc(func(_ context.Context, kind, parent, _, page string, size int) (ResourcePage, error) {
		require.Equal(t, "drive", kind)
		require.Equal(t, 50, size)
		positions = append(positions, parent+":"+page)
		result := ResourcePage{ResolvedParent: parent}
		switch parent + ":" + page {
		case "root:":
			result.Items = []ResourceEntry{
				{ID: "folder", Type: "folder", HasChildren: true},
				{ID: "first", Type: "docx", URL: "https://tenant.feishu.cn/docx/first"},
			}
			result.HasMore, result.NextPage = true, "next"
		case "root:next":
			result.Items = []ResourceEntry{{ID: "second", Type: "sheet", URL: "https://tenant.feishu.cn/sheets/second"}}
		case "folder:":
			result.Items = []ResourceEntry{{ID: "third", Type: "bitable", URL: "https://tenant.feishu.cn/base/third"}}
		default:
			t.Fatalf("unexpected request: %s:%s", parent, page)
		}
		return result, nil
	})
	var found []DiscoveredLink
	for len(frontier.Queue) > 0 {
		before, err := json.Marshal(frontier)
		require.NoError(t, err)
		next, links, err := AdvanceDiscovery(context.Background(), read, frontier)
		require.NoError(t, err)
		unchanged, err := json.Marshal(frontier)
		require.NoError(t, err)
		require.JSONEq(t, string(before), string(unchanged), "advance must not mutate uncommitted state")
		encoded, err := json.Marshal(next)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(encoded, &frontier), "simulate worker restart after each commit")
		found = append(found, links...)
	}
	require.Equal(t, []string{"root:", "root:next", "folder:"}, positions)
	require.Len(t, found, 3)
	require.Equal(t, []string{"folder", "third"}, found[2].Path)
	require.Equal(t, 3, frontier.Pages)
	require.False(t, frontier.Truncated)
}

func TestDiscoveryFrontierPreservesUnacknowledgedPageOnFault(t *testing.T) {
	frontier, err := NewDiscoveryFrontier("wiki", "shortcut", "space")
	require.NoError(t, err)
	frontier.Queue[0].Page = "second"
	frontier.Queue[0].ResolvedParent, frontier.Queue[0].ResolvedSpace = "original", "space"
	for _, fault := range []string{"provider", "moved", "repeated_page"} {
		t.Run(fault, func(t *testing.T) {
			read := discoveryPageFunc(func(context.Context, string, string, string, string, int) (ResourcePage, error) {
				if fault == "provider" {
					return ResourcePage{}, errors.New("permission withdrawn")
				}
				result := ResourcePage{ResolvedParent: "original", ResolvedSpace: "space"}
				if fault == "moved" {
					result.ResolvedParent = "different"
				} else {
					result.HasMore, result.NextPage = true, "second"
				}
				return result, nil
			})
			next, links, err := AdvanceDiscovery(context.Background(), read, frontier)
			require.Error(t, err)
			require.Equal(t, frontier, next)
			require.Empty(t, links)
		})
	}
}

func TestDiscoveryFrontierBoundsDepthAndShortcutCycles(t *testing.T) {
	frontier, err := NewDiscoveryFrontier("wiki", "root", "space")
	require.NoError(t, err)
	frontier.Queue[0].Depth = 10
	read := discoveryPageFunc(func(context.Context, string, string, string, string, int) (ResourcePage, error) {
		return ResourcePage{ResolvedParent: "root", ResolvedSpace: "space", Items: []ResourceEntry{
			{ID: "deeper", SpaceID: "space", HasChildren: true},
			{ID: "doc", Type: "docx", URL: "https://tenant.feishu.cn/docx/doc"},
		}}, nil
	})
	next, links, err := AdvanceDiscovery(context.Background(), read, frontier)
	require.NoError(t, err)
	require.True(t, next.Truncated, "depth limit is partial discovery, never complete success")
	require.Len(t, links, 1)
	require.Empty(t, next.Queue)
	frontier.Queue[0].Depth = 0
	_, links, err = AdvanceDiscovery(context.Background(), read, frontier)
	require.NoError(t, err)
	require.Len(t, links, 2)
	require.Equal(t, "https://feishu.cn/wiki/root", links[0].URL, "include the selected Wiki node itself")
	frontier.Visited["wiki:space:root"] = true
	next, links, err = AdvanceDiscovery(context.Background(), read, frontier)
	require.NoError(t, err)
	require.Empty(t, links, "shortcut resolving to an already visited target must not enumerate twice")
	require.Empty(t, next.Queue)
	_, err = NewDiscoveryFrontier("wiki", "", "")
	require.Error(t, err, "never expand all spaces implicitly")
}
