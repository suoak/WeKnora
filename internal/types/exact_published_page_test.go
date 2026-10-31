package types

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExactPublishedPageValidatesBoundariesNotEveryRow(t *testing.T) {
	ctx := WithExecutionTenant(context.Background(), 1)
	ctx = context.WithValue(ctx, PrincipalContextKey, Principal{Type: PrincipalWebUser, ID: "viewer"})
	calls := 0
	revoked := false
	ctx, err := WithExactPublishedDocumentRead(ctx, 7, ExactDocumentScope{
		TenantID: 1, KnowledgeID: "doc", KBID: "kb",
		Read: func(context.Context) error {
			calls++
			if revoked {
				return ErrExactDocumentScope
			}
			return nil
		},
	})
	require.NoError(t, err)
	var escaped context.Context
	err = ReadExactPublishedPage(ctx, "doc", func(page context.Context) error {
		escaped = page
		for range 25 {
			ok, err := CheckExactDocumentRead(page, "doc", "kb")
			require.True(t, ok)
			require.NoError(t, err)
		}
		ok, err := CheckExactDocumentRead(page, "other", "kb")
		require.False(t, ok)
		require.NoError(t, err)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 3, calls, "issue, before selection, before return")
	_, err = CheckExactDocumentRead(escaped, "doc", "kb")
	require.ErrorIs(t, err, ErrExactDocumentScope, "page capability must not escape its read")
	err = ReadExactPublishedPage(ctx, "other", func(context.Context) error {
		t.Fatal("foreign document must not be selected")
		return nil
	})
	require.ErrorIs(t, err, ErrExactDocumentScope)
	err = ReadExactPublishedPage(ctx, "doc", func(context.Context) error {
		revoked = true
		return nil
	})
	require.ErrorIs(t, err, ErrExactDocumentScope, "revocation during selection discards the page")
	err = ReadExactPublishedPage(ctx, "doc", func(context.Context) error {
		t.Fatal("revoked source must not reach SQL selection")
		return nil
	})
	require.ErrorIs(t, err, ErrExactDocumentScope)
}
