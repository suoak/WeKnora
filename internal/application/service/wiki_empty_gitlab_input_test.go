package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestWikiProductionVerifiedEmptyGitLabInput(t *testing.T) {
	testWikiVerifiedEmptyGitLabInput(t, fixture.New(t))
}

func testWikiVerifiedEmptyGitLabInput(t *testing.T, f *fixture.Fixture) {
	f.PreparePublished(t)
	db, ctx := f.DB, context.Background()
	if !db.Migrator().HasTable(&types.StoredResource{}) {
		require.NoError(t, db.AutoMigrate(&types.StoredResource{}))
	}
	if !db.Migrator().HasTable(&types.ResourceBinding{}) {
		require.NoError(t, db.AutoMigrate(&types.ResourceBinding{}))
	}
	require.NoError(t, db.Exec(`INSERT INTO data_sources(
 id,tenant_id,knowledge_base_id,name,type,status,legacy_access_state,config)
 VALUES('empty-gitlab',1,'public-two','synthetic','gitlab','active','unmanaged','{}')`).Error)
	require.NoError(t, db.Create(&types.Knowledge{
		ID: "empty-gitlab-doc", TenantID: 1, KnowledgeBaseID: "public-two", Type: "file",
		Title: "empty README", FileType: "md", FileSize: 1,
		FileHash: "68b329da9893e34099c7d8ad5cb9c940", FilePath: "resource://abcdefghijklmnopqrstuv",
		Channel: "gitlab", ParseStatus: "completed", EnableStatus: "enabled",
		Metadata: types.JSON(`{"datasource_id":"empty-gitlab","external_id":"gitlab:synthetic:main:README.md"}`),
	}).Error)
	require.NoError(t, db.Create(&types.StoredResource{
		ID: "empty-file", Handle: "abcdefghijklmnopqrstuv", TenantID: 1, Provider: "local",
		PhysicalPath: "synthetic-empty-file", Size: 1, LocationHash: "synthetic-registered-location",
		State: "active", Lifecycle: "persistent",
	}).Error)
	require.NoError(t, db.Create(&types.ResourceBinding{
		ID: "empty-binding", ResourceID: "empty-file", TenantID: 1,
		OwnerType: "knowledge", OwnerID: "empty-gitlab-doc", Relation: "source_file",
	}).Error)
	repo := repository.NewWikiPageRepository(db)
	proof, err := repository.BeginWikiMCPGeneration(ctx, repo, "public-two")
	require.NoError(t, err, "a verified empty source must not block a healthy sibling document")
	page := &types.WikiPage{
		ID: "healthy-sibling-page", TenantID: 1, KnowledgeBaseID: "public-two",
		Slug: "summary/healthy-sibling", PageType: "summary", Status: "published",
		Content: "synthetic current sibling content", Version: 1,
		SourceRefs: types.StringArray{"public-two-doc"},
	}
	require.NoError(t, repo.Create(proof, page))
	for _, mutation := range []string{
		`UPDATE knowledges SET file_hash='00000000000000000000000000000000' WHERE id='empty-gitlab-doc'`,
		`UPDATE knowledges SET file_size=3 WHERE id='empty-gitlab-doc'`,
		`UPDATE knowledges SET file_type='pdf' WHERE id='empty-gitlab-doc'`,
		`UPDATE knowledges SET parse_status='processing' WHERE id='empty-gitlab-doc'`,
		`UPDATE resources SET tenant_id=2 WHERE id='empty-file'`,
		`UPDATE resources SET size=2 WHERE id='empty-file'`,
		`UPDATE resources SET content_hash='wrong-strong-hash' WHERE id='empty-file'`,
		`UPDATE resources SET physical_path='replaced-location' WHERE id='empty-file'`,
		`UPDATE resource_bindings SET owner_id='public-two-doc' WHERE id='empty-binding'`,
		`UPDATE data_sources SET status='paused' WHERE id='empty-gitlab'`,
	} {
		t.Run(mutation, func(t *testing.T) {
			tx := db.Begin()
			require.NoError(t, tx.Error)
			defer tx.Rollback()
			require.NoError(t, tx.Exec(mutation).Error)
			_, err := repository.BeginWikiMCPGeneration(ctx, repository.NewWikiPageRepository(tx), "public-two")
			require.Error(t, err, "source changes and missing-input uncertainty remain closed")
		})
	}
	require.NoError(t, db.Create(&types.ResourceBinding{
		ID: "ambiguous-binding", ResourceID: "empty-file", TenantID: 1,
		OwnerType: "knowledge", OwnerID: "public-two-doc", Relation: "source_file",
	}).Error)
	_, err = repository.BeginWikiMCPGeneration(ctx, repo, "public-two")
	require.Error(t, err, "a shared or ambiguous file binding cannot certify an empty input")
}
