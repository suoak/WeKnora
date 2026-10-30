package container

import (
	"context"
	"os"

	"github.com/Tencent/WeKnora/internal/feishusynccontrol"
	"gorm.io/gorm"
)

// An absent key leaves operational recovery disabled. This never grants an
// administrator source permissions, changes rollout scope or resumes a queue.
func configureFeishuRecovery(db *gorm.DB, c *feishusynccontrol.Controller) error {
	feishusynccontrol.InstallRecovery(nil)
	path := os.Getenv("FEISHU_SYNC_RECOVERY_APPROVAL_KEY_FILE")
	if path == "" {
		return nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return feishusynccontrol.ErrRecoveryRejected
	}
	key, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	m, err := feishusynccontrol.NewRecoveryManager(
		c,
		key,
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
