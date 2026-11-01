package repository

import (
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPersonalExactRetryAcceptsOnlySealedUnpublishedOwner(t *testing.T) {
	for _, change := range []string{
		"valid", "active", "candidate", "generation", "current", "nonce", "lease", "status", "auth", "expected",
	} {
		t.Run(change, func(t *testing.T) {
			a := types.FeishuPersonalAttempt{
				ID: "attempt", ImportID: "import", LogicalKnowledgeID: "document",
				CandidateVersionID: "candidate", Fence: 4, Status: "needs_attention", AuthMode: types.FeishuUserOAuth,
			}
			r := types.FeishuPersonalResource{
				ImportID: a.ImportID, CurrentAttemptID: a.ID,
				LogicalKnowledgeID: a.LogicalKnowledgeID, CandidateVersionID: a.CandidateVersionID, Generation: 5,
			}
			switch change {
			case "active":
				r.ActiveVersionID = "published"
			case "candidate":
				r.CandidateVersionID = "other"
			case "generation":
				r.Generation++
			case "current":
				r.CurrentAttemptID = "other"
			case "nonce":
				a.OwnerNonce = "live"
			case "lease":
				now := time.Now()
				a.LeaseUntil = &now
			case "status":
				a.Status = "processing"
			case "auth":
				a.AuthMode = types.FeishuApplicationGrant
			case "expected":
				a.ExpectedActiveVersion = "published"
			}
			require.Equal(t, change == "valid", reviewedExactRetryState(&r, &a, 5))
		})
	}
}
