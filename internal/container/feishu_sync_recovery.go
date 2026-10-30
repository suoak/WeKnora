package container

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"os"

	"github.com/Tencent/WeKnora/internal/feishusynccontrol"
	"gorm.io/gorm"
)

// An absent key leaves operational recovery disabled. This never grants an
// administrator source permissions, changes rollout scope or resumes a queue.
func configureFeishuRecovery(db *gorm.DB, c *feishusynccontrol.Controller) error {
	feishusynccontrol.InstallRecovery(nil)
	if os.Getenv("FEISHU_SYNC_RECOVERY_APPROVAL_KEY_FILE") != "" {
		return feishusynccontrol.ErrRecoveryRejected
	}
	path := os.Getenv("FEISHU_SYNC_RECOVERY_APPROVAL_PUBLIC_KEY_FILE")
	if path == "" {
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
