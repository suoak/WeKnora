package core

import (
	"context"
	"errors"
)

// DiscoveryPosition is one level/page in an explicitly selected tree. Tokens
// remain server-side. The resolved target binds pages across Wiki shortcuts.
type DiscoveryPosition struct {
	Parent, Space, Page           string
	ResolvedParent, ResolvedSpace string
	Depth                         int
	Path                          []string
}

// DiscoveryFrontier can be persisted after each page; resumption never touches
// a document source's sync cursor, generation, lease or publication state.
type DiscoveryFrontier struct {
	Kind       string
	Queue      []DiscoveryPosition
	Visited    map[string]bool
	Pages      int
	Discovered int
	Truncated  bool
	Deferred   []DiscoveryPosition
}

// DiscoveredLink keeps directory provenance separately from canonical content
// identity. The existing batch/import pipeline owns content deduplication.
type DiscoveredLink struct {
	URL  string   `json:"url"`
	Path []string `json:"path"`
}

// ResourcePageReader supports the existing bound-identity client.
type ResourcePageReader interface {
	ReadResourcePage(context.Context, string, string, string, string, int) (ResourcePage, error)
}

// NewDiscoveryFrontier requires an explicit Drive folder or Wiki space/subtree.
// It deliberately does not select all Wiki spaces or a caller's whole drive.
func NewDiscoveryFrontier(kind, parent, space string) (DiscoveryFrontier, error) {
	if kind != "drive" && kind != "wiki" || kind == "drive" && parent == "" ||
		kind == "wiki" && space == "" {
		return DiscoveryFrontier{}, errors.New("explicit_discovery_root_required")
	}
	for _, id := range []string{parent, space} {
		if id != "" && !resourceTokenPattern.MatchString(id) {
			return DiscoveryFrontier{}, errors.New("invalid_discovery_root")
		}
	}
	return DiscoveryFrontier{
		Kind: kind, Queue: []DiscoveryPosition{{Parent: parent, Space: space}},
		Visited: map[string]bool{},
	}, nil
}

// AdvanceDiscovery reads at most one provider page. Callers must atomically
// commit the returned frontier with the page's admitted batch. An error returns
// the original frontier, so failures cannot silently acknowledge a page.
func AdvanceDiscovery(
	ctx context.Context, reader ResourcePageReader, current DiscoveryFrontier,
) (DiscoveryFrontier, []DiscoveredLink, error) {
	if current.Kind != "drive" && current.Kind != "wiki" {
		return current, nil, errors.New("invalid_resource_kind")
	}
	if len(current.Queue) == 0 {
		return current, nil, nil
	}
	if current.Pages >= 1000 || current.Discovered >= 10000 || len(current.Queue) > 10000 {
		return current, nil, errors.New("discovery_capacity_requires_review")
	}
	position := current.Queue[0]
	page, err := reader.ReadResourcePage(ctx, current.Kind, position.Parent, position.Space, position.Page, 50)
	if err != nil {
		return current, nil, err
	}
	if position.Page != "" && (position.ResolvedParent != page.ResolvedParent ||
		position.ResolvedSpace != page.ResolvedSpace) {
		return current, nil, errors.New("discovery_parent_changed")
	}
	if len(page.Items) > 50 || page.HasMore && (page.NextPage == "" || page.NextPage == position.Page) {
		return current, nil, errors.New("invalid_resource_pagination")
	}
	next := current
	next.Queue = append([]DiscoveryPosition(nil), current.Queue[1:]...)
	next.Visited = make(map[string]bool, len(current.Visited)+1)
	for key, value := range current.Visited {
		next.Visited[key] = value
	}
	next.Pages++
	target := current.Kind + ":" + page.ResolvedSpace + ":" + page.ResolvedParent
	if position.Page == "" && next.Visited[target] {
		return next, nil, nil
	}
	next.Visited[target] = true
	links := []DiscoveredLink{}
	if current.Kind == "wiki" && position.Depth == 0 && position.Parent != "" &&
		position.Page == "" && current.Pages == 0 {
		links = append(links, DiscoveredLink{
			URL:  "https://feishu.cn/wiki/" + position.Parent,
			Path: []string{position.Parent},
		})
	}
	for _, item := range page.Items {
		if !resourceTokenPattern.MatchString(item.ID) {
			return current, nil, errors.New("invalid_discovery_child")
		}
		next.Discovered++
		path := append(append([]string(nil), position.Path...), item.ID)
		if item.URL != "" && item.Type != "folder" && item.Type != "wiki_space" {
			resource, parseErr := ParsePersonalResourceURL(item.URL)
			if parseErr == nil {
				links = append(links, DiscoveredLink{URL: resource.URL, Path: path})
			}
		}
		if item.HasChildren {
			if position.Depth >= 10 {
				next.Truncated = true
				continue
			}
			space := position.Space
			if item.SpaceID != "" {
				space = item.SpaceID
			}
			childKey := current.Kind + ":" + space + ":" + item.ID
			if !next.Visited[childKey] {
				next.Queue = append(next.Queue, DiscoveryPosition{
					Parent: item.ID, Space: space, Depth: position.Depth + 1, Path: path,
				})
			}
		}
	}
	if page.HasMore {
		position.Page = page.NextPage
		position.ResolvedParent, position.ResolvedSpace = page.ResolvedParent, page.ResolvedSpace
		// Finish the current directory's pagination before expanding children.
		next.Queue = append([]DiscoveryPosition{position}, next.Queue...)
	}
	if next.Discovered > 10000 || len(next.Queue) > 10000 {
		return current, nil, errors.New("discovery_capacity_requires_review")
	}
	return next, links, nil
}
