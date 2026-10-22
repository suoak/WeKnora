package access

import (
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDocumentFilterDenyAllAndTrustedIntersection(t *testing.T) {
	var deny AuthorizedDocumentSet
	_, err := deny.Constrain(99, "kb", types.RetrieveParams{}, true)
	require.ErrorIs(t, err, ErrDocumentFilterDenied)
	set := AuthorizedDocumentSet{tenantID: 99, kbID: "kb", ids: []string{"public", "explicit"}}
	for _, test := range []struct {
		tenant    uint64
		kb        string
		params    types.RetrieveParams
		supported bool
	}{
		{99, "kb", types.RetrieveParams{}, false}, {1, "kb", types.RetrieveParams{}, true}, {99, "other", types.RetrieveParams{}, true},
		{99, "kb", types.RetrieveParams{KnowledgeIDs: []string{"sensitive"}}, true}, {99, "kb", types.RetrieveParams{KnowledgeBaseIDs: []string{"other"}}, true},
	} {
		_, err = set.Constrain(test.tenant, test.kb, test.params, test.supported)
		require.ErrorIs(t, err, ErrDocumentFilterDenied)
	}
	original := types.RetrieveParams{KnowledgeIDs: []string{"public", "sensitive"}, TopK: 10}
	p, err := set.Constrain(99, "kb", original, true)
	require.NoError(t, err)
	require.Equal(t, []string{"public"}, p.KnowledgeIDs)
	require.Equal(t, []string{"public", "sensitive"}, original.KnowledgeIDs)
	require.Equal(t, 10, p.TopK)
}

func TestDerivedSourcesNeverSendUnauthorizedBodyToModel(t *testing.T) {
	for _, sources := range [][]string{nil, {""}, {"public", "sensitive"}} {
		modelCalls := 0
		err := CheckDerivedSources(sources, func(id string) error {
			if id != "public" {
				return ErrForbidden
			}
			return nil
		})
		if err == nil {
			modelCalls++
		}
		require.Error(t, err)
		require.Zero(t, modelCalls)
	}
	require.NoError(t, CheckDerivedSources([]string{"public"}, func(string) error { return nil }))
}
