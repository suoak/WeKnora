package service

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type privateTaskKB struct {
	interfaces.KnowledgeBaseService
	db *gorm.DB
}

func (s *privateTaskKB) GetKnowledgeBaseByID(ctx context.Context, id string) (*types.KnowledgeBase, error) {
	var kb types.KnowledgeBase
	err := s.db.WithContext(ctx).Where("id=?", id).First(&kb).Error
	return &kb, err
}

func privateTaskFixture(t *testing.T) (*FeishuPersonalService, *asynq.Task) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "private-task.db")), &gorm.Config{})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	for _, statement := range []string{
		"CREATE TABLE users(id TEXT PRIMARY KEY,is_active BOOLEAN,deleted_at DATETIME)",
		"CREATE TABLE tenants(id INTEGER PRIMARY KEY,status TEXT,deleted_at DATETIME)",
		"CREATE TABLE tenant_members(id INTEGER PRIMARY KEY,user_id TEXT,tenant_id INTEGER," +
			"role TEXT,status TEXT,deleted_at DATETIME)",
		"CREATE TABLE knowledge_bases(id TEXT PRIMARY KEY,tenant_id INTEGER,creator_id TEXT,deleted_at DATETIME)",
		"CREATE TABLE knowledges(id TEXT PRIMARY KEY,tenant_id INTEGER,knowledge_base_id TEXT," +
			"channel TEXT,metadata TEXT,deleted_at DATETIME)",
		"CREATE TABLE feishu_private_kbs(kb_id TEXT PRIMARY KEY,owner_id TEXT)",
		"INSERT INTO users VALUES('A',true,NULL),('B',true,NULL)",
		"INSERT INTO tenants VALUES(1,'active',NULL)",
		"INSERT INTO tenant_members VALUES(1,'A',1,'owner','active',NULL),(2,'B',1,'admin','active',NULL)",
		"INSERT INTO knowledge_bases VALUES('private',1,'A',NULL),('other',1,'B',NULL)",
		"INSERT INTO feishu_private_kbs VALUES('private','A'),('other','B')",
		"INSERT INTO knowledges VALUES('doc',1,'private','web','{}',NULL),('other-doc',1,'other','web','{}',NULL)",
	} {
		require.NoError(t, db.Exec(statement).Error)
	}
	db = types.EnablePrivateKBGuards(db)
	s := NewFeishuPersonalService(db, nil, nil, &privateTaskKB{db: db}, nil)
	body, err := json.Marshal(types.ManualProcessPayload{TenantID: 1, KnowledgeBaseID: "private", KnowledgeID: "doc"})
	require.NoError(t, err)
	return s, asynq.NewTask(types.TypeManualProcess, body)
}

func TestFeishuV2PrivateTaskRestoresFreshOwnerWithoutWeakeningReads(t *testing.T) {
	s, task := privateTaskFixture(t)
	var row types.Knowledge
	require.Error(t, s.db.WithContext(context.Background()).Where("id=?", "doc").First(&row).Error)
	called := false
	wrapped := s.WrapPrivateTaskHandler(func(ctx context.Context, _ *asynq.Task) error {
		called = true
		require.Equal(t, "A", types.PrivateKBReadUser(ctx))
		require.False(t, types.IsSystemAdminFromContext(ctx))
		require.NoError(t, s.db.WithContext(ctx).Where("id=?", "doc").First(&row).Error)
		var other types.Knowledge
		require.Error(t, s.db.WithContext(ctx).Where("id=?", "other-doc").First(&other).Error)
		require.NoError(t, types.CheckFeishuSourceRead(ctx))
		require.NoError(t, s.db.Exec("UPDATE tenant_members SET role='viewer' WHERE user_id='A'").Error)
		require.Error(
			t,
			types.CheckFeishuSourceRead(ctx),
			"a retained task context must not retain removed write authority",
		)
		return nil
	})
	require.NoError(t, wrapped(context.Background(), task))
	require.True(t, called)
	row = types.Knowledge{}
	require.Error(t, s.db.WithContext(context.Background()).Where("id=?", "doc").First(&row).Error)
}

func TestFeishuV2PrivateTaskRejectsWrongParentsPrincipalsAndRevocation(t *testing.T) {
	for _, scenario := range []string{"tenant", "parent", "user", "key", "inactive", "member", "feishu"} {
		t.Run(scenario, func(t *testing.T) {
			s, task := privateTaskFixture(t)
			ctx := context.Background()
			payload := types.ManualProcessPayload{TenantID: 1, KnowledgeBaseID: "private", KnowledgeID: "doc"}
			switch scenario {
			case "tenant":
				payload.TenantID = 2
			case "parent":
				payload.KnowledgeBaseID = "other"
			case "user":
				ctx = context.WithValue(ctx, types.UserIDContextKey, "B")
			case "key":
				ctx = types.WithPrincipal(ctx, types.Principal{Type: "api_key", ID: "machine"})
			case "inactive":
				require.NoError(t, s.db.Exec("UPDATE users SET is_active=false WHERE id='A'").Error)
			case "member":
				require.NoError(t, s.db.Exec("UPDATE tenant_members SET status='revoked' WHERE user_id='A'").Error)
			case "feishu":
				require.NoError(t, s.db.Exec("UPDATE knowledges SET channel='feishu_personal' WHERE id='doc'").Error)
			}
			body, err := json.Marshal(payload)
			require.NoError(t, err)
			task = asynq.NewTask(task.Type(), body)
			called := false
			err = s.WrapPrivateTaskHandler(
				func(context.Context, *asynq.Task) error { called = true; return nil },
			)(
				ctx,
				task,
			)
			require.Error(t, err)
			require.False(t, called)
		})
	}
}
