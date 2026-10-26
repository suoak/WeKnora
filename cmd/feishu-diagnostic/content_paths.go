package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
)

// This bounded capability probe is not an import route. It returns no source
// content and cannot publish or certify complete conversion of a rich document.
func comparePersonalContentPaths(ctx context.Context, client *core.Client, r core.PersonalResource) map[string]any {
	out := map[string]any{"knowledge_writes": false, "complete_import_proven": false}
	permissions, err := client.InspectPersonalPermissions(ctx, r)
	if err != nil || !permissions.View || !permissions.Copy || !permissions.Export || r.Type != "docx" {
		out["status"] = "SOURCE_PERMISSION_NOT_APPROVED"
		return out
	}
	out["effective_permissions"] = permissions
	formats := []map[string]any{}
	for _, format := range []string{"xml", "markdown"} {
		started := time.Now()
		var response struct {
			Data struct {
				Document struct {
					Content  string          `json:"content"`
					Revision json.RawMessage `json:"revision_id"`
				} `json:"document"`
			} `json:"data"`
		}
		err := client.DoRequest(ctx, http.MethodPost,
			"/open-apis/docs_ai/v1/documents/"+url.PathEscape(r.Token)+"/fetch",
			map[string]any{
				"format":      format,
				"extra_param": `{"enable_user_cite_reference_map":true,"return_html5_block_data":true}`,
				"export_option": map[string]bool{
					"export_block_id": true, "export_style_attrs": true, "export_cite_extra_data": true,
				},
			}, &response)
		entry := map[string]any{"format": format, "duration_ms": time.Since(started).Milliseconds()}
		if err != nil {
			entry["error"] = safeError(err)
		} else {
			body := response.Data.Document.Content
			entry["nonempty"] = strings.TrimSpace(body) != ""
			entry["content_bytes"], entry["content_digest"] = len(body), mask(body)
			entry["revision_present"] = len(response.Data.Document.Revision) != 0
			entry["has_image_markup"] = strings.Contains(body, "<image") || strings.Contains(body, "![")
			entry["has_table_markup"] = strings.Contains(body, "<table") || strings.Contains(body, "| ---")
			entry["has_attachment_markup"] = strings.Contains(body, "<file") || strings.Contains(body, "<attachment")
		}
		formats = append(formats, entry)
	}
	out["docs_ai_fetch"] = formats
	page, count := "", 0
	seen := map[string]bool{}
	types := map[int]int{}
	for {
		var response struct {
			Data struct {
				Items []struct {
					Type int `json:"block_type"`
				} `json:"items"`
				HasMore bool   `json:"has_more"`
				Page    string `json:"page_token"`
			} `json:"data"`
		}
		err := client.DoRequest(ctx, http.MethodGet,
			"/open-apis/docx/v1/documents/"+url.PathEscape(r.Token)+
				"/blocks?page_size=500&document_revision_id=-1&page_token="+url.QueryEscape(page), nil, &response)
		if err != nil {
			out["blocks_error"] = safeError(err)
			break
		}
		for _, item := range response.Data.Items {
			types[item.Type]++
		}
		count += len(response.Data.Items)
		if count > 50000 {
			out["blocks_error"] = "block_limit"
			break
		}
		if !response.Data.HasMore {
			out["blocks_listing_terminal"] = true
			break
		}
		page = response.Data.Page
		if page == "" || seen[page] || len(response.Data.Items) == 0 {
			out["blocks_error"] = "incomplete_pagination"
			break
		}
		seen[page] = true
	}
	out["block_count"], out["block_types"] = count, types
	current, revisionErr := client.ResolveBoundResource(ctx, r)
	after, permissionErr := client.InspectPersonalPermissions(ctx, r)
	out["source_revision_stable"] = revisionErr == nil && current.Modified == r.Modified
	out["permissions_still_allowed"] = permissionErr == nil && after.View && after.Copy && after.Export
	out["status"] = "CAPABILITY_PROBED_NOT_PUBLISHED"
	return out
}
