package repository

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestWikiPreservingSnapshotTimezoneKeepsEveryFieldAndInstant(t *testing.T) {
	stamp := time.Date(2026, 11, 4, 3, 14, 15, 123456000, time.UTC)
	original := types.WikiPage{
		ID: "historical", TenantID: 1, KnowledgeBaseID: "public-kb", Slug: "concept/original",
		Title: "Historical", Content: "PRESERVE_THIS_BODY", Summary: "original summary",
		PageType: "concept", Status: "published", Version: 7,
		Aliases: types.StringArray{"alias"}, SourceRefs: types.StringArray{"doc"},
		ChunkRefs: types.StringArray{"chunk"}, InLinks: types.StringArray{"incoming"},
		OutLinks: types.StringArray{"outgoing"}, PageMetadata: types.JSON(`{"value":"original"}`),
		CreatedAt: stamp, UpdatedAt: stamp.Add(time.Second),
	}
	utcDigest := wikiDigest(&original)
	converted := original
	zone := time.FixedZone("UTC8", 8*60*60)
	converted.CreatedAt, converted.UpdatedAt = original.CreatedAt.In(zone), original.UpdatedAt.In(zone)
	require.NotEqual(t, utcDigest, wikiDigest(&converted), "the original representation reproduces the rejection")
	require.Equal(t, wikiPreservingPageDigest(&original), wikiPreservingPageDigest(&converted))
	require.True(t, wikiPreservingPageUnchanged(&converted, utcDigest), "legacy UTC recovery marker")
	require.True(t, wikiPreservingPageUnchanged(&converted, wikiDigest(&converted)), "legacy same-zone marker")
	require.False(t, wikiPreservingPageUnchanged(&converted, ""))
	changes := map[string]func(*types.WikiPage){
		"body":       func(p *types.WikiPage) { p.Content += "changed" },
		"title":      func(p *types.WikiPage) { p.Title += "changed" },
		"slug":       func(p *types.WikiPage) { p.Slug += "changed" },
		"source":     func(p *types.WikiPage) { p.SourceRefs = types.StringArray{"different"} },
		"metadata":   func(p *types.WikiPage) { p.PageMetadata = types.JSON(`{"value":"changed"}`) },
		"backlink":   func(p *types.WikiPage) { p.InLinks = types.StringArray{"different"} },
		"owner":      func(p *types.WikiPage) { p.LastEditorID = "different" },
		"version":    func(p *types.WikiPage) { p.Version++ },
		"created_at": func(p *types.WikiPage) { p.CreatedAt = p.CreatedAt.Add(time.Microsecond) },
		"updated_at": func(p *types.WikiPage) { p.UpdatedAt = p.UpdatedAt.Add(time.Microsecond) },
		"deleted_at": func(p *types.WikiPage) { p.DeletedAt = gorm.DeletedAt{Time: stamp, Valid: true} },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			var edited types.WikiPage
			data, err := json.Marshal(converted)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(data, &edited))
			change(&edited)
			require.False(t, wikiPreservingPageUnchanged(&edited, utcDigest))
			require.False(t, wikiPreservingPageUnchanged(&edited, wikiDigest(&converted)))
		})
	}
	require.Equal(t, zone, converted.CreatedAt.Location(), "hashing must not mutate the caller")
}
