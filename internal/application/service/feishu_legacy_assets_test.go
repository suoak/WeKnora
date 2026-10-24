package service

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	files "github.com/Tencent/WeKnora/internal/application/service/file"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type corruptAssetFiles struct{ interfaces.FileService }

func (c corruptAssetFiles) CheckStagingCapacity(ctx context.Context, n int64) error {
	return legacyStagingCapacity(ctx, c.FileService, n)
}

func (c corruptAssetFiles) GetFile(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("corrupt")), nil
}

func TestLegacyAssetFilesPrivateDedupAndReadback(t *testing.T) {
	ctx := context.Background()
	inner := files.NewLocalFileService(t.TempDir(), "")
	renewed := 0
	f := &legacyAssetFiles{
		FileService: inner, tenant: 123, attempt: "candidate-uuid",
		renew: func(context.Context) error { renewed++; return nil },
	}
	p, err := f.SaveBytes(ctx, []byte("image bytes"), 123, "image.png", false)
	require.NoError(t, err)
	require.Contains(t, p, "/123/candidate-uuid/")
	p2, err := f.SaveBytes(ctx, []byte("image bytes"), 123, "same.png", false)
	require.NoError(t, err)
	require.Equal(t, p, p2)
	require.Len(t, f.assets, 1)
	require.Positive(t, renewed)
	_, err = f.SaveBytes(ctx, []byte("other bytes"), 124, "other.png", false)
	require.Error(t, err)
	require.Len(t, f.assets, 1)
	bad := &legacyAssetFiles{
		FileService: corruptAssetFiles{inner}, tenant: 123, attempt: "bad-readback",
		renew: func(context.Context) error { return nil },
	}
	_, err = bad.SaveBytes(ctx, []byte("original"), 123, "a.png", false)
	require.ErrorContains(t, err, "readback mismatch")
	require.Empty(t, bad.assets)
	bad = &legacyAssetFiles{
		FileService: assetUploadFault{inner}, tenant: 123, attempt: "upload-fault",
		renew: func(context.Context) error { return nil },
	}
	_, err = bad.SaveBytes(ctx, []byte("original"), 123, "a.png", false)
	require.ErrorContains(t, err, "upload fault")
	require.Empty(t, bad.assets)
	f.renew = func(context.Context) error { return errors.New("expired owner") }
	_, err = f.SaveBytes(ctx, []byte("new"), 123, "b.png", false)
	require.ErrorContains(t, err, "expired owner")
	require.Len(t, f.assets, 1)
}

func TestLegacyAssetCapacityClosed(t *testing.T) {
	f := files.NewLocalFileService(t.TempDir(), "")
	ctx := context.Background()
	for _, limit := range []string{"not-a-number", "0", "18446744073709551615"} {
		t.Setenv("FEISHU_STAGING_MIN_FREE_BYTES", limit)
		require.ErrorIs(t, legacyStagingCapacity(ctx, f, 1024), types.ErrFeishuStagingCapacity)
	}
	t.Setenv("FEISHU_STAGING_MIN_FREE_BYTES", "")
	require.NoError(t, legacyStagingCapacity(ctx, f, 1))
	ds := &types.DataSource{Type: "feishu"}
	item := &types.FetchedItem{ExternalID: "resource"}
	t.Setenv("FEISHU_SAFE_INCREMENTAL_ENABLED", "true")
	require.Equal(t, "safe_staging_capacity_insufficient",
		feishuDocumentFailureCode(ds, item, types.ErrFeishuStagingCapacity))
}
