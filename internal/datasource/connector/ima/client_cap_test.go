package ima

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/logger"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

type a4BrokenJSONReader struct{}

func (a4BrokenJSONReader) Read(p []byte) (int, error) {
	copy(p, "partial")
	return 7, io.ErrUnexpectedEOF
}
func TestA4JSONCapReader(t *testing.T) {
	for _, n := range []int64{maxJSONResponseBytes - 1, maxJSONResponseBytes, maxJSONResponseBytes + 1} {
		data, err := readCappedJSON(bytes.NewReader(bytes.Repeat([]byte("x"), int(n))))
		if n > maxJSONResponseBytes {
			if data != nil || !errors.Is(err, ErrResponseTooLarge) {
				t.Fatal("overflow returned partial JSON")
			}
		} else if err != nil || int64(len(data)) != n {
			t.Fatal("legal boundary rejected")
		}
	}
	if data, err := readCappedJSON(a4BrokenJSONReader{}); data != nil || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal("interruption returned partial JSON")
	}
}
func TestA4JSONCapHTTPAndSafeProviderErrors(t *testing.T) {
	for _, mode := range []string{"known", "chunked", "auth", "auth-cap"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if mode == "auth" {
					w.WriteHeader(403)
					_, _ = io.WriteString(w, `{"code":110030,"msg":"TOKEN-CANARY SECRET-CANARY SQL-CANARY Authorization: Bearer SYNTHETIC","message":"TOKEN-CANARY"}`)
					return
				}
				if mode == "auth-cap" {
					w.WriteHeader(403)
				}
				if mode == "known" {
					w.Header().Set("Content-Length", fmt.Sprint(maxJSONResponseBytes+1))
				} else if mode != "auth-cap" {
					w.WriteHeader(200)
					w.(http.Flusher).Flush()
				}
				_, _ = io.Copy(w, strings.NewReader(strings.Repeat("x", int(maxJSONResponseBytes+1))))
			}))
			defer srv.Close()
			var logs bytes.Buffer
			logger.SetOutput(&logs)
			defer logger.SetOutput(os.Stderr)
			c := &client{baseURL: srv.URL, httpClient: srv.Client()}
			err := c.callAPI(context.Background(), "synthetic", nil, nil)
			if err == nil || calls != 1 {
				t.Fatalf("failure/retry: err=%v calls=%d", err, calls)
			}
			if (mode == "auth" || mode == "auth-cap") && !errors.Is(err, datasource.ErrInvalidCredentials) {
				t.Fatal("authentication classification lost")
			}
			if mode != "auth" && !errors.Is(err, ErrResponseTooLarge) {
				t.Fatalf("cap classification: %v", err)
			}
			for _, marker := range []string{"TOKEN-CANARY", "SECRET-CANARY", "SQL-CANARY", "Authorization:", "SYNTHETIC"} {
				if strings.Contains(err.Error(), marker) || strings.Contains(logs.String(), marker) {
					t.Fatal("canary leaked")
				}
			}
		})
	}
}
