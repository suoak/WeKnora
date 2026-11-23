package core

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/types"
)

var errSheetRevisionUnavailable = errors.New("immutable spreadsheet revision unavailable")

// Only a live connector can issue this proof. It binds the export bytes, source
// identity and immutable sheet revision to the same authenticated client.
type sheetExportRevisionProof struct {
	clientID [32]byte
	token    string
	revision int64
	binding  [32]byte
	content  [32]byte
}

func sheetSnapshotBinding(item *types.FetchedItem) [32]byte {
	v := struct {
		ID, Title, Name, URL, Root, Type, Token, Node, Space, Modified string
	}{
		item.ExternalID, item.Title, item.FileName, item.URL, item.SourceResourceID,
		item.Metadata["obj_type"], item.Metadata["obj_token"], item.Metadata["node_token"],
		item.Metadata["space_id"], item.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	raw, _ := json.Marshal(v)
	return sha256.Sum256(raw)
}

func (p *sheetExportRevisionProof) matches(item *types.FetchedItem) bool {
	return p != nil && p.clientID != ([32]byte{}) && p.revision > 0 && item != nil &&
		item.Metadata["obj_type"] == "sheet" && item.Metadata["obj_token"] == p.token &&
		item.Metadata["feishu_sheet_revision"] == strconv.FormatInt(p.revision, 10) &&
		!item.IsDeleted && item.Metadata["attachment"] != "true" &&
		p.binding == sheetSnapshotBinding(item) && p.content == sha256.Sum256(item.Content)
}

// SameImmutableSheetRevision cannot be satisfied by a revision string copied
// into JSON metadata, or by transplanting a proof to different content.
func SameImmutableSheetRevision(original, current *types.FetchedItem) bool {
	if original == nil || current == nil {
		return false
	}
	left, ok := original.ExportSnapshotProof.(*sheetExportRevisionProof)
	right, otherOK := current.ExportSnapshotProof.(*sheetExportRevisionProof)
	return ok && otherOK && left.matches(original) && right.matches(current) &&
		left.clientID == right.clientID && left.token == right.token && left.revision == right.revision
}

func (c *Client) sheetSnapshotRevision(ctx context.Context, token string) (int64, error) {
	if !resourceTokenPattern.MatchString(token) {
		return 0, errSheetRevisionUnavailable
	}
	var response struct {
		Data struct {
			Properties struct {
				Revision *int64 `json:"revision"`
			} `json:"properties"`
		} `json:"data"`
	}
	path := "/open-apis/sheets/v2/spreadsheets/" + url.PathEscape(token) + "/metainfo"
	if err := c.doRequest(ctx, "sheet_snapshot_revision", http.MethodGet, path, nil, &response); err != nil {
		return 0, err
	}
	if response.Data.Properties.Revision == nil || *response.Data.Properties.Revision <= 0 {
		return 0, errSheetRevisionUnavailable
	}
	return *response.Data.Properties.Revision, nil
}

func optionalSheetRevisionUnavailable(err error) bool {
	var api *APIError
	return errors.Is(err, errSheetRevisionUnavailable) || errors.Is(err, ErrAppScope) ||
		errors.Is(err, ErrUserScope) || errors.Is(err, ErrPermission) ||
		(errors.As(err, &api) && api.HTTPStatus == http.StatusNotFound)
}

// WithSheetSnapshotProof strengthens the latest-content path without changing
// permissions required by existing exports. Unsupported metadata keeps the old
// complete-package comparison; it never permits nonce normalization.
func (c *Client) WithSheetSnapshotProof(
	ctx context.Context, token, kind string, fetch func() (*types.FetchedItem, error),
) (*types.FetchedItem, error) {
	_, personal := ctx.Value(userIdentityKey{}).(userIdentity)
	if kind != "sheet" || personal || os.Getenv("KNOWHUB_FEISHU_LATEST_CONTENT") != "true" {
		return fetch()
	}
	before, err := c.sheetSnapshotRevision(ctx, token)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if optionalSheetRevisionUnavailable(err) {
			return fetch()
		}
		return nil, err
	}
	item, err := fetch()
	if err != nil || item == nil {
		return item, err
	}
	after, err := c.sheetSnapshotRevision(ctx, token)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if optionalSheetRevisionUnavailable(err) {
			return item, nil
		}
		return nil, err
	}
	if before != after {
		return nil, &datasource.DocumentFailureError{Code: "source_snapshot_changed"}
	}
	if item.Metadata == nil || item.Metadata["obj_type"] != "sheet" || item.Metadata["obj_token"] != token ||
		len(item.Content) == 0 || item.ExternalID == "" || item.SourceResourceID == "" || item.UpdatedAt.IsZero() {
		return nil, errSheetRevisionUnavailable
	}
	item.Metadata["feishu_sheet_revision"] = strconv.FormatInt(after, 10)
	// Retain no credential-bearing client pointer in a proof that might be
	// inspected by diagnostic formatting. Only its cryptographic scope is bound.
	scope, _ := json.Marshal([]string{c.baseURL, c.appID, c.appSecret})
	item.ExportSnapshotProof = &sheetExportRevisionProof{
		clientID: sha256.Sum256(scope), token: token, revision: after,
		binding: sheetSnapshotBinding(item), content: sha256.Sum256(item.Content),
	}
	return item, nil
}
