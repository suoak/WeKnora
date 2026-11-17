package confluence

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

func pagedTestConnector(t *testing.T, respond func(*http.Request) string) *Connector {
	t.Helper()
	return &Connector{newClient: func(cfg config) (*client, error) {
		return &client{
			cfg: cfg,
			http: &http.Client{Transport: roundTripper(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: 200,
					Header:     http.Header{},
					Body:       io.NopCloser(strings.NewReader(respond(req))),
				}, nil
			})},
		}, nil
	}}
}

func TestPickerFirstPageDoesNotEnumerateSite(t *testing.T) {
	calls := 0
	c := pagedTestConnector(t, func(req *http.Request) string {
		calls++
		if req.URL.Query().Get("limit") != "50" {
			t.Errorf("limit: %s", req.URL)
		}
		if req.URL.Query().Get("start") == "50" {
			return `{"results":[{"id":2,"key":"OPS","name":"Operations"}]}`
		}
		return `{"results":[{"id":1,"key":"ENG","name":"Engineering"}],` +
			`"_links":{"next":"/rest/api/space?limit=50&start=50"}}`
	})
	first, err := c.ListResourcePage(context.Background(), streamConfig(), types.ResourceListOptions{})
	if err != nil || calls != 1 || len(first.Items) != 1 || first.NextCursor == "" {
		t.Fatalf("first = %#v, calls=%d, err=%v", first, calls, err)
	}
	if _, err := c.ListResourcePage(context.Background(), streamConfig(), types.ResourceListOptions{}); err != nil ||
		calls != 1 {
		t.Fatalf("page cache missed: %v, %d", err, calls)
	}
	second, err := c.ListResourcePage(
		context.Background(),
		streamConfig(),
		types.ResourceListOptions{Cursor: first.NextCursor},
	)
	if err != nil || calls != 2 || second.Items[0].ExternalID != "2" || second.NextCursor != "" {
		t.Fatalf("second = %#v, calls=%d, err=%v", second, calls, err)
	}
	rotated := streamConfig()
	rotated.Credentials["password"] = "different"
	if _, err := c.ListResourcePage(context.Background(), rotated, types.ResourceListOptions{}); err != nil ||
		calls != 3 {
		t.Fatalf("credentials shared cache: %v, %d", err, calls)
	}
}

func TestPickerSearchQuotesCQLAndUsesSearchEntities(t *testing.T) {
	query := `test" OR type=page OR title~"`
	c := pagedTestConnector(t, func(req *http.Request) string {
		if req.URL.Path != "/wiki/rest/api/search" ||
			req.URL.Query().Get("cql") != `type=space AND title~"test\" OR type=page OR title~\""` {
			t.Errorf("unsafe search: %s", req.URL)
		}
		return `{"results":[{"space":{"id":12,"key":"TEST","name":"Test"}}]}`
	})
	page, err := c.ListResourcePage(context.Background(), streamConfig(), types.ResourceListOptions{Query: query})
	if err != nil || len(page.Items) != 1 || page.Items[0].ExternalID != "12" {
		t.Fatalf("page=%#v err=%v", page, err)
	}
}

func TestPickerRejectsBadCursorsBeforeNetwork(t *testing.T) {
	c := pagedTestConnector(t, func(_ *http.Request) string { t.Error("unexpected network request"); return "{}" })
	for _, cursor := range []string{
		"not-base64!",
		base64.RawURLEncoding.EncodeToString([]byte(`{"start":-1}`)),
		base64.RawURLEncoding.EncodeToString([]byte(`{"q":"other","start":50}`)),
	} {
		if _, err := c.ListResourcePage(
			context.Background(), streamConfig(), types.ResourceListOptions{Cursor: cursor},
		); err == nil {
			t.Errorf("accepted cursor %q", cursor)
		}
	}
}

func TestPickerRejectsForeignAndRepeatedContinuation(t *testing.T) {
	for _, next := range []string{
		"https://foreign.test/rest/api/space?start=50", "/rest/api/space?start=0", "/rest/api/space",
	} {
		t.Run(next, func(t *testing.T) {
			c := pagedTestConnector(t, func(_ *http.Request) string {
				return fmt.Sprintf(`{"results":[{"id":1,"key":"ENG"}],"_links":{"next":%q}}`, next)
			})
			if _, err := c.ListResourcePage(
				context.Background(), streamConfig(), types.ResourceListOptions{},
			); err == nil {
				t.Fatal("accepted invalid continuation")
			}
		})
	}
}

