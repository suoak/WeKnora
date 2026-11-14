package docparser

import (
	"encoding/binary"
	"testing"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/infrastructure/docparser/anydoc"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestScopedPPTXEMFRequiresCompleteRaster(t *testing.T) {
	emf := make([]byte, 108)
	binary.LittleEndian.PutUint32(emf, 1)
	copy(emf[40:], " EMF")
	makeResult := func() *anydoc.Result {
		return &anydoc.Result{Markdown: "before ![plot](images/plot.bin) after", Assets: []anydoc.Asset{
			{Name: "plot.bin", MediaType: "image/x-emf", Data: emf},
		}}
	}
	reader := &stubDocReader{result: &types.ReadResult{ImageRefs: []types.ImageRef{{ImageData: candidatePNG(t)}}}}
	r := NewAnydocReader(nil, reader)
	ctx := feishusafety.WithScope(t.Context(), "controlled-source")
	parsed := makeResult()
	require.NoError(t, r.renderEmbeddedEMF(ctx, parsed, "pptx"))
	require.Equal(t, "before ![plot](images/plot.bin) after", parsed.Markdown)
	require.Equal(t, "plot.bin", parsed.Assets[0].Name)
	require.Equal(t, "image/png", parsed.Assets[0].MediaType)
	require.Equal(t, emf, reader.got.FileContent)
	require.Equal(t, "emf", reader.got.FileType)
	for _, refs := range [][]types.ImageRef{nil, {{ImageData: []byte("incomplete")}}, {{}, {}}} {
		reader.result.ImageRefs = refs
		parsed = makeResult()
		require.ErrorIs(t, r.renderEmbeddedEMF(ctx, parsed, "pptx"), ErrCandidateImageData)
		require.Equal(t, emf, parsed.Assets[0].Data, "failed conversion must not partially replace assets")
	}
	reader.got = nil
	require.NoError(t, r.renderEmbeddedEMF(t.Context(), makeResult(), "pptx"))
	require.Nil(t, reader.got, "non-Feishu parsing retains its existing behavior")
	r.fallback = nil
	require.ErrorIs(t, r.renderEmbeddedEMF(ctx, makeResult(), "pptx"), ErrCandidateImageData)
}
