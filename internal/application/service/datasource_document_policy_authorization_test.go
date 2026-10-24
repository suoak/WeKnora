package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type documentPolicyScopeRepo struct {
	interfaces.DataSourceRepository
	source *types.DataSource
	saved  []types.SyncDocumentDeferral
}

func (r *documentPolicyScopeRepo) FindByID(context.Context, string) (*types.DataSource, error) {
	return r.source, nil
}

func (r *documentPolicyScopeRepo) ReplaceDocumentDeferrals(_ context.Context,
	source *types.DataSource,
	_ string,
	entries []types.SyncDocumentDeferral,
) error {
	r.saved = entries
	b, _ := json.Marshal(entries)
	source.DocumentDeferrals = b
	return nil
}

func TestDocumentDeferralPolicyUsesCallerNotBorrowedTenant(t *testing.T) {
	r := &documentPolicyScopeRepo{source: &types.DataSource{
		ID:              "source",
		TenantID:        2,
		KnowledgeBaseID: "kb",
		Type:            types.ConnectorTypeFeishu,
	}}
	s := &DataSourceService{dsRepo: r}
	ctx := types.WithCaller(context.Background(),
		types.Caller{
			TenantID: 1,
			UserID:   "author",
			Role:     types.TenantRoleOwner,
		})
	ctx = types.WithExecutionTenant(ctx, 2)
	_, err := s.ReplaceDocumentDeferrals(ctx, "source", types.DocumentDeferralHash(nil), nil)
	require.Error(t, err)
	require.Nil(t, r.saved)
}

func TestDocumentDeferralPolicyStampsAuthorAndPreservesUnchangedAudit(t *testing.T) {
	r := &documentPolicyScopeRepo{source: &types.DataSource{
		ID:              "source",
		TenantID:        1,
		KnowledgeBaseID: "kb",
		Type:            types.ConnectorTypeFeishu,
		Config:          types.JSON(`{"resource_ids":["root"]}`),
	}}
	s := &DataSourceService{dsRepo: r}
	ctx := types.WithCaller(context.Background(),
		types.Caller{
			TenantID: 1,
			UserID:   "real-author",
			Role:     types.TenantRoleOwner,
		})
	input := []types.SyncDocumentDeferral{{
		SourceResourceID: "root",
		ExternalID:       "doc",
		ReasonCode:       types.DocumentDeferralAwaitingAuthorization,
		Reason:           "awaiting owner",
		Reference:        "approved request",
		RequestedBy:      "forged-author",
	}}
	_, err := s.ReplaceDocumentDeferrals(ctx, "source", types.DocumentDeferralHash(nil), input)
	require.NoError(t, err)
	require.Equal(t, "real-author", r.saved[0].RequestedBy)
	when := r.saved[0].RequestedAt
	_, err = s.ReplaceDocumentDeferrals(ctx, "source", types.DocumentDeferralHash(r.saved), r.saved)
	require.NoError(t, err)
	require.Equal(t, when, r.saved[0].RequestedAt)
}
