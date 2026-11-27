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
			tx := &gorm.DB{Statement: &gorm.Statement{Context: ctx}}
			require.NoError(t, k.BeforeCreate(tx))
			m, err := k.Metadata.Map()
			require.NoError(t, err)
			require.Equal(t, "keep", m["source_id"])
			if tc.want {
				require.Equal(t, map[string]interface{}{"user_id": "alice", "name": "Alice"}, m["submitter"])
				meta := NewManualKnowledgeMetadata("edited", ManualKnowledgeStatusDraft, 2)
				require.NoError(t, k.SetManualMetadata(meta))
				after, err := k.Metadata.Map()
				require.NoError(t, err)
				require.Equal(t, m["submitter"], after["submitter"])
			} else {
				require.NotContains(t, m, "submitter")
			}
		})
	}
}

func TestKnowledgeSubmitterDoesNotImpersonateAPICompatibilityUser(t *testing.T) {
	for _, tc := range []struct {
		name      string
		scope     APIKeyScopeType
		principal Principal
		want      bool
	}{
		{"tenant key", APIKeyScopeTenant, Principal{Type: PrincipalAPITenant, ID: "10003"}, false},
		{"platform key", APIKeyScopePlatform, Principal{Type: PrincipalAPIPlatform, ID: "7"}, false},
		{"external id is not a local account", APIKeyScopeTenant, Principal{Type: PrincipalAPIExternalUser, ID: "10003:alice"}, false},
		{"personal MCP key", APIKeyScopeUserMCP, Principal{Type: PrincipalWebUser, ID: "alice"}, true},
		{"personal key mismatched principal", APIKeyScopeUserMCP, Principal{Type: PrincipalWebUser, ID: "bob"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := WithCaller(context.Background(), Caller{TenantID: 10003, UserID: "alice"})
			ctx = context.WithValue(ctx, UserContextKey, &User{ID: "alice", Username: "Alice"})
			ctx = WithTenantAPIKeyScope(ctx, TenantAPIKeyScope{KeyID: 7, ScopeType: tc.scope})
			ctx = WithPrincipal(ctx, tc.principal)
			k := &Knowledge{Metadata: JSON(`{"submitter":{"user_id":"forged"},"author":"source author"}`)}
			require.NoError(t, k.BeforeCreate(&gorm.DB{Statement: &gorm.Statement{Context: ctx}}))
			m, err := k.Metadata.Map()
			require.NoError(t, err)
			require.Equal(t, "source author", m["author"])
			if tc.want {
				require.Equal(t, map[string]interface{}{"user_id": "alice", "name": "Alice"}, m["submitter"])
			} else {
				require.NotContains(t, m, "submitter")
			}
		})
	}
}

func TestKnowledgeSubmitterMultipleUploadersInSharedWorkspace(t *testing.T) {
	for _, id := range []string{"alice", "bob"} {
		ctx := WithCaller(context.Background(), Caller{TenantID: 10003, UserID: id})
		ctx = context.WithValue(ctx, UserContextKey, &User{ID: id, Username: id})
		ctx = WithPrincipal(ctx, Principal{Type: PrincipalWebUser, ID: id})
		k := &Knowledge{}
		require.NoError(t, k.BeforeCreate(&gorm.DB{Statement: &gorm.Statement{Context: ctx}}))
		m, err := k.Metadata.Map()
		require.NoError(t, err)
		require.Equal(t, map[string]interface{}{"user_id": id, "name": id}, m["submitter"])
	}
}
