package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// Retained output must reuse exact owned image bytes and references. If no
// output exists, the normal checked image-storage path can create its assets.
type exactResumeImageFiles struct {
	interfaces.FileService
	consumer *exactPersonalConsumer
}

func (f *exactResumeImageFiles) SaveBytes(
	ctx context.Context,
	data []byte,
	tenant uint64,
	name string,
	temp bool,
) (string, error) {
	if f.consumer == nil || !f.consumer.resuming || temp || tenant != f.consumer.source.tenant ||
		len(data) == 0 {
		return "", errPersonalExactDependencies
	}
	if f.consumer.resumeEmpty {
		empty, err := f.consumer.resumeHasNoOutput(ctx)
		if err != nil {
			return "", err
		}
		if !empty {
			return "", errPersonalExactDocument
		}
		if _, err := f.consumer.recovery.revalidateExactPersonalDocument(ctx, f.consumer.source); err != nil {
			return "", err
		}
		return f.FileService.SaveBytes(ctx, data, tenant, name, false)
	}
	proof := f.consumer.source
	var rows []types.StoredResource
	err := f.consumer.recovery.service.db.WithContext(ctx).Table("resources f").Select("f.*").
		Joins("JOIN resource_bindings b ON b.resource_id=f.id").
		Where(`f.tenant_id=? AND f.provider='local' AND f.state='active' AND f.deleted_at IS NULL
AND f.lifecycle='persistent' AND f.expires_at IS NULL AND f.size=?
AND b.tenant_id=f.tenant_id AND b.owner_type='knowledge' AND b.owner_id=? AND b.relation=?`,
			tenant, len(data), proof.knowledge, types.ResourceRelationExtractedImage).
		Limit(65).Scan(&rows).Error
	if err != nil {
		return "", err
	}
	if len(rows) > 64 {
		return "", errPersonalExactDependencies
	}
	digest := sha256.Sum256(data)
	wanted := hex.EncodeToString(digest[:])
	matched := ""
	for _, row := range rows {
		if row.ContentHash != "" && row.ContentHash != wanted {
			continue
		}
		reader, err := f.GetFile(ctx, row.PhysicalPath)
		if err != nil {
			return "", err
		}
		stored, readErr := io.ReadAll(io.LimitReader(reader, int64(len(data))+1))
		if err := errors.Join(readErr, reader.Close()); err != nil {
			return "", err
		}
		if len(stored) != len(data) || sha256.Sum256(stored) != digest {
			continue
		}
		if matched != "" {
			return "", errPersonalExactDependencies
		}
		matched = types.BuildResourcePath(row.Handle)
	}
	if matched == "" {
		return "", errPersonalExactDependencies
	}
	return matched, nil
}
