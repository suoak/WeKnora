package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ErrPersonalBlocksPolicyUnverified means the source's export/copy authorization
// could not be established. An API returning readable blocks is not that proof.
var ErrPersonalBlocksPolicyUnverified = errors.New("feishu_blocks_source_policy_unverified")

// ErrPersonalBlocksIncomplete prevents silent publication of truncated blocks,
// unsupported rich content, or detached image/attachment placeholders.
var ErrPersonalBlocksIncomplete = errors.New("feishu_blocks_content_requires_review")

var (
	ErrPersonalReadDenied        = errors.New("feishu_source_read_denied")
	ErrPersonalCopyExportDenied  = errors.New("feishu_source_copy_export_denied")
	ErrPersonalCopyDenied        = errors.New("feishu_source_copy_denied")
	ErrPersonalCopyOnlyReview    = errors.New("feishu_source_copy_only_requires_policy")
	ErrPersonalAcquisitionReview = errors.New("feishu_source_acquisition_requires_review")
)

// PersonalPermissions contains effective action results, not a document role.
// It is usable only when InspectPersonalPermissions returns no error.
type PersonalPermissions struct {
	View   bool `json:"view"`
	Export bool `json:"export"`
	Copy   bool `json:"copy"`
}

// PersonalResourcePermission asks Feishu for the CURRENT user's effective action
// permission. A missing field, failed call or false result never grants access.
// https://open.feishu.cn/document/server-docs/docs/permission/permission-member/auth
func (c *Client) PersonalResourcePermission(ctx context.Context, r PersonalResource, action string) (bool, error) {
	if _, app := ctx.Value(applicationIdentityKey{}).(applicationIdentity); app {
		return false, ErrAuth
	}
	if _, ok := ctx.Value(userIdentityKey{}).(userIdentity); !ok {
		return false, ErrAuth
	}
	if !resourceTokenPattern.MatchString(r.Token) ||
		r.Type != "docx" && r.Type != "sheet" && r.Type != "bitable" && r.Type != "file" ||
		action != "view" && action != "export" && action != "copy" {
		return false, ErrPermanent
	}
	var response struct {
		Data struct {
			Allowed *bool `json:"auth_result"`
		} `json:"data"`
	}
	err := c.doRequest(ctx, "check_personal_"+action+"_permission", http.MethodGet,
		"/open-apis/drive/v1/permissions/"+url.PathEscape(r.Token)+"/members/auth?type="+r.Type+"&action="+action,
		nil, &response)
	if err != nil {
		return false, err
	}
	if response.Data.Allowed == nil {
		return false, ErrPersonalBlocksPolicyUnverified
	}
	return *response.Data.Allowed, nil
}

func (c *Client) InspectPersonalPermissions(ctx context.Context, r PersonalResource) (PersonalPermissions, error) {
	var permissions PersonalPermissions
	for _, action := range []string{"view", "export", "copy"} {
		allowed, err := c.PersonalResourcePermission(ctx, r, action)
		if err != nil {
			return permissions, errors.Join(ErrPersonalBlocksPolicyUnverified, err)
		}
		switch action {
		case "view":
			permissions.View = allowed
		case "export":
			permissions.Export = allowed
		case "copy":
			permissions.Copy = allowed
		}
	}
	return permissions, nil
}

func (c *Client) personalBlocksPermission(ctx context.Context, r PersonalResource) (bool, error) {
	permissions, err := c.InspectPersonalPermissions(ctx, r)
	if err != nil {
		return false, err
	}
	switch {
	case !permissions.View:
		return false, ErrPersonalReadDenied
	case !permissions.Export && !permissions.Copy:
		return false, ErrPersonalCopyExportDenied
	case !permissions.Copy:
		return false, ErrPersonalCopyDenied
	case !permissions.Export:
		return false, ErrPersonalCopyOnlyReview
	}
	return true, nil
}

func (c *Client) personalBlocksFallback(
	ctx context.Context,
	r PersonalResource,
	exportErr error,
) ([]byte, string, error) {
	// Availability, identity and unknown failures retain their existing bounded
	// retry/stop semantics; they never select another source read route.
	if r.Type != "docx" || r.Modified == "" || errors.Is(exportErr, ErrAuth) ||
		errors.Is(exportErr, ErrTransient) || errors.Is(exportErr, ErrRateLimited) ||
		errors.Is(exportErr, ErrNotFound) || ctx.Err() != nil {
		return nil, "", exportErr
	}
	var api *APIError
	if !errors.As(exportErr, &api) || api.Operation != "create_export_task" ||
		!errors.Is(exportErr, ErrPermission) && !errors.Is(exportErr, ErrAppScope) &&
			!errors.Is(exportErr, ErrUserScope) && !errors.Is(exportErr, ErrPermanent) {
		return nil, "", exportErr
	}
	allowed, err := c.personalBlocksPermission(ctx, r)
	if err != nil {
		return nil, "", errors.Join(err, exportErr)
	}
	if !allowed {
		return nil, "", exportErr
	}
	// A readable API is not permission to bypass an unexplained export denial.
	// Only this documented format-capability error is currently accepted for
	// automatic fallback. Scope, unknown 403 and overloaded 9499 stay closed.
	if api.Code != 1069918 || api.Category != ErrorCategoryPermanent {
		return nil, "", errors.Join(ErrPersonalAcquisitionReview, exportErr)
	}
	blocks, err := c.personalDocumentBlocks(ctx, r.Token)
	if err != nil {
		return nil, "", err
	}
	return c.finishPersonalBlocks(ctx, r, blocks)
}

