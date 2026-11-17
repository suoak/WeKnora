package confluence

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"golang.org/x/sync/singleflight"
)

// Cache pages independently: a first-page request must never enumerate the site.
// Entries are permission-scoped and bounded across credentials and queries.
type pickerPageCache struct {
	mu      sync.Mutex
	entries map[string]pickerPageEntry
	loads   singleflight.Group
}
type pickerPageEntry struct {
	owner   string
	spaces  []space
	page    types.ResourcePage
	expires time.Time
}
type pickerCursor struct {
	Start    int    `json:"start,omitempty"`
	Cursor   string `json:"cursor,omitempty"`
	Query    string `json:"q,omitempty"`
	ParentID string `json:"parent,omitempty"`
	SpaceKey string `json:"key,omitempty"`
}

func (c *Connector) cachedPickerSpaces(cfg config, id string) []space {
	c.pickerPages.mu.Lock()
	defer c.pickerPages.mu.Unlock()
	owner := pickerSpaceCacheKey(cfg)
	for _, entry := range c.pickerPages.entries {
		if entry.owner != owner || !time.Now().Before(entry.expires) {
			continue
		}
		for _, s := range entry.spaces {
			if s.ID == id {
				return []space{s}
			}
		}
	}
	return nil
}

func pickerSpaceResource(client *client, cfg config, s space) types.Resource {
	metadata := map[string]interface{}{"space_key": s.Key}
	if cfg.cloud() {
		metadata["hierarchy_limitation"] = "cloud_top_level_containers"
	}
	return types.Resource{
		ExternalID:  s.ID,
		Name:        s.Name,
		Type:        "space",
		URL:         client.resourceURL(s.Links.WebUI),
		HasChildren: true,
		Metadata:    metadata,
	}
}

// ListResourcePage lists one permission-scoped resource page without enumerating the site.
func (c *Connector) ListResourcePage(
	ctx context.Context,
	ds *types.DataSourceConfig,
	options types.ResourceListOptions,
) (*types.ResourcePage, error) {
	client, cfg, err := c.configured(ds)
	if err != nil {
		return nil, err
	}
	options.Query = strings.TrimSpace(options.Query)
	if options.Limit == 0 {
		options.Limit = 50
	}
	if options.Limit < 1 || options.Limit > 100 || len(options.Query) > 256 || len(options.Cursor) > 4096 ||
		len(options.ParentID) > 256 ||
		len(options.SpaceKey) > 256 ||
		len(options.RevealID) > 256 {
		return nil, fmt.Errorf("invalid resource page options")
	}
	if options.ParentID != "" && options.Query != "" {
		return nil, fmt.Errorf("child listing does not accept root search")
	}
	if options.RevealID != "" {
		if options.ParentID != "" || options.Query != "" || options.Cursor != "" {
			return nil, fmt.Errorf("invalid reveal options")
		}
		return c.pickerSelectionPath(ctx, client, cfg, options.RevealID)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var cursor pickerCursor
	if options.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(options.Cursor)
		if err != nil || json.Unmarshal(raw, &cursor) != nil || cursor.Start < 0 || cursor.Query != options.Query ||
			cursor.ParentID != options.ParentID ||
			cursor.SpaceKey != options.SpaceKey {
			return nil, fmt.Errorf("invalid resource cursor")
		}
	}
	owner := pickerSpaceCacheKey(cfg)
	keyBytes, _ := json.Marshal(options)
	key := owner + "\x00" + string(keyBytes)
	load := func() (interface{}, error) {
		c.pickerPages.mu.Lock()
		cached, ok := c.pickerPages.entries[key]
		c.pickerPages.mu.Unlock()
		if ok && time.Now().Before(cached.expires) {
			return cached.page, nil
		}
		var spaces []space
		var page types.ResourcePage
		if options.ParentID != "" {
			childPage, err := c.pickerChildPage(ctx, client, cfg, options, cursor)
			if err != nil {
				return nil, err
			}
			page = *childPage
		} else {
			var next string
			spaces, next, err = client.pickerSpacePage(ctx, options, cursor)
			if err != nil {
				return nil, err
			}
			page = types.ResourcePage{Items: make([]types.Resource, 0, len(spaces)), NextCursor: next}
			for _, s := range spaces {
				page.Items = append(page.Items, pickerSpaceResource(client, cfg, s))
			}
		}
		c.pickerPages.mu.Lock()
		defer c.pickerPages.mu.Unlock()
		if c.pickerPages.entries == nil {
			c.pickerPages.entries = make(map[string]pickerPageEntry)
		}
		for k, entry := range c.pickerPages.entries {
			if time.Now().After(entry.expires) {
				delete(c.pickerPages.entries, k)
			}
		}
		if len(c.pickerPages.entries) >= 256 {
			oldestKey := ""
			var oldest time.Time
			for k, entry := range c.pickerPages.entries {
				if oldestKey == "" || entry.expires.Before(oldest) {
					oldestKey, oldest = k, entry.expires
				}
			}
			delete(c.pickerPages.entries, oldestKey)
		}
		c.pickerPages.entries[key] = pickerPageEntry{
			owner:   owner,
			spaces:  spaces,
			page:    page,
			expires: time.Now().Add(pickerSpaceCacheTTL),
		}
		return page, nil
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-c.pickerPages.loads.DoChan(key, load):
		if result.Err != nil {
			return nil, result.Err
		}
		page := result.Val.(types.ResourcePage)
		page.Items = append([]types.Resource{}, page.Items...)
		return &page, nil
	}
}

