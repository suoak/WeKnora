package confluence

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

func (c *client) pickerSpace(ctx context.Context, id, key string) (space, error) {
	var s space
	if value, err := strconv.ParseUint(id, 10, 64); err != nil || value == 0 {
		return s, fmt.Errorf("invalid Confluence space ID")
	}
	if c.cfg.cloud() {
		if err := c.get(ctx, "/api/v2/spaces/"+url.PathEscape(id), &s); err != nil {
			return s, err
		}
	} else {
		if key == "" || strings.ContainsAny(key, "/\\\x00\r\n") || key == "." || key == ".." {
			return s, fmt.Errorf("invalid Confluence space key")
		}
		var result struct {
			ID    json.Number `json:"id"`
			Key   string      `json:"key"`
			Name  string      `json:"name"`
			Links struct {
				WebUI string `json:"webui"`
			} `json:"_links"`
		}
		if err := c.get(ctx, "/rest/api/space/"+url.PathEscape(key), &result); err != nil {
			return s, err
		}
		s = space{ID: result.ID.String(), Key: result.Key, Name: result.Name, Links: result.Links}
	}
	if s.ID != id {
		return space{}, fmt.Errorf("Confluence space key does not match requested space")
	}
	return s, nil
}

// Return only the selected page's ancestor chain, never a whole-site listing.
// Every supplied ID is checked against the authenticated page/space responses.
func (c *Connector) pickerSelectionPath(ctx context.Context, client *client, cfg config, id string) (*types.ResourcePage, error) {
	ref, err := parseResourceID(id)
	if err != nil {
		return nil, err
	}
	if ref.Kind == resourceSpace {
		if cached := c.cachedPickerSpaces(cfg, ref.SpaceID); len(cached) > 0 {
			s, err := client.pickerSpace(ctx, ref.SpaceID, cached[0].Key)
			if err != nil {
				return nil, err
			}
			return &types.ResourcePage{Items: []types.Resource{pickerSpaceResource(client, cfg, s)}}, nil
		}
		if !cfg.cloud() {
			return nil, fmt.Errorf("browse or search for this space to reveal it")
		}
		s, err := client.pickerSpace(ctx, ref.SpaceID, "")
		if err != nil {
			return nil, err
		}
		return &types.ResourcePage{Items: []types.Resource{pickerSpaceResource(client, cfg, s)}}, nil
	}
	if n, err := strconv.ParseUint(ref.PageID, 10, 64); err != nil || n == 0 {
		return nil, fmt.Errorf("invalid Confluence page ID")
	}
	var full page
	if cfg.cloud() {
		full, err = client.pageDetail(ctx, ref.PageID)
	} else {
		err = client.get(ctx, "/rest/api/content/"+url.PathEscape(ref.PageID)+"?expand=ancestors,space", &full)
	}
	if err != nil {
		return nil, err
	}
	if full.ID != ref.PageID || cfg.cloud() && full.SpaceID != ref.SpaceID {
		return nil, fmt.Errorf("Confluence page does not match requested selection")
	}
	s, err := client.pickerSpace(ctx, ref.SpaceID, full.Space.Key)
	if err != nil {
		return nil, err
	}
	ancestors := full.Ancestors
	if cfg.cloud() {
		ancestors, err = client.listCloudHierarchy(ctx, "/api/v2/pages/"+url.PathEscape(ref.PageID)+"/ancestors?limit=250")
		if err != nil {
			return nil, err
		}
	}
	if len(ancestors) > 512 {
		return nil, fmt.Errorf("Confluence ancestor path is too deep")
	}
	out := &types.ResourcePage{Items: []types.Resource{pickerSpaceResource(client, cfg, s)}}
	parent := s.ID
	seen := map[string]bool{ref.PageID: true}
	for _, p := range ancestors {
		if p.Kind != "" && p.Kind != "page" {
			continue
		}
		if n, err := strconv.ParseUint(p.ID, 10, 64); err != nil || n == 0 || seen[p.ID] {
			return nil, fmt.Errorf("invalid Confluence ancestor path")
		}
		seen[p.ID] = true
		out.Items = append(out.Items, pageResource(client, s, parent, p))
		parent = makePageResourceID(s.ID, p.ID)
	}
	out.Items = append(out.Items, pageResource(client, s, parent, full))
	return out, nil
}