func TestPickerChildrenResolveOneSpaceWithoutEnumeration(t *testing.T) {
	calls := 0
	c := pagedTestConnector(t, func(req *http.Request) string {
		calls++
		switch req.URL.Path {
		case "/wiki/rest/api/space/ENG":
			return `{"id":1,"key":"ENG","name":"Engineering"}`
		case "/wiki/rest/api/space/ENG/content/page":
			return `{"results":[{"id":"123","title":"Page"}]}`
		default:
			t.Errorf("unexpected enumeration: %s", req.URL)
			return "{}"
		}
	})
	page, err := c.ListResourcePage(
		context.Background(),
		streamConfig(),
		types.ResourceListOptions{ParentID: "1", SpaceKey: "ENG"},
	)
	if err != nil || calls != 2 || len(page.Items) != 1 || page.Items[0].ParentID != "1" {
		t.Fatalf("page=%#v calls=%d err=%v", page, calls, err)
	}
	if _, err := c.ListResourcePage(
		context.Background(), streamConfig(), types.ResourceListOptions{ParentID: "2", SpaceKey: "ENG"},
	); err == nil {
		t.Fatal("accepted mismatched space")
	}
}

func TestPickerCoalescesRequestsAndCachesFromCompletion(t *testing.T) {
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	c := pagedTestConnector(t, func(_ *http.Request) string {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return `{"results":[{"id":1,"key":"ENG"}]}`
	})
	results := make(chan error, 2)
	go func() {
		_, err := c.ListResourcePage(context.Background(), streamConfig(), types.ResourceListOptions{})
		results <- err
	}()
	<-started
	go func() {
		_, err := c.ListResourcePage(context.Background(), streamConfig(), types.ResourceListOptions{})
		results <- err
	}()
	completedAfter := time.Now()
	close(release)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("requests=%d", calls.Load())
	}
	for _, entry := range c.pickerPages.entries {
		if entry.expires.Before(completedAfter.Add(pickerSpaceCacheTTL)) {
			t.Fatal("cache lifetime started before completion")
		}
	}
}

