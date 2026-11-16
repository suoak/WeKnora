package types

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestKnowledgeSubmitterUsesAuthenticatedCaller(t *testing.T) {
	for _, tc := range []struct {
		name   string
		user   *User
		caller string
		want   bool
	}{
		{"authenticated", &User{ID: "alice", Username: "Alice"}, "alice", true},
		{"missing", nil, "", false},
		{"mismatched", &User{ID: "alice", Username: "Alice"}, "bob", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := WithCaller(context.Background(), Caller{UserID: tc.caller})
			if tc.user != nil {
				ctx = context.WithValue(ctx, UserContextKey, tc.user)
			}
			k := &Knowledge{Metadata: JSON(`{"submitter":{"name":"forged"},"source_id":"keep"}`)}
			require.NoError(t, k.BeforeCreate(&gorm.DB{Statement: &gorm.Statement{Context: ctx}}))
			m, err := k.Metadata.Map()
			require.NoError(t, err)
			require.Equal(t, "keep", m["source_id"])
			if tc.want {
				require.Equal(t, map[string]interface{}{"user_id": "alice", "name": "Alice"}, m["submitter"])
				require.NoError(t, k.SetManualMetadata(NewManualKnowledgeMetadata("edited", ManualKnowledgeStatusDraft, 2)))
				after, err := k.Metadata.Map()
				require.NoError(t, err)
				require.Equal(t, m["submitter"], after["submitter"])
			} else {
				require.NotContains(t, m, "submitter")
			}
		})
	}
}
