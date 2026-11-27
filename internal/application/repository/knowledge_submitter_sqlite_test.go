package repository_test

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestKnowledgeSubmitterSQLiteUsesVerifiedPeopleAndRejectsTenantKeyActors(t *testing.T) {
	f := fixture.New(t)
	db := f.DB
	for _, id := range []string{"web-upload", "personal-upload", "tenant-upload", "unknown-key", "wrong-owner"} {
		require.NoError(t, db.Exec(`INSERT INTO knowledges
 (id,tenant_id,knowledge_base_id,type,title,source,file_name,parse_status,enable_status,channel)
 VALUES(?,99,'public-kb','file',?,'synthetic',?,'completed','enabled','web')`, id, id, id+".txt").Error)
	}
	require.NoError(t, db.Exec(`INSERT INTO tenant_api_keys
 (id,tenant_id,scope_type,name,key_hash,api_key,capabilities,full_access)
 VALUES(100,99,'tenant','synthetic tenant key','synthetic-100','not-a-usable-key','[]',false)`).Error)
	for _, entry := range []struct {
		id, actor, details string
	}{
		{"public-doc", "D", `{"api_key_id":100}`},
		{"web-upload", "A", `{}`},
		{"personal-upload", "B", `{"api_key_id":6}`},
		{"tenant-upload", "D", `{"api_key_id":100}`},
		{"unknown-key", "D", `{"api_key_id":101}`},
		{"wrong-owner", "D", `{"api_key_id":6}`},
	} {
		require.NoError(t, db.Create(&types.AuditLog{
			TenantID: 99, ActorUserID: entry.actor, Action: types.AuditActionKnowledgeCreated,
			ScopeType: "knowledge_base", ScopeID: "public-kb", TargetType: "knowledge",
			TargetID: entry.id, Outcome: types.AuditOutcomeSuccess, Details: types.JSON(entry.details),
		}).Error)
	}
	// A verified correction remains authoritative without rewriting the original audit.
	const corrected = `{"submitter":{"user_id":"A","name":"old display name"}}`
	require.NoError(t, db.Exec(`UPDATE knowledges SET metadata=? WHERE id='public-doc'`, corrected).Error)
	reader := repository.NewKnowledgeRepository(db).(interface {
		ReadKnowledgeSubmitters(context.Context, uint64, string, []string) (map[string]*types.KnowledgeSubmitter, error)
	})
	ids := []string{"public-doc", "web-upload", "personal-upload", "tenant-upload", "unknown-key", "wrong-owner"}
	rows, err := reader.ReadKnowledgeSubmitters(context.Background(), 99, "public-kb", ids)
	require.NoError(t, err)
	require.Equal(t, "A", rows["public-doc"].Name, "resolve the current account instead of a stale metadata name")
	require.Equal(t, "A", rows["web-upload"].UserID)
	require.Equal(t, "B", rows["personal-upload"].UserID)
	for _, id := range []string{"tenant-upload", "unknown-key", "wrong-owner"} {
		require.Nil(t, rows[id], id+" must not be attributed to a compatibility user")
	}
	foreign, err := reader.ReadKnowledgeSubmitters(context.Background(), 1, "public-kb", ids)
	require.NoError(t, err)
	require.Empty(t, foreign)
	var stored string
	require.NoError(t, db.Raw(`SELECT metadata FROM knowledges WHERE id='public-doc'`).Scan(&stored).Error)
	require.Equal(t, corrected, stored, "display queries do not rewrite historical metadata")
}
