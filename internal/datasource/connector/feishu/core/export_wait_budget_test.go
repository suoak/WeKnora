package core

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestLargeExportKeepsTicketBeyondOldPollBudget(t *testing.T) {
	t.Setenv("FEISHU_EXPORT_MAX_POLLS", "")
	t.Setenv("FEISHU_EXPORT_POLL_INTERVAL", "1ms")
	t.Setenv("FEISHU_API_REQUESTS_PER_MINUTE", "60000000")
	var creates, polls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-user" {
			t.Error("export lost authenticated identity")
		}
		switch r.URL.Path {
		case "/open-apis/drive/v1/export_tasks":
			creates.Add(1)
			_, _ = io.WriteString(w, `{"code":0,"data":{"ticket":"one-ticket"}}`)
		case "/open-apis/drive/v1/export_tasks/one-ticket":
			if polls.Add(1) <= 33 {
				_, _ = io.WriteString(w, `{"code":0,"data":{"result":{"job_status":2}}}`)
			} else {
				_, _ = io.WriteString(w, `{"code":0,"data":{"result":{"job_status":0,`+
					`"file_token":"ready-file","file_name":"test.xlsx"}}}`)
			}
		case "/open-apis/drive/v1/export_tasks/file/ready-file/download":
			_, _ = io.WriteString(w, "synthetic-export")
		default:
			t.Error("unexpected request")
		}
	}))
	defer server.Close()
	c := NewClient(&Config{AppID: "large-export-wait", BaseURL: server.URL})
	ctx := WithUserIdentity(context.Background(), func(context.Context) (string, error) { return "test-user", nil })
	body, name, err := c.ExportAndDownload(ctx, "resource", "bitable")
	if err != nil || name != "test.xlsx" || string(body) != "synthetic-export" ||
		creates.Load() != 1 || polls.Load() != 34 {
		t.Fatalf("large export: creates=%d polls=%d name=%q err=%v", creates.Load(), polls.Load(), name, err)
	}
}

func TestLargeExportWaitRemainsCancellable(t *testing.T) {
	t.Setenv("FEISHU_EXPORT_MAX_POLLS", "")
	t.Setenv("FEISHU_EXPORT_POLL_INTERVAL", "1ms")
	t.Setenv("FEISHU_API_REQUESTS_PER_MINUTE", "60000000")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var creates, polls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			creates.Add(1)
			_, _ = io.WriteString(w, `{"code":0,"data":{"ticket":"one-ticket"}}`)
			return
		}
		if polls.Add(1) == 2 {
			cancel()
		}
		_, _ = io.WriteString(w, `{"code":0,"data":{"result":{"job_status":2}}}`)
	}))
	defer server.Close()
	c := NewClient(&Config{AppID: "cancel-export-wait", BaseURL: server.URL})
	ctx = WithUserIdentity(ctx, func(context.Context) (string, error) { return "test-user", nil })
	_, _, err := c.ExportAndDownload(ctx, "resource", "bitable")
	if !errors.Is(err, context.Canceled) || creates.Load() != 1 || polls.Load() != 2 {
		t.Fatalf("cancelled export: creates=%d polls=%d err=%v", creates.Load(), polls.Load(), err)
	}
}

func TestExportPollBudgetIsTimeoutNotServerFailure(t *testing.T) {
	err := &APIError{Category: ErrorCategoryTransient, Operation: "export_poll_budget_exhausted"}
	code, value, _ := feishuFailure(err)
	if code != "feishu_timeout" || value != "" {
		t.Fatalf("incorrect export classification: %s %s", code, value)
	}
}
