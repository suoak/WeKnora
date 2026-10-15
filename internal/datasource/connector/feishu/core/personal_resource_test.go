package core

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

func TestPersonalResourceURLMatrix(t *testing.T) {
	for path, kind := range map[string]string{"docx": "docx", "docs": "doc", "sheets": "sheet", "base": "bitable", "wiki": "wiki", "file": "file", "slides": "slides", "mindnotes": "mindnote"} {
		r, err := ParsePersonalResourceURL("https://team.feishu.cn/" + path + "/token_123?from=share#part")
		if err != nil || r.Type != kind || r.Token != "token_123" || r.URL != "https://team.feishu.cn/"+path+"/token_123" {
			t.Fatalf("%s: %+v %v", path, r, err)
		}
	}
	for _, bad := range []string{"http://x.feishu.cn/docx/t", "https://feishu.cn.evil.org/docx/t", "https://x.feishu.cn:443/docx/t", "https://me@x.feishu.cn/docx/t", "https://x.feishu.cn/docx/%2e%2e", "https://x.feishu.cn/drive/folder/t", "https://x.feishu.cn/wiki/space/x", "https://x.feishu.cn/docx/t/extra"} {
		if _, err := ParsePersonalResourceURL(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestPersonalIdentityConcurrentAndNoTenantFallback(t *testing.T) {
	var tenantCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal" {
			tenantCalls.Add(1)
			t.Error("personal request reached tenant auth")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"identity":"`+r.Header.Get("Authorization")+`"}`)
	}))
	defer server.Close()
	client := NewClient(&Config{AppID: "shared_personal_test", BaseURL: server.URL})
	var wg sync.WaitGroup
	for _, user := range []string{"A", "B"} {
		wg.Add(1)
		go func(user string) {
			defer wg.Done()
			ctx := WithUserIdentity(context.Background(), func(context.Context) (string, error) { return "user-" + user, nil })
			var response struct {
				Identity string `json:"identity"`
			}
			if err := client.DoRequest(ctx, "GET", "/target", nil, &response); err != nil || response.Identity != "Bearer user-"+user {
				t.Errorf("cross-user identity: %+v %v", response, err)
			}
		}(user)
	}
	wg.Wait()
	for _, provider := range []func(context.Context) (string, error){nil, func(context.Context) (string, error) { return "", nil }, func(context.Context) (string, error) { return "", ErrAuth }} {
		if err := client.DoRequest(WithUserIdentity(context.Background(), provider), "GET", "/target", nil, nil); err == nil {
			t.Error("invalid personal grant did not fail")
		}
	}
	if tenantCalls.Load() != 0 {
		t.Fatal("tenant fallback occurred")
	}
}

func TestPersonalIdentityRevokedDuringRetry(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(429)
		_, _ = io.WriteString(w, `{"code":99991400}`)
	}))
	defer server.Close()
	client := NewClient(&Config{AppID: "revoke_test", BaseURL: server.URL})
	var resolved int
	ctx := WithUserIdentity(context.Background(), func(context.Context) (string, error) {
		resolved++
		if resolved > 1 {
			return "", ErrAuth
		}
		return "user-A", nil
	})
	err := client.DoRequest(ctx, "GET", "/target", nil, nil)
	if !errors.Is(err, ErrAuth) || calls.Load() != 1 {
		t.Fatalf("revocation retry: calls=%d err=%v", calls.Load(), err)
	}
}

func TestPersonalExportAndMediaUseUserIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer user-A" {
			t.Errorf("wrong identity for %s", r.URL.Path)
		}
		switch r.URL.Path {
		case "/open-apis/drive/v1/export_tasks":
			_, _ = io.WriteString(w, `{"code":0,"data":{"ticket":"ticket"}}`)
		case "/open-apis/drive/v1/export_tasks/ticket":
			_, _ = io.WriteString(w, `{"code":0,"data":{"result":{"job_status":0,"file_token":"out","file_name":"test.docx"}}}`)
		default:
			_, _ = io.WriteString(w, "binary")
		}
	}))
	defer server.Close()
	client := NewClient(&Config{AppID: "export_user_test", BaseURL: server.URL})
	ctx := WithUserIdentity(context.Background(), func(context.Context) (string, error) { return "user-A", nil })
	if _, _, err := client.ExportAndDownload(ctx, "doc", "docx"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.downloadMediaFile(ctx, "media"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.DownloadDriveFile(ctx, "file"); err != nil {
		t.Fatal(err)
	}
}

func testPersonalWorkbook(t *testing.T, sheets string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, err := z.Create("xl/workbook.xml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(w, `<workbook><sheets>`+sheets+`</sheets></workbook>`)
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestPersonalDownloadsRejectEmptyLoginAndInvalidOffice(t *testing.T) {
	for _, data := range [][]byte{nil, []byte(" "), []byte("<!DOCTYPE html><html>login</html>"), []byte(`{"code":99991663,"msg":"invalid token"}`), []byte("PKbad")} {
		if err := validatePersonalBytes(data, "test.docx"); err == nil {
			t.Fatalf("accepted invalid document %q", data)
		}
	}
	if err := validatePersonalBytes(testPersonalWorkbook(t, `<sheet name="first"/>`), "test.xlsx"); err != nil {
		t.Fatal(err)
	}
}

func TestPersonalWorkbookMissingSheetFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"code":0,"data":{"sheets":[{"title":"one"},{"title":"two"}]}}`)
	}))
	defer server.Close()
	c := NewClient(&Config{AppID: "coverage", BaseURL: server.URL})
	ctx := WithUserIdentity(context.Background(), func(context.Context) (string, error) { return "user", nil })
	resource := PersonalResource{Type: "sheet", Token: "token"}
	if _, err := c.ValidateWorkbookCoverage(ctx, resource, testPersonalWorkbook(t, `<sheet name="one"/>`)); err == nil {
		t.Fatal("missing sheet accepted")
	}
	if count, err := c.ValidateWorkbookCoverage(ctx, resource, testPersonalWorkbook(t, `<sheet name="one"/><sheet name="two"/>`)); err != nil || count != 2 {
		t.Fatalf("full workbook: %d %v", count, err)
	}
}

func TestPersonalWikiResolvesToSameStableID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/open-apis/wiki/v2/spaces/get_node" {
			_, _ = io.WriteString(w, `{"code":0,"data":{"node":{"obj_type":"docx","obj_token":"real","title":"Title"}}}`)
			return
		}
		_, _ = io.WriteString(w, `{"code":0,"data":{"metas":[{"doc_type":"docx","doc_token":"real","title":"Title"}]}}`)
	}))
	defer server.Close()
	c := NewClient(&Config{AppID: "wiki_identity", BaseURL: server.URL})
	ctx := WithUserIdentity(context.Background(), func(context.Context) (string, error) { return "user", nil })
	r, err := c.ResolvePersonalResource(ctx, PersonalResource{Type: "wiki", Token: "node"})
	if err != nil {
		t.Fatal(err)
	}
	if PersonalResourceID(r) != PersonalResourceID(PersonalResource{Type: "docx", Token: "real"}) {
		t.Fatal("wiki and docx have different stable identities")
	}
}
