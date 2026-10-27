package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// Explicit operator diagnostics read only the two approved resources using
// their current personal identity. They do not enqueue or recover sync work.
func inspectPairMetadata(ctx context.Context, db *gorm.DB, svc *service.FeishuPersonalService, ids []string) error {
	var results []map[string]any
	for _, id := range ids {
		var item types.FeishuPersonalImport
		if err := db.Where("id = ?", id).First(&item).Error; err != nil {
			return err
		}
		owner := context.WithValue(ctx, types.UserIDContextKey, item.UserID)
		remote, client, ref, err := svc.ResolveCredentialReference(owner, types.FeishuUserOAuth, "")
		if err != nil || ref.OpenID != item.OpenID || ref.AppID != item.AppID {
			return errors.New("metadata_probe_identity_unconfirmed")
		}
		var observations []core.APIObservation
		remote = core.WithAPIObserver(
			remote,
			func(event core.APIObservation) { observations = append(observations, event) },
		)
		remote, cancel := context.WithTimeout(remote, 25*time.Second)
		started := time.Now()
		resource, parseErr := core.ParsePersonalResourceURL(item.URL)
		if parseErr == nil {
			_, err = client.ResolvePersonalResource(remote, resource)
		} else {
			err = parseErr
		}
		cancel()
		results = append(
			results,
			map[string]any{
				"source_hash":        mask(id),
				"succeeded":          err == nil,
				"elapsed_ms":         time.Since(started).Milliseconds(),
				"transport_category": core.TransportErrorCategory(err),
				"requests":           observations,
			},
		)
	}
	return json.NewEncoder(os.Stdout).
		Encode(map[string]any{"operation": "metadata-probe", "results": results, "knowledge_writes": false, "sync_control_writes": false})
}
