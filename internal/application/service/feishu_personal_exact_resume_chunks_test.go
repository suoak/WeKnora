package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPersonalExactResumeEmptyOutputDoesNotAdoptOrphanVectors(t *testing.T) {
	db := syncdb.Open(t, "postgres")
	require.NoError(t, db.AutoMigrate(&types.Chunk{}))
	require.NoError(t, db.Exec("CREATE TABLE embeddings(knowledge_id text)").Error)
	c := &exactPersonalConsumer{recovery: &PersonalRecovery{service: &FeishuPersonalService{db: db}},
		source: &personalExactDocument{knowledge: "doc", kb: "kb", tenant: 1}, parsed: &personalExactParsedDocument{}, resuming: true}
	generated := []*types.Chunk{{ID: "new", KnowledgeID: "doc", KnowledgeBaseID: "kb", TenantID: 1, ChunkType: types.ChunkTypeText}}
	require.ErrorIs(t, c.reproveResumeChunks(context.Background(), generated), errExactResumeEmptyOutput)
	require.Equal(t, "new", generated[0].ID)
	require.NoError(t, db.Exec("INSERT INTO embeddings VALUES('doc')").Error)
	err := c.reproveResumeChunks(context.Background(), generated)
	require.Error(t, err)
	require.NotErrorIs(t, err, errExactResumeEmptyOutput)
	var count int64
	require.NoError(t, db.Table("chunks").Count(&count).Error)
	require.Zero(t, count)
}

func TestPersonalExactResumeReprovesAllChunkFieldsBeforeAdoption(t *testing.T) {
	for _, change := range []string{
		"valid", "body", "header", "enabled", "unknown_chunk", "missing_vector", "extra_vector", "legacy_video",
		"duplicate_vector", "wrong_vector_kb", "wrong_vector_chunk",
	} {
		t.Run(change, func(t *testing.T) {
			db := syncdb.Open(t, "postgres")
			require.NoError(t, db.AutoMigrate(&types.Chunk{}))
			require.NoError(t, db.Exec("ALTER TABLE chunks ADD COLUMN video_info text").Error)
			require.NoError(t, db.Exec(`CREATE TABLE embeddings (
knowledge_id text,knowledge_base_id text,source_id text,chunk_id text)`).Error)
			parent := &types.Chunk{
				ID: "stored-parent", TenantID: 1, KnowledgeID: "doc", KnowledgeBaseID: "kb",
				Content: "Fresh parser parent text.", ChunkIndex: 0,
				ChunkType: types.ChunkTypeParentText,
			}
			text := &types.Chunk{
				ID: "stored-text", TenantID: 1, KnowledgeID: "doc", KnowledgeBaseID: "kb",
				Content: "Fresh parser child text.", ContextHeader: "Verified heading",
				ChunkIndex: 0, ChunkType: types.ChunkTypeText, ParentChunkID: parent.ID,
			}
			require.NoError(t, db.Create(parent).Error)
			require.NoError(t, db.Create(text).Error)
			require.NoError(t, db.Exec("UPDATE chunks SET is_enabled=false").Error)
			insertVector := "INSERT INTO embeddings VALUES ('doc','kb','stored-text','stored-text')"
			require.NoError(t, db.Exec(insertVector).Error)
			expectedParent, expectedText := *parent, *text
			expectedParent.ID, expectedText.ID = "new-parent", "new-text"
			expectedText.ParentChunkID = "new-parent"
			switch change {
			case "legacy_video":
				err := db.Exec("UPDATE chunks SET video_info='unverified legacy content'").Error
				require.NoError(t, err)
			case "body":
				err := db.Exec("UPDATE chunks SET content='unknown body' WHERE id='stored-text'").Error
				require.NoError(t, err)
			case "header":
				require.NoError(t, db.Exec("UPDATE chunks SET context_header='unknown header'").Error)
			case "enabled":
				require.NoError(t, db.Exec("UPDATE chunks SET is_enabled=true").Error)
			case "unknown_chunk":
				unknown := *text
				unknown.ID, unknown.SeqID = "unknown", 0
				require.NoError(t, db.Create(&unknown).Error)
			case "missing_vector":
				require.NoError(t, db.Exec("DELETE FROM embeddings").Error)
			case "extra_vector":
				err := db.Exec("INSERT INTO embeddings VALUES ('doc','kb','unknown','unknown')").Error
				require.NoError(t, err)
			case "duplicate_vector":
				require.NoError(t, db.Exec(insertVector).Error)
			case "wrong_vector_kb":
				require.NoError(t, db.Exec("UPDATE embeddings SET knowledge_base_id='other'").Error)
			case "wrong_vector_chunk":
				require.NoError(t, db.Exec("UPDATE embeddings SET chunk_id='other'").Error)
			}
			consumer := &exactPersonalConsumer{
				recovery: &PersonalRecovery{service: &FeishuPersonalService{db: db}},
				source:   &personalExactDocument{knowledge: "doc", kb: "kb", tenant: 1},
				parsed:   &personalExactParsedDocument{}, resuming: true,
			}
			var before, after string
			snapshot := `SELECT json_agg(to_jsonb(c) ORDER BY c.id)::text FROM chunks c`
			require.NoError(t, db.Raw(snapshot).Scan(&before).Error)
			err := consumer.reproveResumeChunks(context.Background(),
				[]*types.Chunk{&expectedParent, &expectedText})
			if change == "valid" || change == "missing_vector" {
				require.NoError(t, err)
				require.Equal(t, "stored-parent", expectedParent.ID)
				require.Equal(t, "stored-text", expectedText.ID)
				require.Equal(t, "stored-parent", expectedText.ParentChunkID)
			} else {
				require.Error(t, err)
			}
			require.NoError(t, db.Raw(snapshot).Scan(&after).Error)
			require.JSONEq(t, before, after, "reproof must not alter any existing chunk")
		})
	}
}
