package service

import (
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"

	"github.com/Tencent/WeKnora/internal/application/repository"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/stretchr/testify/require"
)

func TestPortalMetadataUsesAuthoritativeRowsAndReadableCounts(t *testing.T) {
	f := fixture.New(t)
	ctx := fixture.Key("A", 1, 1, true)
	require.NoError(t, f.DB.Exec("UPDATE users SET username='Creator "+
		"badge',email='PRIVATE_EMAIL@example.invalid' WHERE id='A'").Error)
	require.NoError(t, f.DB.Exec("UPDATE knowledge_bases SET description='KB "+
		"description',creator_id='A',created_at='2026-01-02 "+
		"03:04:05',updated_at='2026-02-03 04:05:06' WHERE id='public-kb'").Error)
	resolver := f.Portal(t)
	s := &PortalMCPKnowledge{Resolver: resolver, Knowledge: repository.NewKnowledgeRepository(f.DB)}
	targets, err := s.Resolve(ctx, nil)
	require.NoError(t, err)
	rows, err := s.KnowledgeBaseMetadata(ctx, targets)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	require.NotContains(t, rows, "unknown-kb")
	require.Equal(t, "KB description", rows["public-kb"].Description)
	require.Equal(t, "Creator badge", rows["public-kb"].CreatorName)
	require.Equal(t, "2026-01-02T03:04:05Z", rows["public-kb"].CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
	for _, target := range targets {
		documents, err := s.ListDocuments(ctx, target.Descriptor().ID)
		require.NoError(t, err)
		require.Equal(t, len(documents), rows[target.Descriptor().ID].DocumentCount)
		page, err := s.ListDocumentsPage(ctx, []*access.PortalKBTarget{target}, "", 1, 20)
		require.NoError(t, err)
		require.Equal(t, len(documents), page.Total)
		require.Len(t, page.Documents, len(documents))
		for i, document := range documents {
			require.Equal(t, document, page.Documents[i].PortalMCPHit)
		}
	}
	encoded, err := json.Marshal(rows)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "PRIVATE_EMAIL")
	// Legacy and deleted creators do not fall back to private email addresses.
	require.NoError(t, f.DB.Exec("UPDATE users SET username='' WHERE id='A'").Error)
	rows, err = s.KnowledgeBaseMetadata(ctx, targets)
	require.NoError(t, err)
	require.Empty(t, rows["public-kb"].CreatorName)
	require.NoError(t, f.DB.Exec("UPDATE users SET username='Deleted "+
		"badge',deleted_at=CURRENT_TIMESTAMP WHERE id='A'").Error)
	rows, err = f.Store.LoadPortalMCPMetadata(ctx, targets)
	require.NoError(t, err)
	require.Empty(t, rows["public-kb"].CreatorName)
}
