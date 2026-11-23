package embedding

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/models/api"
)

func TestNormalProviderErrorDoesNotRelatchAtProtocolBoundary(t *testing.T) {
	for _, status := range []int{403, 500} {
		model, _, client, g, _ := reservationEmbedder(t, 0, time.Second)
		g.SetProviderFailureIsolation(true)
		original := model.inner
		model.inner = invalidResultEmbedder{err: &api.HTTPError{StatusCode: status, Body: "PRIVATE_PROVIDER_BODY"}}
		ctx := feishusafety.WithScope(context.Background(), "affected-source")
		vectors, err := model.BatchEmbed(ctx, []string{"one"})
		if vectors != nil || !errors.Is(err, feishusafety.ErrProviderFailure) || strings.Contains(err.Error(), "PRIVATE_") {
			t.Fatal("provider failure leaked output or lost classification", err)
		}
		var response *feishusafety.ProviderResponseError
		if !errors.As(err, &response) || response.Status != status {
			t.Fatal("HTTP status lost", err)
		}
		if client.Exists(ctx, g.PermitKey()).Val() != 1 || g.Check(ctx) != nil {
			t.Fatal("protocol wrapper closed unrelated sources")
		}
		model.inner = original
		vectors, err = model.BatchEmbed(feishusafety.WithScope(context.Background(), "healthy-source"), []string{"1:text"})
		if err != nil || len(vectors) != 1 {
			t.Fatal("healthy source could not embed", err)
		}
		model.inner = invalidResultEmbedder{err: feishusafety.ErrSourceAdmissionChanged}
		_, err = model.BatchEmbed(ctx, []string{"one"})
		if !errors.Is(err, feishusafety.ErrSourceAdmissionChanged) || errors.Is(err, feishusafety.ErrProviderFailure) {
			t.Fatal("authority denial misclassified as a document failure", err)
		}
	}
}

func TestNormalProviderInvalidVectorsStayRejectedWithoutGlobalStop(t *testing.T) {
	model, _, _, g, _ := reservationEmbedder(t, 0, time.Second)
	g.SetProviderFailureIsolation(true)
	model.inner = invalidResultEmbedder{vectors: [][]float32{{1}}}
	ctx := feishusafety.WithScope(context.Background(), "affected-source")
	if vectors, err := model.BatchEmbed(ctx, []string{"one", "two"}); err == nil || vectors != nil {
		t.Fatal("partial vectors accepted")
	}
	if g.Check(feishusafety.WithScope(context.Background(), "sibling")) != nil {
		t.Fatal("invalid provider response latched service")
	}
}
