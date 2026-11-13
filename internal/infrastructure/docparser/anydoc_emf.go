package docparser

import (
	"bytes"
	"context"
	"encoding/binary"
	"image/png"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/infrastructure/docparser/anydoc"
	"github.com/Tencent/WeKnora/internal/types"
)

// renderEmbeddedEMF changes only parser-issued EMF bytes, retaining each exact
// placement and all other assets. Unknown formats and renderer errors refuse
// the candidate; the original package remains the source snapshot.
func (r *AnydocReader) renderEmbeddedEMF(ctx context.Context, result *anydoc.Result, format string) error {
	if format != "pptx" || !feishusafety.Scoped(ctx) || result == nil || result.AssetsError != nil {
		return nil
	}
	assets := append([]anydoc.Asset(nil), result.Assets...)
	count := 0
	for i, asset := range assets {
		data := asset.Data
		if len(data) < 44 || binary.LittleEndian.Uint32(data[:4]) != 1 || string(data[40:44]) != " EMF" {
			continue
		}
		count++
		if count > 8 || len(data) > 8<<20 || !r.canFallback() {
			return candidateImageInvalid("embedded EMF renderer unavailable or outside bounds")
		}
		call, cancel := context.WithTimeout(ctx, 40*time.Second)
		converted, err := r.fallback.Read(call, &types.ReadRequest{
			FileContent: data, FileType: "emf", FileName: "embedded.emf", ParserEngine: BuiltinEngineName,
		})
		cancel()
		if err != nil || converted == nil || converted.Error != "" || len(converted.ImageRefs) != 1 {
			return candidateImageInvalid("embedded EMF rasterization failed")
		}
		image := converted.ImageRefs[0].ImageData
		if len(image) == 0 || len(image) > 32<<20 {
			return candidateImageInvalid("embedded EMF raster size invalid")
		}
		config, err := png.DecodeConfig(bytes.NewReader(image))
		if err != nil || config.Width <= 0 || config.Height <= 0 ||
			int64(config.Width)*int64(config.Height) > 16<<20 {
			return candidateImageInvalid("embedded EMF raster invalid")
		}
		if _, err := png.Decode(bytes.NewReader(image)); err != nil {
			return candidateImageInvalid("embedded EMF raster incomplete")
		}
		assets[i].Data, assets[i].MediaType = image, "image/png"
	}
	result.Assets = assets
	return nil
}
