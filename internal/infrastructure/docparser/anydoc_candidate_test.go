package docparser

import (
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/infrastructure/docparser/anydoc"
	"github.com/stretchr/testify/require"
)

func TestCandidateImagesRejectNativeTextFallback(t *testing.T) {
	// This is the native binding's real fallback contract: successful text
	// conversion with an AssetsError instead of an overall conversion error.
	converted := &anydoc.Result{Markdown: "remaining text", AssetsError: errors.New("private parser detail")}
	ctx := feishusafety.WithScope(t.Context(), "controlled-source")
	result, err := materializeAnydocResult(ctx, converted, "docx", "controlled.docx")
	require.ErrorIs(t, err, ErrCandidateImageData)
	require.Nil(t, result, "the lossy result must not reach chunking or publication")
	require.NotContains(t, err.Error(), "private parser detail")
	result, err = materializeAnydocResult(t.Context(), converted, "docx", "controlled.docx")
	require.NoError(t, err, "ordinary reader fallback remains compatible")
	require.Equal(t, "remaining text", result.MarkdownContent)
}

func TestCandidateImagesKeepSuccessfulNativeAssets(t *testing.T) {
	converted := &anydoc.Result{Markdown: "![fixture](images/image-1.png)", Assets: []anydoc.Asset{{
		Name: "image-1.png", MediaType: "image/png", Data: []byte("controlled bytes"),
	}}}
	ctx := feishusafety.WithScope(t.Context(), "controlled-source")
	result, err := materializeAnydocResult(ctx, converted, "docx", "controlled.docx")
	require.NoError(t, err)
	require.Len(t, result.ImageRefs, 1)
	require.Equal(t, converted.Assets[0].Data, result.ImageRefs[0].ImageData)
	require.Equal(t, converted.Markdown, result.MarkdownContent)
}
