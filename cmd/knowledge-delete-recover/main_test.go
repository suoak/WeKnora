package main

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestRecoveryPreviewRollbackAndApplyCAS(t *testing.T) {
	db := syncdb.Open(t, "postgres")
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}, &types.FeishuPersonalImport{},
		&types.FeishuPersonalResource{}, &types.FeishuPersonalAttempt{}, &types.TaskPendingOp{}))
	for _, sql := range []string{
		"CREATE TABLE users(id text,is_active boolean,deleted_at timestamptz)",
		"CREATE TABLE tenants(id bigint,status text,deleted_at timestamptz)",
		"CREATE TABLE tenant_members(user_id text,tenant_id bigint,role text,status text,deleted_at timestamptz)",
		"CREATE TABLE knowledge_bases(id text,tenant_id bigint,creator_id text,deleted_at timestamptz)",
		"CREATE TABLE feishu_private_kbs(kb_id text,owner_id text)",
		"CREATE TABLE feishu_legacy_resources(data_source_id text,knowledge_id text)",
		"INSERT INTO users VALUES('actor',true,NULL)",
		"INSERT INTO tenants VALUES(1,'active',NULL)",
		"INSERT INTO tenant_members VALUES('actor',1,'admin','active',NULL)",
		"INSERT INTO knowledge_bases VALUES('kb',1,'owner',NULL)",
	} {
		require.NoError(t, db.Exec(sql).Error)
	}
	k := &types.Knowledge{
		ID: "doc", TenantID: 1, KnowledgeBaseID: "kb", Channel: "feishu_personal",
		ParseStatus: types.ParseStatusCompleted, Metadata: types.JSON(`{"datasource_id":"import"}`),
	}
	require.NoError(t, db.Create(k).Error)
	require.NoError(
		t,
		db.Create(
			&types.FeishuPersonalImport{
				ID:          "import",
				KBID:        "kb",
				KnowledgeID: "doc",
				Status:      "source_managed",
				URL:         "https://test.invalid/doc",
			},
		).Error,
	)
	require.NoError(
		t,
		db.Create(
			&types.FeishuPersonalResource{
				ImportID:           "import",
				LogicalKnowledgeID: "doc",
				Generation:         6,
				CurrentAttemptID:   "attempt",
			},
		).Error,
	)
	require.NoError(
		t,
		db.Create(
			&types.FeishuPersonalAttempt{
				ID:                 "attempt",
				ImportID:           "import",
				LogicalKnowledgeID: "doc",
				CallerTenantID:     1,
				Status:             "obsolete",
			},
		).Error,
	)
	require.NoError(
		t,
		db.Create(
			&types.DataSource{
				ID:                "import",
				TenantID:          1,
				KnowledgeBaseID:   "kb",
				Type:              "feishu",
				Status:            "error",
				LegacyAccessState: "hold",
				Config: types.JSON(
					`{"resource_ids":["links"],"settings":{"links":["https://test.invalid/doc"]}}`,
				),
			},
		).Error,
	)
	p := types.KnowledgeListDeletePayload{
		TenantID:        1,
		KnowledgeBaseID: "kb",
		KnowledgeIDs:    []string{"doc"},
		Initiator:       types.TaskInitiator{UserID: "actor"},
	}
	snapshot, err := fence(context.Background(), db, p, "", false)
	require.NoError(t, err)
	require.NotEmpty(t, snapshot)
	var state string
	require.NoError(t, db.Table("feishu_personal_imports").Select("status").Where("id='import'").Scan(&state).Error)
	require.Equal(t, "source_managed", state, "preview rolls back every fence mutation")
	_, err = fence(context.Background(), db, p, "wrong-snapshot", true)
	require.ErrorContains(t, err, "snapshot changed")
	require.NoError(t, db.Exec("UPDATE tenant_members SET status='removed'").Error)
	_, err = fence(context.Background(), db, p, snapshot, true)
	require.ErrorContains(t, err, "no longer authorized")
	require.NoError(t, db.Exec("UPDATE tenant_members SET status='active'").Error)
	actual, err := fence(context.Background(), db, p, snapshot, true)
	require.NoError(t, err)
	require.Equal(t, snapshot, actual)
	require.NoError(t, db.Table("feishu_personal_imports").Select("status").Where("id='import'").Scan(&state).Error)
	require.Equal(t, "deleted", state)
	var source types.DataSource
	require.NoError(t, db.Unscoped().Where("id='import'").First(&source).Error)
	require.True(t, source.DeletedAt.Valid)
	require.Equal(t, "hold", source.LegacyAccessState)
}
