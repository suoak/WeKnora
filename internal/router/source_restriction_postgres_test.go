package router

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/database"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/testutil/feishulegacy"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type sourceStatusKBService struct {
	interfaces.KnowledgeBaseService
	repo interfaces.KnowledgeBaseRepository
}

func (s sourceStatusKBService) GetKnowledgeBaseByID(ctx context.Context, id string) (*types.KnowledgeBase, error) {
	return s.repo.GetKnowledgeBaseByID(ctx, id)
}

type sourceStatusDSService struct {
	interfaces.DataSourceService
	reader interface {
		KnowledgeSourceRestriction(context.Context, uint64, string, string) (types.KnowledgeSourceRestriction, error)
	}
}

func (s sourceStatusDSService) KnowledgeBaseSourceRestriction(
	ctx context.Context,
	tenant uint64,
	kb string,
) (types.KnowledgeSourceRestriction, error) {
	return s.reader.KnowledgeSourceRestriction(ctx, tenant, kb, "")
}

type sourceStatusReadGate struct{ db *gorm.DB }

func (g sourceStatusReadGate) CheckLegacyReadScope(ctx context.Context) error {
	return types.CheckLegacyReadScope(g.db.WithContext(ctx))
}

func TestSourceRestrictionPostgresHTTPPrivacyAndResponseBoundary(t *testing.T) {
	dsn := feishulegacy.PrivateDSN(t)
	if dsn == "" {
		t.Skip("requires disposable PostgreSQL")
	}
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	t.Chdir(root)
	require.NoError(t, database.RunMigrations(dsn))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	tenant := &types.Tenant{ID: 809933, Name: "source-status-http", Status: "active"}
	require.NoError(t, db.Create(tenant).Error)
	owner := &types.User{
		ID:       uuid.NewString(),
		TenantID: tenant.ID,
		Username: "source-status-owner",
		Email:    "source-status-owner@example.invalid",
		IsActive: true,
	}
	require.NoError(t, db.Create(owner).Error)
	public := &types.KnowledgeBase{
		ID:       uuid.NewString(),
		TenantID: tenant.ID,
		Name:     "SENSITIVE_KB_NAME",
		Type:     "document",
	}
	private := &types.KnowledgeBase{
		ID:       uuid.NewString(),
		TenantID: tenant.ID,
		Name:     "PRIVATE_NAME_CANARY",
		Type:     "document",
	}
	require.NoError(t, db.Create(public).Error)
	require.NoError(t, db.Create(private).Error)
	require.NoError(
		t,
		db.Exec("INSERT INTO feishu_private_kbs(kb_id,owner_id) VALUES (?,?)", private.ID, owner.ID).Error,
	)
	for _, kb := range []*types.KnowledgeBase{public, private} {
		require.NoError(
			t,
			db.Create(
				&types.DataSource{
					ID:                uuid.NewString(),
					TenantID:          tenant.ID,
					KnowledgeBaseID:   kb.ID,
					Name:              "SECRET_SOURCE_NAME",
					Type:              "feishu_drive",
					Status:            "error",
					LegacyAccessState: "revoked",
					Config:            types.JSON(`{}`),
				},
			).Error,
		)
	}
	db = types.EnablePrivateKBGuards(types.EnableLegacyReadGuards(db))
	reader := repository.NewDataSourceRepository(db).(interface {
		KnowledgeSourceRestriction(context.Context, uint64, string, string) (types.KnowledgeSourceRestriction, error)
	})
	h := handler.NewDataSourceHandler(
		sourceStatusDSService{reader: reader},
		sourceStatusKBService{repo: repository.NewKnowledgeBaseRepository(db)},
	)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(legacyReadScopeMiddleware(sourceStatusReadGate{db: db}))
	r.Use(func(c *gin.Context) { c.Set(types.TenantIDContextKey.String(), tenant.ID); c.Next() })
	r.GET("/knowledge-bases/:id/source-restriction", h.GetKnowledgeBaseSourceRestriction)
	for _, tc := range []struct {
		name, kb, user string
		admin          bool
		want           int
	}{
		{"ordinary noncreator member", public.ID, "ordinary-member", false, 200},
		{"private owner", private.ID, owner.ID, false, 200},
		{"ordinary private nonowner", private.ID, "ordinary-member", false, 404},
		{"platform admin private nonowner", private.ID, "platform-admin", true, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), types.TenantIDContextKey, tenant.ID)
			ctx = context.WithValue(ctx, types.UserIDContextKey, tc.user)
			ctx = context.WithValue(ctx, types.SystemAdminContextKey, tc.admin)
			ctx = types.WithCaller(
				ctx,
				types.Caller{TenantID: tenant.ID, UserID: tc.user, Role: types.TenantRoleViewer},
			)
			ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: tc.user})
			w := httptest.NewRecorder()
			r.ServeHTTP(
				w,
				httptest.NewRequest("GET", "/knowledge-bases/"+tc.kb+"/source-restriction", nil).WithContext(ctx),
			)
			require.Equal(t, tc.want, w.Code, w.Body.String())
			canaries := []string{"SENSITIVE_KB_NAME", "PRIVATE_NAME_CANARY", "SECRET_SOURCE_NAME", owner.ID}
			for _, canary := range canaries {
				require.NotContains(t, w.Body.String(), canary)
			}
			if tc.want == 200 {
				require.Contains(t, w.Body.String(), `"source_state":"revoked"`)
			}
		})
	}
	require.ErrorIs(
		t,
		types.CheckLegacySourceRead(db, "", public.ID),
		types.ErrLegacySourceRestricted,
		"metadata endpoint does not release document protection",
	)
}
