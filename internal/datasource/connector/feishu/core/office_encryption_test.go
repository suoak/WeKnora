package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/officeguard"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestOfficeAuthorizedDownloadRetainsTypedRefusal(t *testing.T) {
	encrypted, err := os.ReadFile("../../../../officeguard/testdata/encrypted.docx")
	require.NoError(t, err)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(t, "Bearer synthetic-office-user", r.Header.Get("Authorization"))
		require.Equal(t, "/open-apis/drive/v1/files/office/download", r.URL.Path)
		_, _ = w.Write(encrypted)
	}))
	defer server.Close()
	client := NewClient(&Config{AppID: "office-test", BaseURL: server.URL})
	ctx := WithUserIdentity(
		context.Background(),
		func(context.Context) (string, error) { return "synthetic-office-user", nil },
	)
	for _, name := range []string{"password.docx", "password.xlsx", "password.pptx", "disguised.pdf"} {
		_, _, err = client.DownloadPersonalResource(ctx, PersonalResource{Type: "file", Token: "office", Title: name})
		require.True(t, officeguard.IsEncryptedError(err), "actual verdict: %v", err)
		meta := FeishuErrorItemMeta(err, map[string]string{"failure_stage": "fetch_document"})
		require.Equal(t, "office_encrypted", meta["error_reason_code"])
		require.Equal(t, "2400", meta["error_reason_code_value"])
		require.Equal(t, "content_refused", meta["feishu_error_category"])
	}
	require.Equal(t, 4, calls, "no fallback, tenant identity or second download")
}

type officeRejectedOps struct{ deferralTestOps }

func (o *officeRejectedOps) Fetch(
	ctx context.Context,
	c *Client,
	node, root string,
	b bool,
) ([]*types.FetchedItem, error) {
	if node == "bad" {
		return nil, officeguard.NewEncryptedError()
	}
	return o.deferralTestOps.Fetch(ctx, c, node, root, b)
}

type officeRejectedHandler struct {
	deferralTestHandler
	refused []types.FetchedItem
}

func (h *officeRejectedHandler) Emit(ctx context.Context, item types.FetchedItem) error {
	if item.Metadata["error_reason_code"] == "office_encrypted" {
		h.refused = append(h.refused, item)
		return &datasource.DocumentFailureError{Code: "office_encrypted", Cause: officeguard.NewEncryptedError()}
	}
	return h.deferralTestHandler.Emit(ctx, item)
}

func TestOfficeRefusalKeepsCursorIdentityAndRetriesWithoutTombstone(t *testing.T) {
	ops := &officeRejectedOps{deferralTestOps{nodes: []string{"bad", "good"}}}
	h := &officeRejectedHandler{}
	initial := ops.EncodeCursor(map[string]map[string]string{"root": {"bad": "old"}}, time.Now())
	next, err := runSync(context.Background(), &Client{}, &types.DataSourceConfig{}, []string{"root"}, initial, h, ops)
	require.NoError(t, err)
	require.Len(t, h.refused, 1)
	require.False(t, h.refused[0].IsDeleted)
	require.Len(t, h.items, 1)
	require.Equal(t, "good", h.items[0].ExternalID)
	require.Equal(t, "old", ops.DecodeCursorTimes(next.ConnectorCursor)["root"]["bad"])
	require.Contains(t, next.ConnectorCursor, failedDocumentCursorKey)
	normal := &deferralTestOps{nodes: []string{"bad", "good"}}
	completed, err := runSync(
		context.Background(),
		&Client{},
		&types.DataSourceConfig{},
		[]string{"root"},
		next,
		&deferralTestHandler{},
		normal,
	)
	require.NoError(t, err)
	require.Equal(t, []string{"bad"}, normal.fetched)
	require.NotContains(t, completed.ConnectorCursor, failedDocumentCursorKey)
}
