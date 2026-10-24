package container

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/redis/go-redis/v9"
	"github.com/shirou/gopsutil/v3/disk"
)

// Monitoring is independent of queue permits: a disk warning does not freeze
// healthy sources globally. The file service enforces admission at each write.
// Samples expire so loss of the monitor cannot look like a fresh observation.
func monitorFeishuStorage(ctx context.Context, client *redis.Client, prefix string) {
	paths := []string{os.TempDir()}
	root := strings.TrimSpace(os.Getenv("LOCAL_STORAGE_BASE_DIR"))
	if root == "" {
		root = "/data/files"
	}
	if root != paths[0] {
		paths = append(paths, root)
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	previous := ""
	for {
		free := map[string]any{}
		state := "healthy"
		for i, path := range paths {
			label := "data"
			if i == 0 {
				label = "temporary"
			}
			v, err := disk.UsageWithContext(ctx, path)
			if err != nil {
				state = "unknown"
				free[label] = nil
			} else {
				free[label] = v.Free
				if v.Free < 4<<30 && state == "healthy" {
					state = "low"
				}
			}
		}
		raw, _ := json.Marshal(map[string]any{
			"observed_utc": time.Now().UTC(), "state": state, "free_bytes": free, "automatic_deletes": false,
		})
		check, cancel := context.WithTimeout(ctx, time.Second)
		err := client.Set(check, prefix+"storage", raw, 90*time.Second).Err()
		if state != previous || err != nil {
			logger.Warnf(ctx, "feishu_storage_capacity state=%s sample_persisted=%v automatic_deletes=false",
				state, err == nil)
			if err == nil {
				err = client.XAdd(check, &redis.XAddArgs{
					Stream: prefix + "events", MaxLen: 1000, Approx: true,
					Values: map[string]any{"kind": "feishu_storage_capacity", "state": state},
				}).Err()
			}
			if err == nil {
				previous = state
			}
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
