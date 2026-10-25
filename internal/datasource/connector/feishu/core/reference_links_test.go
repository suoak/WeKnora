package core

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/require"
)

func TestReferenceSourceRevisionAndIdentityNeverFallBack(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1,localhost")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	var revision atomic.Int64
	revision.Store(1)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		require.Equal(t, "Bearer owned-user", r.Header.Get("Authorization"))
		require.Equal(t, "/open-apis/drive/v1/metas/batch_query", r.URL.Path)
		fmt.Fprintf(w, `{"code":0,"data":{"metas":[{"doc_type":"file","doc_token":"real","title":"controlled.pdf","latest_modify_time":"%d"}]}}`, revision.Load())
	}))
	defer server.Close()
	client := NewClient(&Config{AppID: "synthetic-app", AppSecret: "must-not-be-used", BaseURL: server.URL})
	ctx := WithUserIdentity(context.Background(), func(context.Context) (string, error) { return "owned-user", nil })
	resource, err := ParsePersonalResourceURL("https://test.feishu.cn/file/real")
	require.NoError(t, err)
	expected, err := client.ResolveBoundResource(ctx, resource)
	require.NoError(t, err)
	bound := WithExpectedLink(ctx, expected)
	require.Equal(t, "file:real", PersonalResourceID(expected))
	_, err = client.ResolveBoundResource(bound, resource)
	require.NoError(t, err)
	revision.Store(2)
	_, err = client.ResolveBoundResource(bound, resource)
	require.ErrorIs(t, err, ErrTransient)
	before := requests.Load()
	_, err = client.ResolveBoundResource(context.Background(), resource)
	require.Error(t, err)
	require.Equal(t, before, requests.Load(), "an absent user identity must not call a tenant-token endpoint")
}

func TestReferenceLinksBoundedConfigAndCursorRoundTrip(t *testing.T) {
	ref := types.FeishuSourceReference{Version: 1, OwnerUserID: "A", Kind: types.FeishuUserOAuth, ConnectionID: "opaque", AppID: "app", IdentityVersion: "version"}
	config := &types.DataSourceConfig{Settings: map[string]interface{}{"credential_reference": ref, "links": []string{"https://test.feishu.cn/file/real"}}}
	links, reference, err := ReferenceLinks(config)
	require.NoError(t, err)
	require.True(t, reference)
	require.Len(t, links, 1)
	config.Settings["links"] = []string{}
	_, _, err = ReferenceLinks(config)
	require.Error(t, err)
	config.Settings["links"] = make([]string, 101)
	_, _, err = ReferenceLinks(config)
	require.Error(t, err)
	config.Settings["credential_reference"] = map[string]interface{}{"version": 1, "owner_user_id": "B"}
	_, _, err = ReferenceLinks(config)
	require.Error(t, err)
	ops := ReferenceLinkOps{}
	times := map[string]map[string]string{"links": {"file:real": "100"}}
	cursor := ops.EncodeCursor(times, time.Now())
	require.Equal(t, times, ops.DecodeCursorTimes(cursor.ConnectorCursor))
}
