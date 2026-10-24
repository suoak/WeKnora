package file

import (
	"context"
	"os"
	"strconv"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/shirou/gopsutil/v3/disk"
)

// Feishu-only admission, not a promise of an exclusive filesystem reservation.
// Keep a floor for PostgreSQL/online work; unknown disk state fails closed.
func (s *localFileService) CheckStagingCapacity(ctx context.Context, bytes int64) error {
	floor := uint64(2 << 30)
	if raw := os.Getenv("FEISHU_STAGING_MIN_FREE_BYTES"); raw != "" {
		v, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || v < 128<<20 {
			return types.ErrFeishuStagingCapacity
		}
		floor = v
	}
	info, err := disk.UsageWithContext(ctx, s.baseDir)
	if err != nil || bytes < 0 || uint64(bytes) > (^uint64(0)-floor)/3 {
		return types.ErrFeishuStagingCapacity
	}
	if info.Free < floor+3*uint64(bytes) {
		return types.ErrFeishuStagingCapacity
	}
	return nil
}

func checkInnerStagingCapacity(ctx context.Context, inner interfaces.FileService, bytes int64) error {
	if guard, ok := inner.(interface {
		CheckStagingCapacity(context.Context, int64) error
	}); ok {
		return guard.CheckStagingCapacity(ctx, bytes)
	}
	return types.ErrFeishuStagingCapacity
}

func (s *resourceCatalogFileService) CheckStagingCapacity(ctx context.Context, bytes int64) error {
	return checkInnerStagingCapacity(ctx, s.inner, bytes)
}

func (s *backendScopedFileService) CheckStagingCapacity(ctx context.Context, bytes int64) error {
	return checkInnerStagingCapacity(ctx, s.inner, bytes)
}
