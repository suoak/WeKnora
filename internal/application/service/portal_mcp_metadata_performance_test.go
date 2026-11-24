package service

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/logger"
)

type metadataSQLCounter struct {
	logger.Interface
	statements atomic.Int64
}

func TestPortalMetadataManyKnowledgeBasesUseBoundedQueries(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	var kbs []*types.KnowledgeBase
	for i := 0; i < 501; i++ {
		kbs = append(kbs, &types.KnowledgeBase{
			ID:               fmt.Sprintf("metadata-many-%03d", i),
			TenantID:         99,
			Name:             "Duplicate display name",
			Type:             "document",
			EmbeddingModelID: "synthetic",
			SummaryModelID:   "synthetic",
		})
	}
	require.NoError(t, f.DB.CreateInBatches(kbs, 50).Error)
	counter := &metadataSQLCounter{Interface: logger.Discard}
	f.DB.Logger = counter
	s := publishedServiceM7(t, f)
	ctx := types.WithLocalSourceContentRead(fixture.Key("A", 1, 1, true))
	all, err := s.Resolve(ctx, nil)
	require.NoError(t, err)
	targets := []*access.PortalKBTarget{}
	for _, target := range all {
		if len(target.Descriptor().ID) >= 14 && target.Descriptor().ID[:14] == "metadata-many-" {
			targets = append(targets, target)
		}
	}
	require.Len(t, targets, 501)
	counter.statements.Store(0)
	rows, err := s.KnowledgeBaseMetadata(ctx, targets)
	require.NoError(t, err)
	require.Len(t, rows, 501)
	require.EqualValues(t, 2, counter.statements.Load(),
		"two metadata queries for 501 authorized KBs; no document queries")
	for _, row := range rows {
		require.Zero(t, row.DocumentCount)
	}
	_, err = s.Resolve(ctx, []string{"Duplicate display name"})
	require.ErrorIs(t, err, access.ErrPortalKBAmbiguous)
	exact, err := s.Resolve(ctx, []string{"metadata-many-500"})
	require.NoError(t, err)
	require.Len(t, exact, 1)
	require.Equal(t, "metadata-many-500", exact[0].Descriptor().ID)
}

func (c *metadataSQLCounter) Trace(_ context.Context, _ time.Time, _ func() (string, int64), _ error) {
	c.statements.Add(1)
}

// Compare the retained legacy coordinator with the new page on the same
// migrated SQL fixture and real grants. Assert work reduction, not wall time.
func TestPortalMetadataPaginationSQLWorkReduction(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	var documents []*types.Knowledge
	for i := 0; i < 1006; i++ {
		documents = append(documents, &types.Knowledge{
			ID:              fmt.Sprintf("metadata-perf-%04d", i),
			TenantID:        99,
			KnowledgeBaseID: "public-kb",
			Title:           fmt.Sprintf("Metadata document %04d", i),
			Type:            "manual",
			ParseStatus:     types.ParseStatusCompleted,
			EnableStatus:    "enabled",
			Channel:         "web",
		})
	}
	require.NoError(t, f.DB.CreateInBatches(documents, 100).Error)
	counter := &metadataSQLCounter{Interface: logger.Discard}
	f.DB.Logger = counter
	s := publishedServiceM7(t, f)
	ctx := types.WithLocalSourceContentRead(fixture.Key("A", 1, 1, true))
	started := time.Now()
	legacy, err := s.ListDocuments(ctx, "public-kb")
	oldTime := time.Since(started)
	oldSQL := counter.statements.Swap(0)
	require.NoError(t, err)
	require.Len(t, legacy, 1007)
	started = time.Now()
	targets, err := s.Resolve(ctx, []string{"public-kb"})
	require.NoError(t, err)
	page, err := s.ListDocumentsPage(ctx, targets, "", 1, 20)
	require.NoError(t, err)
	require.NoError(t, validatePortalTargets(ctx, targets))
	newTime := time.Since(started)
	newSQL := counter.statements.Load()
	require.Equal(t, len(legacy), page.Total)
	require.Len(t, page.Documents, 20)
	require.Less(t, newSQL, oldSQL-900, "current-page selection must remove the legacy per-document query loop")
	t.Logf("metadata_pagination_comparison documents=%d page_size=20 "+
		"legacy_sql=%d paged_sql=%d legacy_ms=%.3f paged_ms=%.3f",
		len(legacy), oldSQL, newSQL, float64(oldTime.Microseconds())/1000, float64(newTime.Microseconds())/1000)
}
