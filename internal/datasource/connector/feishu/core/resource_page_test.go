package core

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/require"
)

func TestResourcePageBoundsAndExplicitRootToken(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1,localhost")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, "Bearer user-only", r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/open-apis/drive/explorer/v2/root_folder/meta":
			_, _ = io.WriteString(w, `{"code":0,"data":{"token":"root"}}`)
		case "/open-apis/drive/v1/files":
			require.Equal(t, "root", r.URL.Query().Get("folder_token"))
			require.Equal(t, "2", r.URL.Query().Get("page_size"))
			_, _ = io.WriteString(
				w,
				`{"code":0,"data":{"files":[{"token":"one","name":"one","type":"docx"}],"has_`+
					`more":true,"next_page_token":"next"}}`,
			)
		default:
			t.Error("unexpected endpoint")
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	client := NewClient(&Config{AppID: "cli_test", BaseURL: server.URL})
	ctx := WithUserIdentity(
		context.Background(),
		func(context.Context) (string, error) { return "user-only", nil },
	)
	page, err := client.ReadResourcePage(ctx, "drive", "", "", "", 2)
	require.NoError(t, err)
	require.True(t, page.HasMore)
	require.Equal(t, "next", page.NextPage)
	require.Len(t, page.Items, 1)
	require.EqualValues(t, 2, calls.Load())
	_, err = client.ReadResourcePage(ctx, "drive", "", "", "", 51)
	require.Error(t, err)
	_, err = client.ReadResourcePage(ctx, "drive", "../other", "", "", 2)
	require.Error(t, err)
	require.EqualValues(t, 2, calls.Load(), "invalid queries must not make HTTP requests")
}

func TestResourcePageRejectsBrokenPaginationWithoutFollowingIt(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1,localhost")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"code":0,"data":{"items":[],"has_more":true,"page_token":"same"}}`)
	}))
	defer server.Close()
	c := NewClient(&Config{AppID: "cli_test", BaseURL: server.URL})
	ctx := WithUserIdentity(
		context.Background(),
		func(context.Context) (string, error) { return "user", nil },
	)
	_, err := c.ReadResourcePage(ctx, "wiki", "", "", "same", 2)
	require.Error(t, err)
}
