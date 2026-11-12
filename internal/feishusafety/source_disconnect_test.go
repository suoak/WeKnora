package feishusafety

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"
)

type resetDownloadBody struct {
	prefix io.Reader
	err    error
}

type resetDownloadTransport func(*http.Request) (*http.Response, error)

func (f resetDownloadTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func (b *resetDownloadBody) Read(p []byte) (int, error) {
	if b.prefix != nil {
		n, err := b.prefix.Read(p)
		if err != io.EOF {
			return n, err
		}
		b.prefix = nil
	}
	return 0, b.err
}

func (*resetDownloadBody) Close() error { return nil }

func TestSourceDownloadResetPreservesFailureWithoutStoppingIndependentSources(t *testing.T) {
	for _, cause := range []error{syscall.ECONNRESET, syscall.EPIPE} {
		t.Run(cause.Error(), func(t *testing.T) {
			g, client := fixture(t, Config{})
			InstallDefault(g)
			defer InstallDefault(nil)
			wrapped := &net.OpError{Op: "read", Net: "tcp", Err: fmt.Errorf("download interrupted: %w", cause)}
			transport := Transport{
				Base: resetDownloadTransport(func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: 200,
						Header:     make(http.Header),
						Request:    req,
						Body:       &resetDownloadBody{prefix: strings.NewReader("partial"), err: wrapped},
					}, nil
				}),
			}
			req, err := http.NewRequestWithContext(WithScope(context.Background(), "failed-download"),
				http.MethodGet, "https://source.invalid/file", nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := transport.RoundTrip(req)
			if err != nil {
				t.Fatal(err)
			}
			_, err = io.ReadAll(response.Body)
			_ = response.Body.Close()
			if !errors.Is(err, cause) {
				t.Fatalf("partial download lost its failure: %v", err)
			}
			if client.Exists(context.Background(), g.key("trip")).Val() != 0 ||
				g.Check(WithScope(context.Background(), "independent-source")) != nil {
				t.Fatal("retryable source reset stopped independent sources")
			}
		})
	}
}

func TestSourceResponseResetRequiresHealthyPermit(t *testing.T) {
	for _, embedding := range []bool{false, true} {
		g, client := fixture(t, Config{})
		if !embedding {
			if err := client.Del(context.Background(), g.PermitKey()).Err(); err != nil {
				t.Fatal(err)
			}
		}
		tripRequestFailure(context.Background(), g, syscall.ECONNRESET, embedding, "response")
		if !g.localClosed.Load() || g.Check(context.Background()) == nil {
			t.Fatalf("reset escaped the closed gate: embedding=%t", embedding)
		}
	}
}
