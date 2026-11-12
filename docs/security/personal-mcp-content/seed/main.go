// Isolated acceptance seeder. Fixed loopback host/database; never imports a
// production backup or upgrades an existing database. Synthetic credentials
// are written to a private file, never stdout.
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/hibiken/asynq"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) > 1 && os.Args[1] == "retry-synthetic" {
		if filepath.Base(os.Getenv("KNOWHUB_UPLOAD_ACCEPT_DIR")) != "knowhub-personal-upload-r1-20261112" {
			return errors.New("isolated lab required")
		}
		inspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: "127.0.0.1:18872"})
		defer func() { _ = inspector.Close() }()
		queues, e := inspector.Queues()
		if e != nil {
			return e
		}
		n := 0
		for _, q := range queues {
			retry, e := inspector.ListRetryTasks(q)
			if e != nil {
				return e
			}
			archived, e := inspector.ListArchivedTasks(q)
			if e != nil {
				return e
			}
			for _, task := range append(retry, archived...) {
				if task.Type != "document:process" {
					continue
				}
				if e = inspector.RunTask(q, task.ID); e != nil {
					return e
				}
				n++
			}
		}
		fmt.Println("isolated tasks retried", n)
		return nil
	}

	dir := os.Getenv("KNOWHUB_UPLOAD_ACCEPT_DIR")
	if filepath.Base(dir) != "knowhub-personal-upload-r1-20261112" {
		return errors.New("isolated directory required")
	}
	const dsn = "postgres://postgres@127.0.0.1:18871/knowhub_personal_upload3046?sslmode=di" +
		"sable&options=-c%20app.skip_embedding=false"
	m, e := migrate.New("file://migrations/versioned", dsn)
	if e != nil {
		return e
	}
	defer func() { _ = m.Close() }()
	_, _, e = m.Version()
	if !errors.Is(e, migrate.ErrNilVersion) {
		return errors.New("refuse to seed or migrate any preexisting database")
	}
	if e = m.Migrate(3046); e != nil {
		return e
	}
	version, dirty, e := m.Version()
	if e != nil || version != 3046 || dirty {
		return errors.New("unexpected schema")
	}
	db, e := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if e != nil {
		return e
	}
	pool, e := db.DB()
	if e != nil {
		return e
	}
	defer func() { _ = pool.Close() }()
	must := func(e error) {
		if e != nil {
			panic(e)
		}
	}
	ctx := context.Background()
	for _, id := range []uint64{1, 2} {
		must(
			db.Create(
				&types.Tenant{
					ID:                  id,
					Name:                fmt.Sprintf("Synthetic space %d", id),
					Status:              "active",
					StorageEngineConfig: &types.StorageEngineConfig{DefaultProvider: "local"},
				},
			).Error,
		)
	}
	for _, user := range []string{"upload-A", "upload-B"} {
		must(
			db.Create(
				&types.User{ID: user, Username: user, Email: user + "@synthetic.invalid", TenantID: 1, IsActive: true},
			).Error,
		)
		must(
			db.Create(
				&types.TenantMember{
					TenantID: 1,
					UserID:   user,
					Role:     types.TenantRoleContributor,
					Status:   types.TenantMemberStatusActive,
				},
			).Error,
		)
	}
	must(
		db.Create(
			&types.TenantMember{
				TenantID: 2,
				UserID:   "upload-B",
				Role:     types.TenantRoleOwner,
				Status:   types.TenantMemberStatusActive,
			},
		).Error,
	)
	model := types.Model{
		ID:       "upload-embedding",
		TenantID: 1,
		Name:     "synthetic-wire-embedding",
		Type:     types.ModelTypeEmbedding,
		Source:   types.ModelSourceRemote,
		Status:   types.ModelStatusActive,
		Parameters: types.ModelParameters{
			BaseURL:             "http://127.0.0.1:18874/v1",
			Provider:            "openai",
			EmbeddingParameters: types.EmbeddingParameters{Dimension: 3},
		},
	}
	must(db.Create(&model).Error)
	model.ID = "upload-embedding-2"
	model.TenantID = 2
	must(db.Create(&model).Error)
	for _, item := range []struct {
		id, owner string
		tenant    uint64
	}{
		{"upload-a-kb", "upload-A", 1},
		{"upload-private-kb", "upload-A", 1},
		{"upload-b-kb", "upload-B", 1},
		{"upload-editor-kb", "upload-B", 2},
		{"upload-viewer-kb", "upload-B", 2},
	} {
		embedding := "upload-embedding"
		if item.tenant == 2 {
			embedding = "upload-embedding-2"
		}
		kb := &types.KnowledgeBase{
			ID:                    item.id,
			Name:                  item.id,
			TenantID:              item.tenant,
			CreatorID:             item.owner,
			Type:                  "document",
			EmbeddingModelID:      embedding,
			StorageProviderConfig: &types.StorageProviderConfig{Provider: "local"},
		}
		kb.EnsureDefaults()
		must(db.Create(kb).Error)
	}
	must(db.Exec("INSERT INTO feishu_private_kbs(kb_id,owner_id) VALUES('upload-private-kb','upload-A')").Error)
	must(
		db.Create(
			&types.Organization{
				ID:            "upload-org",
				Name:          "Synthetic upload organization",
				OwnerID:       "upload-B",
				OwnerTenantID: 2,
				InviteCode:    "upload-test-only",
			},
		).Error,
	)
	for _, tenant := range []uint64{1, 2} {
		must(
			db.Create(
				&types.OrganizationTenantMember{
					ID:             fmt.Sprintf("upload-member-%d", tenant),
					OrganizationID: "upload-org",
					TenantID:       tenant,
					Role:           types.OrgRoleEditor,
				},
			).Error,
		)
	}
	for _, item := range []struct{ id, kb, permission string }{
		{"upload-editor-share", "upload-editor-kb", "editor"},
		{"upload-viewer-share", "upload-viewer-kb", "viewer"},
	} {
		must(
			db.Exec(
				"INSERT INTO kb_shares(id,knowledge_base_id,organization_id,source_tenant_i"+
					"d,shared_by_user_id,permission) VALUES(?,?,'upload-org',2,'upload-B',?)",
				item.id,
				item.kb,
				item.permission,
			).Error,
		)
	}
	must(
		db.Exec(
			"INSERT INTO tenant_portal_configs(tenant_id,status,display_name,created_by" +
				",updated_by) VALUES(2,'published','Synthetic public','upload-B','upload-B'" +
				")",
		).Error,
	)
	engines := `[{"retriever_type":"keywords","retriever_engine_type":"postgres"},{"retrie` +
		`ver_type":"vector","retriever_engine_type":"postgres"}]`
	must(db.Exec("UPDATE tenants SET retriever_engines=?", engines).Error)
	credentials := map[string]any{"schema_version": version, "synthetic": true}
	for _, item := range []struct {
		name, user string
		id         uint64
		caps       types.StringArray
		fixed      bool
	}{
		{"key_a", "upload-A", 101, types.StringArray{"retrieve", "enterprise_retrieve", "add_content"}, false},
		{"key_b", "upload-B", 102, types.StringArray{"retrieve", "enterprise_retrieve", "add_content"}, false},
		{"key_read", "upload-A", 103, types.StringArray{"retrieve", "enterprise_retrieve"}, false},
		{"key_fixed", "upload-A", 104, types.StringArray{"retrieve", "enterprise_retrieve", "add_content"}, true},
	} {
		bytes := make([]byte, 32)
		if _, e = rand.Read(bytes); e != nil {
			return e
		}
		token := "sk-" + hex.EncodeToString(bytes)
		hash := sha256.Sum256([]byte(token))
		owner := item.user
		mode := "dynamic_all"
		if item.fixed {
			mode = "fixed"
		}
		key := &types.TenantAPIKey{
			ID:                item.id,
			OwnerUserID:       &owner,
			ScopeType:         types.APIKeyScopeUserMCP,
			PersonalScopeMode: mode,
			Name:              item.name,
			ClientType:        types.MCPClientGeneric,
			KeyHash:           hex.EncodeToString(hash[:]),
			Capabilities:      item.caps,
		}
		must(db.WithContext(ctx).Create(key).Error)
		if item.fixed {
			scope := &types.APIKeyTenantScope{APIKeyID: item.id, TenantID: 1, KBScopeMode: types.APIKeyKBScopeSelected}
			must(db.Create(scope).Error)
			must(
				db.Create(
					&types.APIKeyKnowledgeBaseScope{
						APIKeyTenantScopeID: scope.ID,
						APIKeyID:            item.id,
						TenantID:            1,
						KnowledgeBaseID:     "upload-a-kb",
						SourceType:          types.APIKeyKBSourceOwned,
					},
				).Error,
			)
		}
		credentials[item.name] = token
	}
	private := filepath.Join(dir, "private")
	if e = os.MkdirAll(private, 0700); e != nil {
		return e
	}
	raw, e := json.Marshal(credentials)
	if e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(private, "clients.json"), raw, 0600); e != nil {
		return e
	}
	fmt.Println("synthetic schema3046 seed complete; no existing database upgraded")
	return nil
}
