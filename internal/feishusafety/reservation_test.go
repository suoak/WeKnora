package feishusafety

import (
	"context"
	"strings"
	"testing"
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
