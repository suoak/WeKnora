package types

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestExactDocumentScopeDoesNotTrustContextOrWholeKB(t *testing.T) {
	ctx := context.WithValue(context.Background(), "exact_document_proof", "forged")
	ok, err := CheckExactDocumentRead(ctx, "doc", "kb")
	require.NoError(t, err)
	require.False(t, ok)
	_, err = WithExactDocumentScope(ctx, ExactDocumentScope{TenantID: 1, KnowledgeID: "doc", KBID: "kb"})
	require.ErrorIs(t, err, ErrExactDocumentScope)
	revoked := false
	read := func(context.Context) error {
		if revoked {
			return ErrExactDocumentScope
		}
		return nil
	}
	ctx, err = WithExactDocumentScope(ctx, ExactDocumentScope{
		TenantID: 1, KnowledgeID: "doc", KBID: "kb", Read: read, Commit: func(*gorm.DB) error { return nil },
	})
	require.NoError(t, err)
	for _, target := range [][2]string{{"", "kb"}, {"other", "kb"}, {"doc", "other"}} {
		ok, err = CheckExactDocumentRead(ctx, target[0], target[1])
		require.NoError(t, err)
		require.False(t, ok)
	}
	ok, err = CheckExactDocumentRead(ctx, "doc", "kb")
	require.True(t, ok)
	require.NoError(t, err)
	revoked = true
	ok, err = CheckExactDocumentRead(ctx, "doc", "kb")
	require.True(t, ok)
	require.ErrorIs(t, err, ErrExactDocumentScope)
}

func TestExactDocumentScopeCommitUsesActualWriteTransaction(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Exec("CREATE TABLE writes (id integer PRIMARY KEY)").Error)
	commitCalls := 0
	ctx, err := WithExactDocumentScope(context.Background(), ExactDocumentScope{
		TenantID: 1, KnowledgeID: "doc", KBID: "kb", Read: func(context.Context) error { return nil },
		Commit: func(tx *gorm.DB) error {
			commitCalls++
			return tx.Exec("INSERT INTO writes VALUES (1)").Error
		},
	})
	require.NoError(t, err)
	rollback := errors.New("reject data write")
	err = WithExactDocumentWrite(ctx, db, 1, "doc", "kb", func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("writes").Count(&count).Error)
		require.EqualValues(t, 1, count)
		return rollback
	})
	require.ErrorIs(t, err, rollback)
	require.Equal(t, 1, commitCalls)
	var count int64
	require.NoError(t, db.Table("writes").Count(&count).Error)
	require.Zero(t, count, "authorization and data writes must roll back together")
	for _, target := range []struct {
		tenant  uint64
		doc, kb string
	}{{2, "doc", "kb"}, {1, "other", "kb"}, {1, "doc", "other"}} {
		err = WithExactDocumentWrite(ctx, db, target.tenant, target.doc, target.kb, func(*gorm.DB) error {
			t.Fatal("mismatched target reached write")
			return nil
		})
		require.ErrorIs(t, err, ErrExactDocumentScope)
	}
	require.Equal(t, 1, commitCalls)
}

func TestExactDocumentScopePublishedReadCannotBecomeWorkerOrWholeKB(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	revoked := false
	ctx, err := WithExactPublishedDocumentRead(WithExecutionTenant(context.Background(), 1), 17, ExactDocumentScope{
		TenantID: 1, KnowledgeID: "doc", KBID: "kb", Read: func(context.Context) error {
			if revoked {
				return ErrExactDocumentScope
			}
			return nil
		},
		Commit: func(*gorm.DB) error { t.Fatal("serving cannot retain a write callback"); return nil },
	})
	require.NoError(t, err)
	ok, err := CheckExactPublishedDocumentRead(ctx)
	require.True(t, ok)
	require.NoError(t, err)
	ok, err = CheckExactDocumentRead(ctx, "", "kb")
	require.False(t, ok)
	require.NoError(t, err)
	err = WithExactDocumentWrite(ctx, db, 1, "doc", "kb", func(*gorm.DB) error {
		t.Fatal("read-only proof reached a data write")
		return nil
	})
	require.ErrorIs(t, err, ErrExactDocumentScope)
	_, err = CheckExactPublishedDocumentRead(WithExecutionTenant(ctx, 2))
	require.ErrorIs(t, err, ErrExactDocumentScope)
	revoked = true
	_, err = CheckExactPublishedDocumentRead(ctx)
	require.ErrorIs(t, err, ErrExactDocumentScope)
	_, _, _, _, forged := ExactPublishedDocumentIdentity(
		context.WithValue(context.Background(), "published_receipt", 17),
	)
	require.False(t, forged)
}
