package core

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

var ErrApplicationIncompleteContent = errors.New("feishu_application_content_not_verified")

// ApplicationDocxText fails closed on unvalidated blocks, images, attachments
// and embeds. A raw text endpoint alone cannot prove a Docx export is complete.
func (c *Client) ApplicationDocxText(ctx context.Context, r PersonalResource) (string, error) {
	if err := requireApplicationIdentity(ctx); err != nil {
		return "", err
	}
	if r.Type != "docx" {
		return "", ErrUnsupportedPersonalResource
	}
	page := ""
	seen := map[string]bool{}
	count := 0
	for {
		var response struct {
			Data struct {
				Items []struct {
					BlockType int `json:"block_type"`
				} `json:"items"`
				HasMore   bool   `json:"has_more"`
				PageToken string `json:"page_token"`
			} `json:"data"`
		}
		path := "/open-apis/docx/v1/documents/" + r.Token + "/blocks?page_size=500"
		if page != "" {
			path += "&page_token=" + url.QueryEscape(page)
		}
		if err := c.doRequest(ctx, "application_docx_blocks", http.MethodGet, path, nil, &response); err != nil {
			return "", err
		}
		for _, b := range response.Data.Items {
			count++
			if !(b.BlockType >= 1 && b.BlockType <= 14 || b.BlockType == 22 || b.BlockType == 31 || b.BlockType == 32 || b.BlockType == 34) {
				return "", ErrApplicationIncompleteContent
			}
		}
		if count > 10000 {
			return "", ErrApplicationIncompleteContent
		}
		if !response.Data.HasMore {
			break
		}
		page = response.Data.PageToken
		if page == "" || seen[page] {
			return "", ErrApplicationIncompleteContent
		}
		seen[page] = true
	}
	if count == 0 {
		return "", ErrApplicationIncompleteContent
	}
	return c.getDocumentRawContent(ctx, r.Token)
}
