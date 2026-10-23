package feishusafety

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestReservationChineseSplitting(t *testing.T) {
	g, _ := fixture(t, Config{TPM: 30000, TokenOverhead: 128})
	InstallDefault(g)
	ctx := WithScope(context.Background(), "source")
	for _, n := range []int{20, 21, 100} {
		inputs := make([]string, n)
		for i := range inputs {
			inputs[i] = strings.Repeat("知", 512)
		}
		parts, err := SplitEmbeddingInputs(ctx, inputs)
		if err != nil {
			t.Fatal(err)
		}
		total := 0
		for _, part := range parts {
			cost := 0
			for _, input := range part {
				cost += len(input) + 128
			}
			if len(part) > 20 || cost > 30000 {
				t.Fatal("unsafe batch", len(part), cost)
			}
			total += len(part)
		}
		if total != n {
			t.Fatal("lost inputs")
		}
	}
	if _, err := SplitEmbeddingInputs(ctx, []string{strings.Repeat("知", 11000)}); err == nil {
		t.Fatal("oversized chunk was accepted")
	}
	plain := make([]string, 100)
	parts, err := SplitEmbeddingInputs(context.Background(), plain)
	if err != nil || len(parts) != 1 || len(parts[0]) != 100 {
		t.Fatal("unmarked batch changed")
	}
}

type sourceStatusTransport struct{ status int }

func (s sourceStatusTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: s.status,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewBufferString(`{"error":"source unavailable"}`)),
		Request:    r,
	}, nil
}

type gracefulWaitTransport struct{ started chan struct{} }

func (s gracefulWaitTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	close(s.started)
	<-r.Context().Done()
	return nil, r.Context().Err()
}

func TestGracefulLocalCancelDoesNotCreateGlobalTrip(t *testing.T) {
	g, client := fixture(t, Config{})
	InstallDefault(g)
	started, done := make(chan struct{}), make(chan error, 1)
	request, _ := http.NewRequestWithContext(
		WithScope(context.Background(), "source"),
		"GET",
		"https://source.invalid/doc",
		nil,
	)
	go func() {
		_, err := (Transport{Base: gracefulWaitTransport{started: started}}).RoundTrip(request)
		done <- err
	}()
	<-started
	g.CloseLocal()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("local departure did not cancel wire")
		}
	case <-time.After(time.Second):
		t.Fatal("wire did not drain")
	}
	if client.Exists(context.Background(), g.key("trip")).Val() != 0 {
		t.Fatal("normal local drain globally tripped other workers")
	}
}

func TestSource403404DoNotTripOtherSources(t *testing.T) {
	for _, status := range []int{403, 404} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			g, client := fixture(t, Config{})
			InstallDefault(g)
			request, _ := http.NewRequestWithContext(
				WithScope(context.Background(), "source-a"),
				"GET",
				"https://source.invalid/doc",
				nil,
			)
			response, err := (Transport{Base: sourceStatusTransport{status: status}}).RoundTrip(request)
			if err != nil || response.StatusCode != status {
				t.Fatal("source error not preserved", err)
			}
			if _, err := io.Copy(io.Discard, response.Body); err != nil {
				t.Fatal(err)
			}
			if err := response.Body.Close(); err != nil {
				t.Fatal(err)
			}
			if client.Exists(context.Background(), g.key("trip")).Val() != 0 ||
				g.Check(context.Background()) != nil {
				t.Fatal("one missing/forbidden source globally stopped synchronization")
			}
		})
	}
}

func TestReservationTicketSingleUseAndRelease(t *testing.T) {
	g, client := fixture(t, Config{})
	InstallDefault(g)
	inputs := []string{"abc"}
	ctx := WithScope(context.Background(), "source")
	ticket, cleanup, err := ReserveEmbedding(ctx, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if client.HLen(ctx, g.key("inflight")).Val() != 1 {
		t.Fatal("reservation absent")
	}
	if _, reserved, err := consumeReservation(ticket, g, []string{"xyz"}); !reserved || err == nil {
		t.Fatal("content mismatch accepted")
	}
	release, reserved, err := consumeReservation(ticket, g, inputs)
	if !reserved || err != nil {
		t.Fatal(err)
	}
	cleanup() // parent cleanup cannot release a ticket while wire owns it.
	if client.HLen(ctx, g.key("inflight")).Val() != 1 {
		t.Fatal("wire slot released prematurely")
	}
	if _, _, err := consumeReservation(ticket, g, inputs); err == nil {
		t.Fatal("ticket reused by retry")
	}
	release()
	release()
	cleanup()
	if client.HLen(ctx, g.key("inflight")).Val() != 0 || client.ZCard(ctx, g.key("window")).Val() != 1 {
		t.Fatal("release refunded budget or leaked slot")
	}
	unused, closeUnused, err := ReserveEmbedding(ctx, []string{"unused"})
	if err != nil {
		t.Fatal(err)
	}
	closeUnused()
	if _, _, err := consumeReservation(unused, g, []string{"unused"}); err == nil {
		t.Fatal("canceled ticket reused")
	}
}
