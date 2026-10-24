package types

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDocumentDeferralPolicyValidation(t *testing.T) {
	s := &DataSource{ID: "ds", TenantID: 1, KnowledgeBaseID: "kb", Config: JSON(`{"resource_ids":["root"]}`)}
	e := SyncDocumentDeferral{TenantID: 1,
		DataSourceID:     "ds",
		KnowledgeBaseID:  "kb",
		SourceResourceID: "root",
		ExternalID:       "document",
		ReasonCode:       DocumentDeferralAwaitingAuthorization,
		Reason:           "awaiting owner",
		RequestedBy:      "operator",
		RequestedAt:      time.Now().UTC(),
		Reference:        "explicit request"}
	for _, kind := range []string{"valid",
		"other_tenant",
		"other_source",
		"other_kb",
		"other_root",
		"reason",
		"no_author",
		"duplicate",
		"null"} {
		t.Run(kind, func(t *testing.T) {
			entry := e
			switch kind {
			case "other_tenant":
				entry.TenantID = 2
			case "other_source":
				entry.DataSourceID = "other"
			case "other_kb":
				entry.KnowledgeBaseID = "other"
			case "other_root":
				entry.SourceResourceID = "other"
			case "reason":
				entry.ReasonCode = "ignore_unknown_errors"
			case "no_author":
				entry.RequestedBy = ""
			}
			policy := []SyncDocumentDeferral{entry}
			if kind == "duplicate" {
				policy = append(policy, entry)
			}
			blob, _ := json.Marshal(policy)
			s.DocumentDeferrals = blob
			if kind == "null" {
				s.DocumentDeferrals = JSON(`null`)
			}
			_, err := s.ParseDocumentDeferrals()
			if kind == "valid" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
