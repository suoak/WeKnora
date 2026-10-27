package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"syscall"
	"testing"
)

func TestTransportDiagnosticsNeverExposeRequestMaterial(t *testing.T) {
	for _, tc := range []struct {
		cause error
		want  string
	}{
		{context.DeadlineExceeded, "timeout"},
		{io.EOF, "eof"},
		{io.ErrUnexpectedEOF, "truncated"},
		{syscall.ECONNRESET, "connection_reset"},
		{&net.DNSError{Err: "secret-token", Name: "secret-document.invalid"}, "dns"},
		{errors.New("http2: server sent GOAWAY secret-token"), "http2_goaway"},
		{errors.New("stream error: stream ID 1 secret-token"), "http2_stream"},
		{errors.New("certificate secret-token"), "tls_certificate"},
		{errors.New("SSRF blocked secret-token"), "security_rejected"},
		{errors.New("secret-token"), "unclassified"},
	} {
		wrapped := fmt.Errorf("request https://secret-document.invalid/secret-token: %w", tc.cause)
		if got := TransportErrorCategory(wrapped); got != tc.want {
			t.Fatalf("category = %q, want %q", got, tc.want)
		}
		err := &APIError{Category: ErrorCategoryTransient, Operation: "get_wiki_node", Cause: wrapped}
		if text := err.Error(); strings.Contains(text, "secret-") || !strings.Contains(text, "transport="+tc.want) {
			t.Fatal("unsafe or incomplete transport diagnostic")
		}
	}
	if TransportErrorCategory(nil) != "" {
		t.Fatal("nil failure has a transport cause")
	}
}
