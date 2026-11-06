package types

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestExactWorkerPageKeepsLiveBoundariesAndCannotEscape(t *testing.T) {
	ctx := WithExecutionTenant(context.Background(), 1)
	calls, revoked := 0, false
	ctx, err := WithExactDocumentScope(ctx, ExactDocumentScope{
		TenantID: 1, KnowledgeID: "doc", KBID: "kb",
		Read: func(context.Context) error {
			calls++
			if revoked {
				return ErrExactDocumentScope
			}
			return nil
		}, Commit: func(*gorm.DB) error { return nil },
	})
	require.NoError(t, err)
	var escaped context.Context
	err = ReadExactWorkerPage(ctx, 1, "doc", "kb", func(page context.Context) error {
		escaped = page
		for range 100 {
			ok, err := CheckExactDocumentRead(page, "doc", "kb")
			require.True(t, ok)
			require.NoError(t, err)
		}
		_, err := CheckExactDocumentRead(WithExecutionTenant(page, 2), "doc", "kb")
		require.ErrorIs(t, err, ErrExactDocumentScope)
		changed := context.WithValue(page, PrincipalContextKey, Principal{Type: PrincipalWebUser, ID: "viewer"})
		_, err = CheckExactDocumentRead(changed, "doc", "kb")
		require.ErrorIs(t, err, ErrExactDocumentScope)
		ok, err := CheckExactDocumentCommit(&gorm.DB{Statement: &gorm.Statement{Context: page}}, 1, "doc", "kb")
		require.True(t, ok)
		require.ErrorIs(t, err, ErrExactDocumentScope, "read batch cannot write with the worker nonce")
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 3, calls)
	_, err = CheckExactDocumentRead(escaped, "doc", "kb")
	require.ErrorIs(t, err, ErrExactDocumentScope)
	for _, target := range []struct {
		tenant  uint64
		doc, kb string
	}{{2, "doc", "kb"}, {1, "other", "kb"}, {1, "doc", "other"}} {
		err := ReadExactWorkerPage(ctx, target.tenant, target.doc, target.kb, func(context.Context) error {
			t.Fatal("mismatched target reached body selection")
			return nil
		})
		require.ErrorIs(t, err, ErrExactDocumentScope)
	}
	err = ReadExactWorkerPage(ctx, 1, "doc", "kb", func(context.Context) error { revoked = true; return nil })
	require.ErrorIs(t, err, ErrExactDocumentScope, "revocation during read discards output")
	err = ReadExactWorkerPage(ctx, 1, "doc", "kb", func(context.Context) error {
		t.Fatal("revoked source reached selection")
		return nil
	})
	require.ErrorIs(t, err, ErrExactDocumentScope)
}

func TestExactWorkerPageRejectsViewerAndPreservesReadErrors(t *testing.T) {
	ctx := WithExecutionTenant(context.Background(), 1)
	scope := ExactDocumentScope{
		TenantID: 1, KnowledgeID: "doc", KBID: "kb",
		Read: func(context.Context) error { return nil }, Commit: func(*gorm.DB) error { return nil },
	}
	viewer, err := WithExactPublishedDocumentRead(ctx, 7, scope)
	require.NoError(t, err)
	err = ReadExactWorkerPage(viewer, 1, "doc", "kb", func(context.Context) error {
		t.Fatal("viewer became a worker")
		return nil
	})
	require.ErrorIs(t, err, ErrExactDocumentScope)
	worker, err := WithExactDocumentScope(ctx, scope)
	require.NoError(t, err)
	want := errors.New("database unavailable")
	err = ReadExactWorkerPage(worker, 1, "doc", "kb", func(context.Context) error { return want })
	require.ErrorIs(t, err, want)
}
