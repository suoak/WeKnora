//go:build feishu_recovery_live

package service

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

// This is a real, opt-in TEST-site authorization preflight. It cannot execute a
// production source or mutate source permissions. Normal serialized token refresh
// may update the test connection; no token is copied into a disposable clone.
func TestRecoveryLiveAuthorizationPreflight(t *testing.T) {
	if os.Getenv("FEISHU_RECOVERY_LIVE_ACCEPTANCE") != "1" {
		t.Skip("explicit isolated live acceptance required")
	}
	u, err := url.Parse(os.Getenv("FEISHU_RECOVERY_TEST_SITE_DSN"))
	if err != nil || u.Hostname() != "knowhub-feishu-v2-d6e99d894d3a-postgres-1" || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		t.Fatal("isolated test-site DSN required")
	}
	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: gormlog.Discard})
	if err != nil {
		t.Fatal("test database unavailable")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal("test database unavailable")
	}
	defer pool.Close()
	var source types.DataSource
	if db.Where("id=? AND legacy_access_state='active'", "847ec22f-ceb3-4f73-b268-ba54c7c3ff67").Take(&source).Error != nil {
		t.Fatal("dedicated test source unavailable")
	}
	cfg, err := source.ParseConfig()
	if err != nil {
		t.Fatal("test config invalid")
	}
	ref, err := types.ParseFeishuSourceReference(cfg)
	if err != nil || ref == nil || ref.Kind != types.FeishuUserOAuth {
		t.Fatal("test OAuth reference unavailable")
	}
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), types.UserIDContextKey, ref.OwnerUserID), 60*time.Second)
	defer cancel()
	personal := NewFeishuPersonalService(db, repository.NewSystemSettingRepository(db), nil, nil, nil)
	app, err := personal.app(ctx)
	if err != nil {
		t.Fatal("test app unavailable")
	}
	if validateFeishuApp(app) != nil {
		t.Fatal("live gate blocked: exact test callback policy invalid or expired; no override applied")
	}
	remote, client, _, err := personal.ResolveCredentialReference(ctx, ref.Kind, ref.ConnectionID)
	if err != nil {
		t.Fatal("live gate blocked: current test identity could not be resolved")
	}
	links, reference, err := core.ReferenceLinks(cfg)
	if err != nil || !reference || len(links) != 1 {
		t.Fatal("single bounded test reference required")
	}
	resource, err := core.ParsePersonalResourceURL(links[0])
	if err != nil {
		t.Fatal("test resource invalid")
	}
	resource, err = client.ResolveBoundResource(remote, resource)
	if err != nil {
		t.Fatal("live gate blocked: real resource ACL/revision lookup refused")
	}
	permissions, err := client.InspectPersonalPermissions(remote, resource)
	if err != nil || !permissions.View || !permissions.Copy || !permissions.Export {
		t.Fatal("live gate blocked: required real document permissions not proven")
	}
	t.Log("real test identity, bounded resource revision, view/copy/export permission preflight passed; full recovery lifecycle not implied")
}
