// Command feishu-diagnostic reproduces authorized export requests in the isolated
// test site. It never publishes, creates Knowledge, or changes a sync stop latch.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

func mask(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])[:12]
}

func run() error {
	if os.Getenv("FEISHU_DIAGNOSTIC_ISOLATED") != "1" || os.Getenv("DB_NAME") != "knowhub_feishu_v2_test" {
		return errors.New("isolated_diagnostic_guard_required")
	}
	dsn := &url.URL{
		Scheme: "postgres", Host: os.Getenv("DB_HOST") + ":" + os.Getenv("DB_PORT"),
		Path: "/knowhub_feishu_v2_test", User: url.UserPassword(os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD")),
		RawQuery: "sslmode=disable",
	}
	db, err := gorm.Open(
		postgres.Open(dsn.String()),
		&gorm.Config{Logger: gormlog.Default.LogMode(gormlog.Silent)},
	)
	if err != nil {
		return errors.New("isolated_database_unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	var user types.User
	if err := db.Where("username = ? AND deleted_at IS NULL", "test").First(&user).Error; err != nil {
		return errors.New("test_owner_missing")
	}
	ctx = context.WithValue(ctx, types.UserIDContextKey, user.ID)
	var connection types.FeishuConnection
	if err := db.Where("user_id = ?", user.ID).First(&connection).Error; err != nil {
		return errors.New("test_connection_missing")
	}
	settings := repository.NewSystemSettingRepository(db)
	row, err := settings.Get(ctx, "integration.feishu.personal_app")
	if err != nil || row == nil {
		return errors.New("application_setting_unavailable")
	}
	cipher, err := row.AsString()
	if err != nil {
		return errors.New("application_setting_invalid")
	}
	plain, err := utils.DecryptStoredSecret(cipher)
	if err != nil {
		return errors.New("application_setting_unreadable")
	}
	var cfg service.FeishuAppConfig
	if json.Unmarshal([]byte(plain), &cfg) != nil || cfg.AppID != connection.AppID {
		return errors.New("application_identity_mismatch")
	}
	plain = ""
	var grant struct {
		Scope string `json:"scope"`
	}
	grantPlain, err := utils.DecryptStoredSecret(connection.Tokens)
	if err != nil || json.Unmarshal([]byte(grantPlain), &grant) != nil {
		return errors.New("grant_unreadable")
	}
	grantPlain = ""
	result := map[string]any{
		"status": "DIAGNOSTIC_ONLY", "at": time.Now().UTC(),
		"identity": "existing_personal_oauth", "access_expired_before": !connection.ExpiresAt.After(time.Now()),
		"refresh_valid_before": connection.RefreshExpiresAt.After(
			time.Now(),
		), "granted_scopes_before": strings.Fields(grant.Scope),
		"original_provider_receipt": "not_recorded_by_original_binary", "knowledge_writes": false,
		"sync_control_writes": false, "application_token_fallback": false,
	}
	var observations []core.APIObservation
	ctx = core.WithAPIObserver(
		ctx,
		func(event core.APIObservation) { observations = append(observations, event) },
	)
	svc := service.NewFeishuPersonalService(db, settings, nil, nil, nil)
	_, err = svc.UserToken(ctx, user.ID, connection.AppID, connection.OpenID)
	if err != nil {
		result["authorization_error"] = safeError(err)
		result["requests"] = observations
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	ctx = core.WithUserIdentity(ctx, func(ctx context.Context) (string, error) {
		return svc.UserToken(ctx, user.ID, connection.AppID, connection.OpenID)
	})
	client := core.NewClient(&core.Config{AppID: connection.AppID})
	var imports []types.FeishuPersonalImport
	if err := db.Where("user_id = ? AND resource_type = ?", user.ID, "docx").Find(&imports).Error; err != nil {
		return errors.New("import_inventory_unavailable")
	}
	var documents []map[string]any
	var batchItems []struct {
		ID       string
		ImportID string
	}
	if db.Table("feishu_import_batch_items").Select("id, import_id").Where("import_id IN (?)",
		db.Model(&types.FeishuPersonalImport{}).
			Select("id").
			Where("user_id = ?", user.ID),
	).
		Find(&batchItems).Error != nil {
		return errors.New("original_batch_item_unavailable")
	}
	failedImport := ""
	for _, item := range batchItems {
		if mask(item.ID) == "7de6370f39d3" {
			failedImport = item.ImportID
		}
	}
	if failedImport == "" {
		return errors.New("reported_batch_item_missing")
	}
	for _, item := range imports {
		// Reproduce only the user-reported failed source and one proven successful
		// comparison source in test001. No folder or Wiki-space discovery occurs.
		if mask(item.KnowledgeID) != "070925df4104" && item.ID != failedImport {
			continue
		}
		var kb types.KnowledgeBase
		if db.Where("id = ? AND name = ? AND deleted_at IS NULL", item.KBID, "test001").
			First(&kb).
			Error != nil {
			continue
		}
		start := time.Now()
		before := len(observations)
		doc := map[string]any{
			"import_hash":     mask(item.ID),
			"previous_status": item.Status,
			"knowledge_hash":  mask(item.KnowledgeID),
		}
		resource, fetchErr := core.ParsePersonalResourceURL(item.URL)
		if fetchErr == nil {
			resource, fetchErr = client.ResolvePersonalResource(ctx, resource)
		}
		doc["metadata_readable"] = fetchErr == nil
		if fetchErr == nil {
			policy, permissionErr := client.PersonalExportPolicy(ctx, resource)
			if permissionErr != nil {
				doc["policy_inspection_error"] = safeError(permissionErr)
			} else {
				doc["security_entity"] = policy
			}
			var data []byte
			data, _, fetchErr = client.ExportAndDownload(ctx, resource.Token, resource.Type)
			if fetchErr == nil {
				doc["export_bytes"] = len(data)
				doc["export_sha256_prefix"] = mask(string(data))
			}
		}
		if fetchErr != nil {
			doc["error"] = safeError(fetchErr)
		}
		doc["export_succeeded"], doc["duration_ms"], doc["requests"] = fetchErr == nil, time.Since(start).
			Milliseconds(),
			append(
				[]core.APIObservation(nil),
				observations[before:]...)
		documents = append(documents, doc)
	}
	result["requests"], result["documents"] = observations, documents
	return json.NewEncoder(os.Stdout).Encode(result)
}

func safeError(err error) any {
	var api *core.APIError
	if errors.As(err, &api) {
		return core.APIObservation{
			Operation: api.Operation, Category: api.Category,
			HTTPStatus: api.HTTPStatus, Code: api.Code, RequiredScopes: api.Scopes,
		}
	}
	for name, target := range map[string]error{
		"refresh_expired":  service.ErrFeishuRefreshExpired,
		"refresh_rejected": service.ErrFeishuRefreshRejected, "oauth_app_configuration": service.ErrFeishuOAuthConfiguration,
		"reconnect_required": service.ErrFeishuReconnect,
	} {
		if errors.Is(err, target) {
			return name
		}
	}
	return "diagnostic_request_failed"
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "feishu_diagnostic_failed")
		os.Exit(1)
	}
}
