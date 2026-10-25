package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/vlm"
	"github.com/Tencent/WeKnora/internal/searchutil"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
)

// The adapter deliberately overrides SaveBytes: ordinary image storage is not
// attempt-scoped. SaveFile's owner is the candidate UUID, never the stable KID.
type legacyAssetFiles struct {
	interfaces.FileService
	tenant  uint64
	attempt string
	renew   func(context.Context) error
	assets  []types.FeishuLegacyAsset
	byHash  map[string]string
}

func (f *legacyAssetFiles) SaveBytes(ctx context.Context, data []byte, tenant uint64,
	name string, temp bool,
) (string, error) {
	if tenant != f.tenant || temp {
		return "", errors.New("candidate asset ownership invalid")
	}
	if err := f.renew(ctx); err != nil {
		return "", err
	}
	if err := legacyStagingCapacity(ctx, f.FileService, int64(len(data))); err != nil {
		return "", err
	}
	h := sha256.Sum256(data)
	digest := hex.EncodeToString(h[:])
	if path := f.byHash[digest]; path != "" {
		return path, nil
	}
	form, file, err := feishuFile(data, name)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := form.RemoveAll(); err != nil {
			logger.Warnf(ctx, "Feishu candidate temporary file cleanup failed: %v", err)
		}
	}()
	path, err := f.SaveFile(ctx, file, tenant, f.attempt)
	if err != nil {
		return "", err
	}
	if err = f.verify(ctx, path, digest, int64(len(data))); err != nil {
		return "", err
	}
	if err = f.renew(ctx); err != nil {
		return "", err
	}
	if f.byHash == nil {
		f.byHash = map[string]string{}
	}
	f.byHash[digest] = path
	f.assets = append(f.assets, types.FeishuLegacyAsset{
		Path: path, SHA256: digest, Bytes: int64(len(data)), MimeType: http.DetectContentType(data),
	})
	return path, nil
}

func legacyStagingCapacity(ctx context.Context, files interfaces.FileService, bytes int64) error {
	if guard, ok := files.(interface {
		CheckStagingCapacity(context.Context, int64) error
	}); ok {
		return guard.CheckStagingCapacity(ctx, bytes)
	}
	return types.ErrFeishuStagingCapacity
}

func (f *legacyAssetFiles) verify(ctx context.Context, path, digest string, size int64) (err error) {
	reader, err := f.GetFile(ctx, path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, reader.Close()) }()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(reader, size+1))
	if err != nil {
		return err
	}
	if n != size || hex.EncodeToString(h.Sum(nil)) != digest {
		return errors.New("candidate asset readback mismatch")
	}
	if local, ok := reader.(interface{ Sync() error }); ok {
		if err := local.Sync(); err != nil {
			return err
		}
	}
	return nil
}

// Enrichment reuses the ordinary action selection, observation and OCR policy,
// but creates no public rows and dispatches no asynchronous append jobs.
func (s *knowledgeService) enrichLegacyImages(ctx context.Context, ds *types.DataSource, kb *types.KnowledgeBase,
	eff types.EffectiveProcessConfig, store *repository.FeishuLegacyStore, attempt *types.FeishuLegacyAttempt,
	files *legacyAssetFiles, images []docparser.StoredImage, chunks []*types.Chunk,
) ([]*types.Chunk, error) {
	var model vlm.VLM
	if eff.EnableMultimodel && len(images) > 0 {
		var err error
		if eff.VLMConfig.ModelID != "" {
			model, err = s.modelService.GetVLMModel(ctx, eff.VLMConfig.ModelID)
		} else {
			model, err = vlm.NewVLMFromLegacyConfig(eff.VLMConfig, nil)
		}
		if err != nil {
			return nil, err
		}
	}
	unique := map[string]bool{}
	for _, img := range images {
		if unique[img.ServingURL] {
			continue
		}
		unique[img.ServingURL] = true
		if err := store.Renew(ctx, ds, attempt); err != nil {
			return nil, err
		}
		reader, err := files.GetFile(ctx, img.ServingURL)
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, 32<<20))
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			return nil, errors.Join(readErr, closeErr)
		}
		info := types.ImageInfo{
			URL: img.ServingURL, OriginalURL: img.ServingURL, SHA256: fmt.Sprintf("%x", sha256.Sum256(data)),
		}
		var owners []*types.Chunk
		for _, c := range chunks {
			if c.ChunkType == types.ChunkTypeText && searchutil.ImageURLsInContent(c.Content)[img.ServingURL] {
				owners = append(owners, c)
			}
		}
		if len(owners) == 0 {
			return nil, errors.New("candidate image has no text owner")
		}
		if model != nil {
			payload := &types.ImageMultimodalPayload{
				EnableOCR: true, EnableCaption: true,
				ImageAttrsEnabled: eff.ImageAttrsEnabled, ImageActions: eff.ImageActions,
			}
			out := types.JSONMap{}
			plan := selectImageActionRounds(payload)
			processor := &ImageMultimodalService{}
			for i := 0; i < len(plan); i++ {
				if err := store.Renew(ctx, ds, attempt); err != nil {
					return nil, err
				}
				call, cancel := context.WithTimeout(ctx, 90*time.Second)
				processor.executeActionRound(call, payload, model, data, eff.VLMConfig, &info, out, plan[i])
				cancel()
				if out["caption_error"] != nil || out["ocr_error"] != nil {
					if err := ctx.Err(); err != nil {
						return nil, err
					}
					return nil, &feishuStagingRefusal{reason: "image_enrichment_failed"}
				}
				if err := store.Renew(ctx, ds, attempt); err != nil {
					return nil, err
				}
				if plan[i].Contains(types.ActionObservation) && DecideOCR(info.Attrs, payload.ImageActions) {
					plan = append(plan, ActionRound{Actions: []types.ActionKind{types.ActionOCR}})
				}
			}
		}
		encoded, err := json.Marshal([]types.ImageInfo{info})
		if err != nil {
			return nil, err
		}
		for _, owner := range owners {
			var infos []types.ImageInfo
			if owner.ImageInfo != "" {
				if err = json.Unmarshal([]byte(owner.ImageInfo), &infos); err != nil {
					return nil, err
				}
			}
			infos = append(infos, info)
			b, err := json.Marshal(infos)
			if err != nil {
				return nil, err
			}
			owner.ImageInfo = string(b)
		}
		// Match the ordinary unique-owner rule. An ambiguous image still has
		// rendering metadata but does not attach evidence to an arbitrary text.
		parent := ""
		if len(owners) == 1 {
			parent = owners[0].ID
		}
		for _, entry := range []struct {
			kind types.ChunkType
			text string
		}{{types.ChunkTypeImageOCR, info.OCRText}, {types.ChunkTypeImageCaption, info.Caption}} {
			if strings.TrimSpace(entry.text) == "" {
				continue
			}
			chunks = append(chunks, &types.Chunk{
				ID: uuid.NewString(), TenantID: ds.TenantID, KnowledgeID: owners[0].KnowledgeID, KnowledgeBaseID: kb.ID,
				Content: entry.text, ChunkType: entry.kind, ParentChunkID: parent, ImageInfo: string(encoded),
				IsEnabled: true, Flags: types.ChunkFlagRecommended,
			})
		}
	}
	return chunks, nil
}
