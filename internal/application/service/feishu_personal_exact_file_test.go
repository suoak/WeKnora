package service

import (
	"context"
	"io"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestExactPublishedFileRequiresFreshCheckedBytes(t *testing.T) {
	t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY", "1")
	ctx := types.WithExecutionTenant(context.Background(), 1)
	ctx = context.WithValue(ctx, types.PrincipalContextKey, types.Principal{Type: types.PrincipalWebUser, ID: "viewer"})
	snapshot := &exactPublishedFileSnapshot{knowledge: "doc"}
	calls := 0
	revoked := false
	currentBytes := []byte("original verified DOCX bytes")
	ctx, err := types.WithExactPublishedDocumentRead(ctx, 9, types.ExactDocumentScope{
		TenantID: 1, KnowledgeID: "doc", KBID: "kb", Read: func(context.Context) error {
			calls++
			if revoked {
				return types.ErrExactDocumentScope
			}
			snapshot.mu.Lock()
			snapshot.filename, snapshot.data = "original.docx", append([]byte(nil), currentBytes...)
			snapshot.mu.Unlock()
			return nil
		},
	})
	require.NoError(t, err)
	s := &knowledgeService{} // No repository or file service may be used to reopen an unchecked path.
	file, _, err := s.GetKnowledgeFile(ctx, "doc")
	require.ErrorIs(t, err, types.ErrExactDocumentScope, "a serving identity alone cannot expose an arbitrary file")
	require.Nil(t, file)
	ctx = context.WithValue(ctx, exactPublishedFileKey{}, snapshot)
	currentBytes = []byte("fresh checked DOCX bytes")
	before := calls
	file, name, err := s.GetKnowledgeFile(ctx, "doc")
	require.NoError(t, err)
	require.Equal(t, before+1, calls, "every file read revalidates live authority")
	_, seekable := file.(io.ReadSeeker)
	require.True(t, seekable, "verified previews preserve range/HEAD transport support")
	data, err := io.ReadAll(file)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	require.Equal(t, currentBytes, data)
	require.Equal(t, "original.docx", name)
	file, _, err = s.GetKnowledgeFile(ctx, "foreign")
	require.ErrorIs(t, err, types.ErrExactDocumentScope)
	require.Nil(t, file)
	revoked = true
	file, _, err = s.GetKnowledgeFile(ctx, "doc")
	require.ErrorIs(t, err, types.ErrExactDocumentScope)
	require.Nil(t, file, "the previous successful byte snapshot is never reused after revocation")
}