func (c *Client) finishPersonalBlocks(
	ctx context.Context,
	r PersonalResource,
	blocks []DocxBlock,
) ([]byte, string, error) {
	content, attachments, err := blocksToMarkdown(ctx, nil, blocks)
	if err != nil || len(attachments) != 0 || len(strings.TrimSpace(string(content))) == 0 || len(content) > 32<<20 {
		return nil, "", ErrPersonalBlocksIncomplete
	}
	allowed, err := c.personalBlocksPermission(ctx, r)
	if err != nil {
		return nil, "", err
	}
	if !allowed {
		return nil, "", ErrPermission
	}
	current, err := c.ResolveBoundResource(ctx, r)
	if err != nil {
		return nil, "", err
	}
	if current.Modified != r.Modified {
		return nil, "", fmt.Errorf("personal_blocks_revision_changed: %w", ErrTransient)
	}
	return content, SanitizeFileName(r.Title) + ".md", nil
}

// Unlike the legacy block iterator, this reader never treats a block limit or
// malformed/cyclic pagination as a complete document. It uses the same user,
// shared request budgets, provider error mapping and stop controller.
func (c *Client) personalDocumentBlocks(ctx context.Context, token string) ([]DocxBlock, error) {
	var all []DocxBlock
	page := ""
	seen := map[string]bool{}
	ids := map[string]bool{}
	for {
		var response struct {
			Data struct {
				Items     []json.RawMessage `json:"items"`
				HasMore   bool              `json:"has_more"`
				PageToken string            `json:"page_token"`
			} `json:"data"`
		}
		path := "/open-apis/docx/v1/documents/" + url.PathEscape(token) +
			"/blocks?page_size=500&document_revision_id=-1&page_token=" + url.QueryEscape(page)
		if err := c.doRequest(ctx, "get_personal_docx_blocks", http.MethodGet, path, nil, &response); err != nil {
			return nil, err
		}
		for _, raw := range response.Data.Items {
			var block DocxBlock
			if json.Unmarshal(raw, &block) != nil || block.BlockID == "" || ids[block.BlockID] ||
				!personalBlockSupported(raw, block) {
				return nil, ErrPersonalBlocksIncomplete
			}
			ids[block.BlockID] = true
			all = append(all, block)
		}
		if len(all) > maxDocumentBlocks {
			return nil, ErrPersonalBlocksIncomplete
		}
		if !response.Data.HasMore {
			break
		}
		page = response.Data.PageToken
		if len(response.Data.Items) == 0 || page == "" || seen[page] {
			return nil, ErrPersonalBlocksIncomplete
		}
		seen[page] = true
	}
	for _, block := range all {
		children := block.Children
		if block.Table != nil {
			if block.Table.Property == nil || block.Table.Property.ColumnSize < 1 || len(block.Table.Cells) == 0 ||
				len(block.Table.Cells)%block.Table.Property.ColumnSize != 0 {
				return nil, ErrPersonalBlocksIncomplete
			}
			children = append(append([]string{}, children...), block.Table.Cells...)
		}
		for _, child := range children {
			if !ids[child] {
				return nil, ErrPersonalBlocksIncomplete
			}
		}
	}
	if !personalBlocksAcyclic(all) {
		return nil, ErrPersonalBlocksIncomplete
	}
	return all, nil
}

func personalBlocksAcyclic(blocks []DocxBlock) bool {
	byID := map[string]DocxBlock{}
	for _, block := range blocks {
		byID[block.BlockID] = block
	}
	colors := map[string]int{}
	var visit func(string, int) bool
	visit = func(id string, depth int) bool {
		if depth > 128 || colors[id] == 1 {
			return false
		}
		if colors[id] == 2 {
			return true
		}
		colors[id] = 1
		block := byID[id]
		children := append([]string{}, block.Children...)
		if block.Table != nil {
			children = append(children, block.Table.Cells...)
		}
		for _, child := range children {
			if !visit(child, depth+1) {
				return false
			}
		}
		colors[id] = 2
		return true
	}
	for id := range byID {
		if !visit(id, 0) {
			return false
		}
	}
	return true
}

func personalBlockSupported(raw json.RawMessage, block DocxBlock) bool {
	switch block.BlockType {
	case BlockTypePage, BlockTypeText, BlockTypeBullet, BlockTypeOrdered, BlockTypeCode,
		BlockTypeQuote, BlockTypeTodo, BlockTypeCallout, BlockTypeDivider, BlockTypeTable, BlockTypeTableCell:
	default:
		if block.BlockType < BlockTypeHeading1 || block.BlockType > blockTypeHeading9 {
			return false
		}
	}
	if block.BlockType != BlockTypePage && block.BlockType != BlockTypeTable &&
		block.BlockType != BlockTypeTableCell && block.BlockType != BlockTypeDivider &&
		block.BlockType != BlockTypeCallout && textBearingField(block) == nil {
		return false
	}
	// The old converter has no safe asset/embedded-resource handoff and drops
	// unrecognized inline elements. Refuse those documents instead of indexing
	// placeholders or a partial body; successful exports remain unchanged.
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return false
	}
	for _, field := range object {
		var text struct {
			Elements []map[string]json.RawMessage `json:"elements"`
		}
		if json.Unmarshal(field, &text) != nil {
			continue
		}
		for _, element := range text.Elements {
			if len(element) != 1 || element["text_run"] == nil {
				return false
			}
			var run struct {
				Content *string `json:"content"`
				Style   struct {
					Link json.RawMessage `json:"link"`
				} `json:"text_element_style"`
			}
			if json.Unmarshal(element["text_run"], &run) != nil || run.Content == nil || len(run.Style.Link) != 0 {
				return false
			}
		}
	}
	return true
}