func TestPickerCloudCursorKeepsWikiContextAndSearchBinding(t *testing.T) {
	calls := 0
	c := pagedTestConnector(t, func(req *http.Request) string {
		calls++
		if req.URL.Path != "/wiki/api/v2/spaces" {
			t.Errorf("path=%s", req.URL.Path)
		}
		if calls == 1 {
			return `{"results":[{"id":"1","key":"ENG","name":"Engineering"}],` +
				`"_links":{"next":"/wiki/api/v2/spaces?cursor=opaque%2Btoken&limit=50"}}`
		}
		if req.URL.Query().Get("cursor") != "opaque+token" {
			t.Errorf("cursor=%s", req.URL.Query().Get("cursor"))
		}
		return `{"results":[{"id":"2","key":"OPS","name":"Operations"}]}`
	})
	ds := streamConfig()
	ds.Credentials["edition"] = "cloud"
	ds.Credentials["api_token"] = "cloud-token"
	first, err := c.ListResourcePage(context.Background(), ds, types.ResourceListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Items[0].Metadata["hierarchy_limitation"] != "cloud_top_level_containers" {
		t.Fatal("lost Cloud hierarchy hint")
	}
	if _, err := c.ListResourcePage(
		context.Background(), ds, types.ResourceListOptions{Cursor: first.NextCursor, Query: "different"},
	); err == nil {
		t.Fatal("cursor accepted for a different search")
	}
	if _, err := c.ListResourcePage(
		context.Background(), ds, types.ResourceListOptions{Cursor: first.NextCursor},
	); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestPickerCacheIsBoundedAcrossSearches(t *testing.T) {
	c := pagedTestConnector(t, func(_ *http.Request) string { return `{"results":[]}` })
	for i := range 260 {
		if _, err := c.ListResourcePage(
			context.Background(), streamConfig(), types.ResourceListOptions{Query: fmt.Sprintf("space %d", i)},
		); err != nil {
			t.Fatal(err)
		}
	}
	if len(c.pickerPages.entries) != 256 {
		t.Fatalf("cache size=%d", len(c.pickerPages.entries))
	}
}

func TestCanceledPickerDoesNotRequestUpstream(t *testing.T) {
	c := pagedTestConnector(
		t,
		func(_ *http.Request) string { t.Error("canceled request reached upstream"); return "{}" },
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.ListResourcePage(ctx, streamConfig(), types.ResourceListOptions{}); err != context.Canceled {
		t.Fatalf("err=%v", err)
	}
}

func TestPickerChildLookupRejectsPathTraversalBeforeNetwork(t *testing.T) {
	c := pagedTestConnector(t, func(_ *http.Request) string { t.Error("invalid path reached upstream"); return "{}" })
	for _, options := range []types.ResourceListOptions{
		{ParentID: "../other", SpaceKey: "ENG"},
		{ParentID: "1", SpaceKey: ".."},
		{ParentID: "1", SpaceKey: "ENG/../../other"},
		{ParentID: "page:1:../other", SpaceKey: "ENG"},
	} {
		if _, err := c.ListResourcePage(context.Background(), streamConfig(), options); err == nil {
			t.Fatalf("accepted %#v", options)
		}
	}
}

func TestPickerChildrenPaginateAndBindCursorToParent(t *testing.T) {
	calls := 0
	c := pagedTestConnector(t, func(req *http.Request) string {
		calls++
		if req.URL.Path == "/wiki/rest/api/space/ENG" {
			return `{"id":1,"key":"ENG","name":"Engineering"}`
		}
		q := req.URL.Query()
		if q.Get("depth") != "root" || q.Get("expand") != serverPageExpand || q.Get("limit") != "50" {
			t.Errorf("listing scope lost: %s", req.URL)
		}
		if q.Get("start") == "50" {
			return `{"results":[{"id":"20","title":"Second"}]}`
		}
		return `{"results":[{"id":"10","title":"First"}],"_links":{"next":"/rest/api/space/ENG/content/page?start=50"}}`
	})
	options := types.ResourceListOptions{ParentID: "1", SpaceKey: "ENG"}
	first, err := c.ListResourcePage(context.Background(), streamConfig(), options)
	if err != nil || calls != 2 || first.NextCursor == "" {
		t.Fatalf("first=%#v calls=%d err=%v", first, calls, err)
	}
	options.Cursor = first.NextCursor
	second, err := c.ListResourcePage(context.Background(), streamConfig(), options)
	if err != nil || calls != 4 || second.NextCursor != "" || second.Items[0].ExternalID != "page:1:20" {
		t.Fatalf("second=%#v calls=%d err=%v", second, calls, err)
	}
	options.ParentID = "2"
	if _, err := c.ListResourcePage(context.Background(), streamConfig(), options); err == nil || calls != 4 {
		t.Fatal("child cursor replayed for a different parent")
	}
}

func TestPickerRevealPageDoesNotEnumerateSpacesOrSiblings(t *testing.T) {
	calls := 0
	c := pagedTestConnector(t, func(req *http.Request) string {
		calls++
		switch req.URL.Path {
		case "/wiki/rest/api/content/30":
			return `{"id":"30","title":"Selected","space":{"key":"ENG"},` +
				`"ancestors":[{"id":"10","title":"Root"},{"id":"20","title":"Parent"}]}`
		case "/wiki/rest/api/space/ENG":
			return `{"id":1,"key":"ENG","name":"Engineering"}`
		default:
			t.Errorf("unexpected full listing: %s", req.URL)
			return `{}`
		}
	})
	result, err := c.ListResourcePage(
		context.Background(),
		streamConfig(),
		types.ResourceListOptions{RevealID: "page:1:30"},
	)
	if err != nil || calls != 2 || len(result.Items) != 4 {
		t.Fatalf("result=%#v calls=%d err=%v", result, calls, err)
	}
	if result.Items[2].ParentID != "page:1:10" || result.Items[3].ParentID != "page:1:20" {
		t.Fatalf("incorrect path: %#v", result.Items)
	}
	if _, err := c.ListResourcePage(
		context.Background(), streamConfig(), types.ResourceListOptions{RevealID: "page:2:30"},
	); err == nil {
		t.Fatal("revealed a page under a forged space")
	}
}
