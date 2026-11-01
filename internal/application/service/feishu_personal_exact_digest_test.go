package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestExactChunkDigestPostgresJSONBRoundTrip(t *testing.T) {
	db := syncdb.Open(t, "postgres")
	require.NoError(t, db.Exec("CREATE TABLE exact_digest_rows(id text PRIMARY KEY,metadata jsonb)").Error)
	before := &types.Chunk{ID: "exact", Content: "verified worksheet"}
	before.Metadata = types.JSON([]byte("{\"parser.source_format\":\"embedded_xlsx\"," +
		"\"parser.worksheet\":\"Sheet1\",\"parser.worksheet_rows\":\"2\"}"))
	require.NoError(t, db.Exec("INSERT INTO exact_digest_rows VALUES ('exact',?::jsonb)",
		string(before.Metadata)).Error)
	var raw string
	require.NoError(t, db.Raw("SELECT metadata::text FROM exact_digest_rows WHERE id='exact'").Scan(&raw).Error)
	after := *before
	after.Metadata = types.JSON(raw)
	parserDigest, err := exactChunkDigest(before)
	require.NoError(t, err)
	databaseDigest, err := exactChunkDigest(&after)
	require.NoError(t, err)
	require.NotEqual(t, string(before.Metadata), raw, "the fixture exercises an actual JSONB representation change")
	require.Equal(t, parserDigest, databaseDigest)
}

func TestExactChunkDigestJSONBSemantics(t *testing.T) {
	parser := &types.Chunk{ID: "exact", TenantID: 1, KnowledgeID: "document", KnowledgeBaseID: "kb",
		Content: "proved worksheet", SourceContent: "proved worksheet",
		Metadata: types.JSON(
			`{"parser.source_format":"embedded_xlsx","parser.worksheet":"Sheet1","parser.worksheet_rows":"2"}`,
		)}
	database := *parser
	database.Metadata = types.JSON(
		`{"parser.worksheet": "Sheet1", "parser.source_format": "embedded_xlsx", "parser.worksheet_rows": "2"}`,
	)
	before, err := exactChunkDigest(parser)
	require.NoError(t, err)
	after, err := exactChunkDigest(&database)
	require.NoError(t, err)
	require.Equal(t, before, after, "JSONB object order/spacing must not revoke a semantically identical chunk")
	withArray := *parser
	withArray.RelationChunks = types.JSON(`["first","second"]`)
	ordered, err := exactChunkDigest(&withArray)
	require.NoError(t, err)
	withArray.RelationChunks = types.JSON(`["second","first"]`)
	reversed, err := exactChunkDigest(&withArray)
	require.NoError(t, err)
	require.NotEqual(t, ordered, reversed, "array ordering remains part of the execution proof")

	for _, change := range []string{"content", "source", "metadata", "type", "array_order"} {
		t.Run(change, func(t *testing.T) {
			changed := *parser
			switch change {
			case "content":
				changed.Content += " unproved"
			case "source":
				changed.SourceContent += " unproved"
			case "metadata":
				changed.Metadata = types.JSON(
					`{"parser.source_format":"embedded_xlsx","parser.worksheet":"OTHER","parser.worksheet_rows":"2"}`,
				)
			case "type":
				changed.Metadata = types.JSON(
					`{"parser.source_format":"embedded_xlsx","parser.worksheet":"Sheet1","parser.worksheet_rows":2}`,
				)
			case "array_order":
				changed.RelationChunks = types.JSON(`["second","first"]`)
			}
			got, err := exactChunkDigest(&changed)
			require.NoError(t, err)
			require.NotEqual(t, before, got, "canonicalization must retain all semantic content")
		})
	}
}
