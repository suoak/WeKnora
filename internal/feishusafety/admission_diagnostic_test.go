package feishusafety

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestEmbeddingAdmissionStopRetainsFiniteDiagnosticAndFirstIncident(t *testing.T) {
	for _, scenario := range []struct{ raw, code string }{
		{"SAFETY_NO_PERMIT", "safety_no_permit"},
		{"reused or mismatched embedding reservation", "reservation_mismatch"},
		{"single request exceeds token allocation", "request_exceeds_allocation"},
		{"untrusted provider body secret-token document content", "unclassified"},
	} {
		t.Run(scenario.code, func(t *testing.T) {
			g, client := fixture(t, Config{})
			InstallDefault(g)
			ctx := WithScope(context.Background(), "source")
			cause := fmt.Errorf("%w: %s", ErrClosed, scenario.raw)
			err := embeddingAdmissionFailure(ctx, g, cause, "embedding_budget_or_state_unavailable")
			if !errors.Is(err, ErrClosed) {
				t.Fatal("diagnostics changed the rejected request")
			}
			rows, readErr := client.XRange(ctx, g.key("audit"), "-", "+").Result()
			if readErr != nil || len(rows) != 1 || rows[0].Values["admission_code"] != scenario.code ||
				rows[0].Values["reason"] != "embedding_budget_or_state_unavailable" {
				t.Fatalf("missing bounded first failure: %v %v", rows, readErr)
			}
			if strings.Contains(fmt.Sprint(rows), "secret-token") || client.Exists(ctx, g.PermitKey()).Val() != 0 {
				t.Fatal("diagnostics leaked contents or retained an outbound permit")
			}
			_ = embeddingAdmissionFailure(ctx, g, errors.New("later failure"), "embedding_reservation_unavailable")
			if client.XLen(ctx, g.key("audit")).Val() != 1 {
				t.Fatal("a later failure overwrote or flooded the original incident")
			}
		})
	}
}