// Resolve a supplied key using the upstream API and verify its ID before using
// it. The key is only a lookup hint, never authority to select another space.
func (c *Connector) pickerChildPage(
	ctx context.Context,
	client *client,
	cfg config,
	options types.ResourceListOptions,
	cursor pickerCursor,
) (*types.ResourcePage, error) {
	ref, err := parseResourceID(options.ParentID)
	if err != nil {
		return nil, err
	}
	if id, err := strconv.ParseUint(ref.SpaceID, 10, 64); err != nil || id == 0 {
		return nil, fmt.Errorf("invalid Confluence space ID")
	}
	if strings.ContainsAny(options.SpaceKey, "/\\\x00\r\n") || options.SpaceKey == "." || options.SpaceKey == ".." {
		return nil, fmt.Errorf("invalid Confluence space key")
	}
	if ref.Kind == resourcePage {
		if id, err := strconv.ParseUint(ref.PageID, 10, 64); err != nil || id == 0 {
			return nil, fmt.Errorf("invalid Confluence page ID")
		}
	}
	selected, err := client.pickerSpace(ctx, ref.SpaceID, options.SpaceKey)
	if err != nil {
		return nil, err
	}
	var pages []page
	var next string
	if !cfg.cloud() || ref.Kind == resourceSpace {
		endpoint := serverTopLevelPagesEndpoint(selected.Key)
		if ref.Kind == resourcePage {
			endpoint = serverChildPagesEndpoint(ref.PageID)
		}
		if cfg.cloud() {
			endpoint = "/api/v2/spaces/" + url.PathEscape(selected.ID) + "/pages?status=current&depth=0"
		}
		if ref.Kind == resourcePage {
			if err := client.validatePageOwnership(ctx, selected, ref.PageID); err != nil {
				return nil, err
			}
		}
		u, _ := url.Parse(endpoint)
		q := u.Query()
		q.Set("limit", strconv.Itoa(options.Limit))
		if cursor.Start > 0 {
			q.Set("start", strconv.Itoa(cursor.Start))
		}
		if cursor.Cursor != "" {
			q.Set("cursor", cursor.Cursor)
		}
		u.RawQuery = q.Encode()
		if cfg.cloud() {
			var result cloudHierarchyList
			if err := client.get(ctx, u.String(), &result); err != nil {
				return nil, err
			}
			for _, item := range result.Results {
				if item.Type == "" || item.Type == "page" {
					pages = append(
						pages,
						page{
							ID:      item.ID,
							Title:   item.Title,
							Status:  item.Status,
							Kind:    item.Type,
							SpaceID: item.SpaceID,
							Links:   item.Links,
						},
					)
				}
			}
			next, err = client.pickerContinuation(result.Links.Next, cursor, options, len(result.Results))
		} else {
			var result pageList
			if err := client.get(ctx, u.String(), &result); err != nil {
				return nil, err
			}
			pages = result.Results
			next, err = client.pickerContinuation(result.Links.Next, cursor, options, len(pages))
		}
	} else {
		// Cloud's transparent containers retain the existing projection contract.
		// A Server continuation cannot be replayed against this traversal.
		if options.Cursor != "" {
			return nil, fmt.Errorf("cloud mixed hierarchy does not accept a page cursor")
		}
		if err := client.validatePageOwnership(ctx, selected, ref.PageID); err != nil {
			return nil, err
		}
		pages, err = client.directChildPages(ctx, selected, ref.PageID)
	}
	if err != nil {
		return nil, err
	}
	out := &types.ResourcePage{Items: make([]types.Resource, 0, len(pages)), NextCursor: next}
	for _, p := range pages {
		out.Items = append(out.Items, pageResource(client, selected, options.ParentID, p))
	}
	return out, nil
}

