package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPersonalDrivePageUserIdentityProjectionAndPagination(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/open-apis/drive/v1/files" || r.Header.Get("Authorization") != "Bearer personal-grant" || r.URL.Query().Get("page_size") != "50" {
			t.Error("not a bounded personal Drive request")
		}
		q := r.URL.Query()
		if calls == 1 && q.Has("folder_token") {
			t.Error("root should omit folder_token")
		}
		if calls == 2 && (q.Get("folder_token") != "folder_1" || q.Get("page_token") != "opaque+/=&") {
			t.Error("folder/cursor not encoded correctly")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
			"has_more": true, "next_page_token": "opaque+/=&", "files": []map[string]string{
				{"token": "doc_1", "name": "Synthetic note", "type": "docx", "url": "https://team.feishu.cn/docx/doc_1?from=share", "owner_id": "never_expose_owner"},
				{"token": "folder_1", "name": "Folder", "type": "folder"},
				{"token": "bad_1", "name": "Unsafe link", "type": "docx", "url": "https://attacker.invalid/docx/bad_1"},
			},
		}})
	}))
	defer server.Close()
	c := &Client{baseURL: server.URL, httpClient: server.Client()}
	ctx := WithUserIdentity(context.Background(), func(context.Context) (string, error) { return "personal-grant", nil })
	page, err := c.ListPersonalDrivePage(ctx, "", "")
	if err != nil || len(page.Items) != 3 || !page.HasMore || page.NextPage != "opaque+/=&" {
		t.Fatalf("root page: %v", err)
	}
	if !page.Items[0].Importable || page.Items[0].URL != "https://team.feishu.cn/docx/doc_1" || page.Items[2].Importable || page.Items[2].URL != "" {
		t.Fatal("unsafe or mismatched import selection")
	}
	b, _ := json.Marshal(page)
	if strings.Contains(string(b), "never_expose_owner") || strings.Contains(string(b), "personal-grant") {
		t.Fatal("credential/owner exposed")
	}
	// The server incorrectly repeating a continuation cursor must fail closed.
	if _, err := c.ListPersonalDrivePage(ctx, "folder_1", page.NextPage); !errors.Is(err, ErrPersonalDrivePage) {
		t.Fatalf("repeated cursor accepted: %v", err)
	}
	before := calls
	for _, input := range [][2]string{{"../folder", ""}, {"ok", strings.Repeat("x", 2049)}, {"ok", "cursor\nheader"}} {
		if _, err := c.ListPersonalDrivePage(ctx, input[0], input[1]); !errors.Is(err, ErrPersonalDrivePage) {
			t.Error("invalid browse input accepted")
		}
	}
	if _, err := c.ListPersonalDrivePage(context.Background(), "", ""); !errors.Is(err, ErrAuth) {
		t.Fatal("missing user identity accepted")
	}
	if calls != before {
		t.Fatal("invalid input or missing identity made a network call")
	}
}

func TestPersonalDrivePageDisconnectAndPermissionFailClosed(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":99991672,"msg":"permission denied"}`))
	}))
	defer server.Close()
	c := &Client{baseURL: server.URL, httpClient: server.Client()}
	for _, provider := range []func(context.Context) (string, error){nil, func(context.Context) (string, error) { return "", ErrAuth }} {
		if _, err := c.ListPersonalDrivePage(WithUserIdentity(context.Background(), provider), "", ""); err == nil {
			t.Fatal("disconnected account accepted")
		}
	}
	if calls != 0 {
		t.Fatal("disconnected user reached the provider or tenant auth")
	}
	if _, err := c.ListPersonalDrivePage(WithUserIdentity(context.Background(), func(context.Context) (string, error) { return "grant", nil }), "", ""); err == nil {
		t.Fatal("provider permission error returned a successful directory")
	}
}

func TestPersonalDrivePageMalformedResponseFailsClosed(t *testing.T) {
	for _, body := range []string{
		`{"code":0}`,
		`{"code":0,"data":{"files":[],"has_more":true}}`,
		`{"code":0,"data":{"files":[],"has_more":true,"next_page_token":"bad\n"}}`,
		`{"code":0,"data":{"files":[{"token":"../invalid"}],"has_more":false}}`,
		`{"code":0,"padding":"` + strings.Repeat("x", 2*1024*1024) + `","data":{"files":[]}}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		c := &Client{baseURL: server.URL, httpClient: server.Client()}
		ctx := WithUserIdentity(context.Background(), func(context.Context) (string, error) { return "grant", nil })
		page, err := c.ListPersonalDrivePage(ctx, "", "")
		server.Close()
		if err == nil || page != nil {
			t.Fatal("malformed/oversized response returned a directory")
		}
	}
}
