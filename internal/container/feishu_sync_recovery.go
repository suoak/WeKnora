package container

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"os"

	"github.com/Tencent/WeKnora/internal/feishusynccontrol"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// An absent key leaves operational recovery disabled. This never grants an
// administrator source permissions, changes rollout scope or resumes a queue.
func configureFeishuRecovery(db *gorm.DB, c *feishusynccontrol.Controller) error {
	feishusynccontrol.InstallRecovery(nil)
	// Startup/restart never inherits an open continuation lane from a previous
	// process incarnation. A fresh signed approval must reopen only this lane.
	if types.ApprovedSyncLaneEnabled() {
		if err := c.Client.SetNX(context.Background(), "asynq:{sync}:paused", "approved_sync_frozen", 0).Err(); err != nil {
			return err
		}
		if err := c.Client.Set(context.Background(), "asynq:{approved_sync}:paused", "approved_sync_frozen", 0).Err(); err != nil {
			return err
		}
	}
	c.ApprovedConsumers = feishusynccontrol.ApprovedConsumers(c.Client)
	if os.Getenv("FEISHU_SYNC_RECOVERY_APPROVAL_KEY_FILE") != "" {
		return feishusynccontrol.ErrRecoveryRejected
	}
	path := os.Getenv("FEISHU_SYNC_RECOVERY_APPROVAL_PUBLIC_KEY_FILE")
	if path == "" {
		if types.ApprovedSyncLaneEnabled() {
			return feishusynccontrol.ErrRecoveryRejected
		}
		return nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > 4096 {
		return feishusynccontrol.ErrRecoveryRejected
	}
	key, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	block, rest := pem.Decode(key)
	if block == nil || block.Type != "PUBLIC KEY" || len(bytes.TrimSpace(rest)) != 0 {
		return feishusynccontrol.ErrRecoveryRejected
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return feishusynccontrol.ErrRecoveryRejected
	}
	public, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return feishusynccontrol.ErrRecoveryRejected
	}
	m, err := feishusynccontrol.NewRecoveryManager(
		c,
		public,
		func(ctx context.Context, id string) (feishusynccontrol.RecoveryFacts, error) {
			return feishusynccontrol.RecoveryFactsFromDatabase(ctx, db, id)
		},
	)
	if err != nil {
		return err
	}
	feishusynccontrol.InstallRecovery(m)
	return nil
}
