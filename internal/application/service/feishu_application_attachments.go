package service

import (
	"bytes"
	"context"
	"fmt"
	"image"
	// Register the GIF decoder used to inspect Feishu image attachments.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
	"github.com/Tencent/WeKnora/internal/types"
)

// Reuse the existing reader/model clients synchronously before durable manifest
// acceptance. No second image queue or best-effort multimodal completion is
// introduced: a missing/unreadable attachment fails the entire candidate.
func (p *PersonalRecovery) applicationAttachmentContext(
	ctx context.Context,
	m *types.FeishuPersonalImport,
	a *types.FeishuPersonalAttempt,
) context.Context {
	cache := map[string]core.ApplicationAttachment{}
	return core.WithApplicationAttachmentProcessor(
		ctx,
		func(call context.Context, asset core.ApplicationAttachment) (core.ApplicationAttachment, error) {
			if _, err := p.authorize(call, m, a); err != nil {
				return asset, err
			}
			key := strings.Join(
				[]string{asset.TableID, asset.FieldID, asset.RecordID, asset.Token, asset.SHA256},
				"\x00",
			)
			if saved, ok := cache[key]; ok {
				return saved, nil
			}
			if len(cache) >= 128 {
				return asset, core.ErrApplicationTableIncomplete
			}
			ks := p.service.knowledge.(*knowledgeService)
			ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(asset.Name), "."))
			switch ext {
			case "png", "jpg", "jpeg", "gif":
				config, _, err := image.DecodeConfig(bytes.NewReader(asset.Data))
				if err != nil || config.Width < 1 || config.Height < 1 ||
					config.Width > 16000000/config.Height {
					return asset, core.ErrApplicationTableIncomplete
				}
				kb, err := ks.kbService.GetKnowledgeBaseByID(call, m.KBID)
				if err != nil {
					return asset, err
				}
				if !kb.VLMConfig.IsEnabled() || kb.VLMConfig.ModelID == "" || ks.modelService == nil {
					return asset, core.ErrApplicationTableMedia
				}
				model, err := ks.modelService.GetVLMModel(call, kb.VLMConfig.ModelID)
				if err != nil {
					return asset, err
				}
				ocr, err := model.Predict(call, [][]byte{asset.Data}, buildVLMOCRPrompt("", kb.VLMConfig))
				if err != nil {
					return asset, err
				}
				caption, err := model.Predict(
					call,
					[][]byte{asset.Data},
					buildVLMCaptionPrompt(call, kb.VLMConfig),
				)
				if err != nil {
					return asset, err
				}
				caption = strings.TrimSpace(caption)
				if caption == "" {
					return asset, core.ErrApplicationTableIncomplete
				}
				asset.Text = fmt.Sprintf("Image description: %s\nOCR: %s", caption, sanitizeOCRText(ocr))
				asset.Parser = "existing_vlm:" + model.GetModelID()
			case "txt", "md", "csv", "json":
				if !utf8.Valid(asset.Data) {
					return asset, core.ErrApplicationTableIncomplete
				}
				request := &types.ReadRequest{FileType: ext, FileName: asset.Name, FileContent: asset.Data}
				result, err := (&docparser.SimpleFormatReader{}).Read(call, request)
				asset.Parser = "existing_simple"
				if err != nil || result == nil || len(result.ImageRefs) > 0 ||
					strings.TrimSpace(result.MarkdownContent) == "" {
					return asset, core.ErrApplicationTableMedia
				}
				asset.Text = result.MarkdownContent
			default:
				// A nonempty PDF/Office conversion alone cannot prove attachment
				// completeness. Keep those formats closed until independently verified.
				return asset, core.ErrApplicationTableMedia
			}
			if len(asset.Text) > 4<<20 {
				return asset, core.ErrApplicationTableIncomplete
			}
			cache[key] = asset
			return asset, nil
		},
	)
}
