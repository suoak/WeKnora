package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

type expectedLinkKey struct{}

// WithExpectedLink binds subsequent reads to one canonical resource revision.
func WithExpectedLink(ctx context.Context, r PersonalResource) context.Context {
	return context.WithValue(ctx, expectedLinkKey{}, r)
}

// ReferenceLinks validates an optional bounded list using a credential reference.
func ReferenceLinks(config *types.DataSourceConfig) ([]string, bool, error) {
	ref, err := types.ParseFeishuSourceReference(config)
	if err != nil {
		return nil, false, err
	}
	if ref == nil {
		return nil, false, nil
	}
	raw, ok := config.Settings["links"]
	if !ok {
		return nil, false, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, true, err
	}
	var links []string
	if json.Unmarshal(b, &links) != nil || len(links) < 1 || len(links) > 100 {
		return nil, true, errors.New("invalid_feishu_reference_links")
	}
	return links, true, nil
}

// ResolveBoundResource uses only the bound identity and checks the expected revision.
func (c *Client) ResolveBoundResource(ctx context.Context, r PersonalResource) (PersonalResource, error) {
	var resolved PersonalResource
	var err error
	if _, ok := ctx.Value(applicationIdentityKey{}).(applicationIdentity); ok {
		resolved, err = c.ResolveApplicationResource(ctx, r)
	} else {
		resolved, err = c.ResolvePersonalResource(ctx, r)
	}
	if err != nil {
		return resolved, err
	}
	if expected, ok := ctx.Value(expectedLinkKey{}).(PersonalResource); ok &&
		(resolved.Type != expected.Type || resolved.Token != expected.Token || resolved.Modified != expected.Modified) {
		return resolved, ErrTransient
	}
	return resolved, nil
}

// ReferenceLinkOps adapts a bounded explicit list to the existing cursor,
// checkpoint and safe publication engine. It owns no index or publishing code.
type ReferenceLinkOps struct{ Links []string }

// List resolves and deduplicates explicitly selected canonical objects.
func (o ReferenceLinkOps) List(ctx context.Context, c *Client, _ string) ([]PersonalResource, error, error) {
	nodes := []PersonalResource{}
	seen := map[string]bool{}
	for _, link := range o.Links {
		r, err := ParsePersonalResourceURL(link)
		if err != nil {
			return nil, nil, err
		}
		r, err = c.ResolveBoundResource(ctx, r)
		if err != nil {
			return nil, nil, err
		}
		key := PersonalResourceID(r)
		if !seen[key] {
			nodes = append(nodes, r)
			seen[key] = true
		}
	}
	return nodes, nil, nil
}

// Token returns the canonical object key used by the existing sync engine.
func (ReferenceLinkOps) Token(r PersonalResource) string { return PersonalResourceID(r) }

// Title returns the source title.
func (ReferenceLinkOps) Title(r PersonalResource) string { return r.Title }

// ObjType returns the resolved source type.
func (ReferenceLinkOps) ObjType(r PersonalResource) string { return r.Type }

// EditTime returns the resolved source revision.
func (ReferenceLinkOps) EditTime(r PersonalResource) string { return r.Modified }

// Fetch exports through the bound identity and rechecks the source revision.
func (ReferenceLinkOps) Fetch(
	ctx context.Context,
	c *Client,
	r PersonalResource,
	root string,
	_ bool,
) ([]*types.FetchedItem, error) {
	var data []byte
	var name string
	var err error
	if _, ok := ctx.Value(applicationIdentityKey{}).(applicationIdentity); ok {
		data, name, err = c.DownloadApplicationResource(ctx, r)
	} else {
		data, name, err = c.DownloadPersonalResource(ctx, r)
		if err == nil && (r.Type == "sheet" || r.Type == "bitable") {
			_, err = c.ValidateWorkbookCoverage(ctx, r, data)
		}
	}
	if err != nil {
		return nil, err
	}
	check, err := c.ResolveBoundResource(ctx, r)
	if err != nil {
		return nil, err
	}
	if check.Modified != r.Modified {
		return nil, ErrTransient
	}
	modified := ParseFeishuTimestamp(r.Modified)
	if modified.IsZero() {
		return nil, fmt.Errorf("source_revision_required")
	}
	return []*types.FetchedItem{
		{
			ExternalID:       PersonalResourceID(r),
			Title:            r.Title,
			URL:              r.URL,
			Content:          data,
			FileName:         name,
			UpdatedAt:        modified,
			SourceResourceID: root,
			Metadata: map[string]string{
				"obj_type":           r.Type,
				"obj_token":          r.Token,
				"source_url":         r.URL,
				"feishu_resource_id": PersonalResourceID(r),
			},
		},
	}, nil
}

// ListFailureItems leaves reference-list failures to the source-level retry policy.
func (ReferenceLinkOps) ListFailureItems(string, error) []types.FetchedItem { return nil }

// ResourceNoun names the source's items for progress messages.
func (ReferenceLinkOps) ResourceNoun() string { return "documents" }

// EmptyResourceIDsError describes an empty reference selection.
func (ReferenceLinkOps) EmptyResourceIDsError() string { return "no reference links selected" }

// LogTag identifies reference connector diagnostics.
func (ReferenceLinkOps) LogTag() string { return "[FeishuReference]" }

// DecodeCursorTimes reads per-object revisions from the existing cursor envelope.
func (ReferenceLinkOps) DecodeCursorTimes(m map[string]interface{}) map[string]map[string]string {
	var out struct {
		Times map[string]map[string]string `json:"reference_times"`
	}
	b, _ := json.Marshal(m)
	_ = json.Unmarshal(b, &out)
	return out.Times
}

// EncodeCursor stores per-object revisions without modifying another source's cursor.
func (ReferenceLinkOps) EncodeCursor(times map[string]map[string]string, t time.Time) *types.SyncCursor {
	return &types.SyncCursor{
		LastSyncTime:    t,
		ConnectorCursor: map[string]interface{}{"reference_times": times},
	}
}
