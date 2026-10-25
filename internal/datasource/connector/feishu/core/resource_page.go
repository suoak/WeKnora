package core

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
)

// ResourceEntry describes one accessible child without implying content access.
type ResourceEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	URL         string `json:"url,omitempty"`
	HasChildren bool   `json:"has_children"`
	SpaceID     string `json:"space_id,omitempty"`
}

// ResourcePage holds one bounded level of Drive or Wiki metadata.
type ResourcePage struct {
	Items    []ResourceEntry `json:"items"`
	HasMore  bool            `json:"has_more"`
	NextPage string          `json:"-"`
}

// ReadResourcePage reads one level and one bounded page. It never uses the
// unbounded all-pages convenience helpers or discovers "shared with me".
func (c *Client) ReadResourcePage(
	ctx context.Context,
	kind, parent, space, page string,
	size int,
) (ResourcePage, error) {
	result := ResourcePage{Items: []ResourceEntry{}}
	if size < 1 || size > 50 || len(page) > 2048 {
		return result, errors.New("invalid_resource_page")
	}
	for _, id := range []string{parent, space} {
		if id != "" && !resourceTokenPattern.MatchString(id) {
			return result, errors.New("invalid_resource_parent")
		}
	}
	q := url.Values{"page_size": {strconv.Itoa(size)}}
	if page != "" {
		q.Set("page_token", page)
	}
	switch kind {
	case "drive":
		if parent == "" {
			var root struct {
				Data struct {
					Token string `json:"token"`
				} `json:"data"`
			}
			if err := c.
				DoRequest(ctx,
					http.MethodGet,
					"/open-apis/drive/explorer/v2/root_folder/meta",
					nil,
					&root); err != nil {
				return result, err
			}
			if !resourceTokenPattern.MatchString(root.Data.Token) {
				return result, errors.New("invalid_drive_root")
			}
			parent = root.Data.Token
		}
		q.Set("folder_token", parent)
		var body struct {
			Data struct {
				Files []struct {
					Token string `json:"token"`
					Name  string `json:"name"`
					Type  string `json:"type"`
					URL   string `json:"url"`
				} `json:"files"`
				HasMore bool   `json:"has_more"`
				Next    string `json:"next_page_token"`
			} `json:"data"`
		}
		if err := c.DoRequest(ctx, http.MethodGet, "/open-apis/drive/v1/files?"+q.Encode(), nil, &body); err != nil {
			return result, err
		}
		result.HasMore, result.NextPage = body.Data.HasMore, body.Data.Next
		for _, f := range body.Data.Files {
			result.Items = append(
				result.Items,
				ResourceEntry{
					ID:          f.Token,
					Name:        f.Name,
					Type:        f.Type,
					URL:         f.URL,
					HasChildren: f.Type == "folder",
				},
			)
		}
	case "wiki":
		if space == "" {
			var body struct {
				Data struct {
					Items []struct {
						ID   string `json:"space_id"`
						Name string `json:"name"`
					} `json:"items"`
					HasMore bool   `json:"has_more"`
					Next    string `json:"page_token"`
				} `json:"data"`
			}
			if err := c.
				DoRequest(ctx,
					http.MethodGet,
					"/open-apis/wiki/v2/spaces?"+q.
						Encode(),
					nil,
					&body); err != nil {
				return result, err
			}
			result.HasMore, result.NextPage = body.Data.HasMore, body.Data.Next
			for _, s := range body.Data.Items {
				result.Items = append(
					result.Items,
					ResourceEntry{
						ID:          s.ID,
						Name:        s.Name,
						Type:        "wiki_space",
						HasChildren: true,
						SpaceID:     s.ID,
					},
				)
			}
		} else {
			if parent != "" {
				q.Set("parent_node_token", parent)
			}
			var body struct {
				Data struct {
					Items []struct {
						ID       string `json:"node_token"`
						Title    string `json:"title"`
						Type     string `json:"obj_type"`
						HasChild bool   `json:"has_child"`
					} `json:"items"`
					HasMore bool   `json:"has_more"`
					Next    string `json:"page_token"`
				} `json:"data"`
			}
			if err := c.
				DoRequest(ctx,
					http.MethodGet,
					"/open-apis/wiki/v2/spaces/"+space+"/nodes?"+q.
						Encode(),
					nil,
					&body); err != nil {
				return result, err
			}
			result.HasMore, result.NextPage = body.Data.HasMore, body.Data.Next
			for _, n := range body.Data.Items {
				result.Items = append(result.Items,
					ResourceEntry{
						ID:          n.ID,
						Name:        n.Title,
						Type:        n.Type,
						URL:         "https://feishu.cn/wiki/" + n.ID,
						HasChildren: n.HasChild,
						SpaceID:     space,
					})
			}
		}
	default:
		return result, errors.New("invalid_resource_kind")
	}
	if len(result.Items) > size || result.HasMore && (result.NextPage == "" || result.NextPage == page) {
		return ResourcePage{}, errors.New("invalid_resource_pagination")
	}
	return result, nil
}
