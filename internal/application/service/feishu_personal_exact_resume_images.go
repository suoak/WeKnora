package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// Retained output must reuse exact owned image bytes and references. If no
// output exists, the normal checked image-storage path can create its assets.
type exactResumeImageFiles struct {
	interfaces.FileService
	consumer *exactPersonalConsumer
	used     map[string]bool
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
		return "", fmt.Errorf("%w: invalid retained image request", errPersonalExactDependencies)
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
	digest := sha256.Sum256(data)
	wanted := hex.EncodeToString(digest[:])
	var rows []types.StoredResource
	err := f.consumer.recovery.service.db.WithContext(ctx).Table("resources f").Select("f.*").
		Joins("JOIN resource_bindings b ON b.resource_id=f.id").
		Where(`f.tenant_id=? AND f.provider='local' AND f.state='active' AND f.deleted_at IS NULL
AND f.lifecycle='persistent' AND f.expires_at IS NULL AND f.size=?
AND (f.content_hash=? OR f.content_hash='')
AND b.tenant_id=f.tenant_id AND b.owner_type='knowledge' AND b.owner_id=? AND b.relation=?`,
			tenant, len(data), wanted, proof.knowledge, types.ResourceRelationExtractedImage).
		Limit(65).Scan(&rows).Error
	if err != nil {
		return "", err
	}
	if len(rows) > 64 {
		return "", fmt.Errorf("%w: retained image candidates exceed limit", errPersonalExactDependencies)
	}
	var matches []string
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
		matches = append(matches, types.BuildResourcePath(row.Handle))
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("%w: retained image bytes have no reference", errPersonalExactDependencies)
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return f.orderedReference(ctx, matches)
}

// Distinct package image references may contain identical bytes. Reconstruct
// their original occurrence order using numeric positions only, never adopting
// retained text. The complete freshly generated chunk comparison remains the
// authority: an incorrect URL substitution cannot pass reproveResumeChunks.
func (f *exactResumeImageFiles) orderedReference(ctx context.Context, refs []string) (string, error) {
	type reference struct {
		url      string
		position int64
	}
	ordered := make([]reference, 0, len(refs))
	proof := f.consumer.source
	for _, ref := range refs {
		var row struct{ Position *int64 }
		if err := f.consumer.recovery.service.db.WithContext(ctx).Raw(`
SELECT min(start_at+strpos(content,?)) AS position FROM chunks
WHERE tenant_id=? AND knowledge_id=? AND knowledge_base_id=? AND deleted_at IS NULL
AND chunk_type IN ('text','parent_text') AND strpos(content,?)>0`,
			ref, proof.tenant, proof.knowledge, proof.kb, ref).Scan(&row).Error; err != nil {
			return "", err
		}
		if row.Position == nil {
			// An earlier failed attempt can leave bound assets with no chunk
			// reference. They cannot supply a URL for retained output.
			continue
		}
		ordered = append(ordered, reference{url: ref, position: *row.Position})
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].position < ordered[j].position })
	for i, ref := range ordered {
		if i > 0 && ref.position == ordered[i-1].position {
			return "", fmt.Errorf("%w: retained image order is ambiguous", errPersonalExactDependencies)
		}
	}
	if f.used == nil {
		f.used = map[string]bool{}
	}
	for _, ref := range ordered {
		if !f.used[ref.url] {
			f.used[ref.url] = true
			return ref.url, nil
		}
	}
	return "", fmt.Errorf("%w: retained image references exhausted", errPersonalExactDependencies)
}