func (c *client) pickerSpacePage(
	ctx context.Context,
	options types.ResourceListOptions,
	cursor pickerCursor,
) ([]space, string, error) {
	query := url.Values{"limit": {strconv.Itoa(options.Limit)}}
	endpoint := "/rest/api/space"
	if c.cfg.cloud() {
		endpoint = "/api/v2/spaces"
	}
	if options.Query != "" {
		// The search API (not content/search) can return space entities. Quote
		// user text as a CQL literal so it cannot change the query's scope.
		endpoint = "/rest/api/search"
		literal := strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(options.Query)
		query.Set("cql", `type=space AND title~"`+literal+`"`)
	}
	if cursor.Start > 0 {
		query.Set("start", strconv.Itoa(cursor.Start))
	}
	if cursor.Cursor != "" {
		query.Set("cursor", cursor.Cursor)
	}
	var spaces []space
	var next string
	if options.Query != "" {
		var result struct {
			Results []struct {
				Space struct {
					ID    json.Number `json:"id"`
					Key   string      `json:"key"`
					Name  string      `json:"name"`
					Links struct {
						WebUI string `json:"webui"`
					} `json:"_links"`
				} `json:"space"`
			} `json:"results"`
			Links struct {
				Next string `json:"next"`
			} `json:"_links"`
		}
		if err := c.get(ctx, endpoint+"?"+query.Encode(), &result); err != nil {
			return nil, "", err
		}
		for _, hit := range result.Results {
			if hit.Space.ID == "" {
				return nil, "", fmt.Errorf("confluence search did not return space entities; browse spaces instead")
			}
			spaces = append(
				spaces,
				space{ID: hit.Space.ID.String(), Key: hit.Space.Key, Name: hit.Space.Name, Links: hit.Space.Links},
			)
		}
		next = result.Links.Next
	} else if c.cfg.cloud() {
		var result spaceList
		if err := c.get(ctx, endpoint+"?"+query.Encode(), &result); err != nil {
			return nil, "", err
		}
		spaces, next = result.Results, result.Links.Next
	} else {
		var result serverSpaceList
		if err := c.get(ctx, endpoint+"?"+query.Encode(), &result); err != nil {
			return nil, "", err
		}
		for _, s := range result.Results {
			spaces = append(spaces, space{ID: s.ID.String(), Key: s.Key, Name: s.Name, Links: s.Links})
		}
		next = result.Links.Next
	}
	nextToken, err := c.pickerContinuation(next, cursor, options, len(spaces))
	return spaces, nextToken, err
}

func (c *client) pickerContinuation(
	next string,
	cursor pickerCursor,
	options types.ResourceListOptions,
	count int,
) (string, error) {
	if next == "" || count == 0 {
		return "", nil
	}
	// Never accept an arbitrary outbound URL from a caller's continuation token.
	// Only the offset/cursor from a validated upstream link is carried forward.
	resolved, err := c.resolveEndpoint(next)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(resolved)
	if err != nil {
		return "", err
	}
	nextCursor := pickerCursor{
		Query:    options.Query,
		ParentID: options.ParentID,
		SpaceKey: options.SpaceKey,
		Cursor:   u.Query().Get("cursor"),
	}
	if raw := u.Query().Get("start"); raw != "" {
		nextCursor.Start, err = strconv.Atoi(raw)
		if err != nil || nextCursor.Start <= cursor.Start {
			return "", fmt.Errorf("confluence pagination did not advance")
		}
	}
	if nextCursor.Cursor == "" && nextCursor.Start == 0 ||
		nextCursor.Cursor != "" && nextCursor.Cursor == cursor.Cursor {
		return "", fmt.Errorf("confluence pagination did not advance")
	}
	raw, _ := json.Marshal(nextCursor)
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
