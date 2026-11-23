package docparser

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// Large PDF readers can return thousands of source blocks and hundreds of
// image placements. Verify all positions and duplicate-image storage through
// the real resolver, without a timing assertion or dropping semantic spans.
func TestCandidateImagesManySourceBlocks(t *testing.T) {
	const placements, blocks = 300, 4000
	var prefix strings.Builder
	for range placements {
		prefix.WriteString(strings.Repeat("\u8def\u7531\u914d\u7f6e\u8bf4\u660e\n", 96))
		prefix.WriteString("![embedded](images/a.png)\n")
	}
	unit := "\u4fdd\u7559\u6b63\u6587\n"
	beforePrefix := prefix.String()
	before := beforePrefix + strings.Repeat(unit, blocks)
	base, width := utf8.RuneCountInString(beforePrefix), utf8.RuneCountInString(unit)
	r := &types.ReadResult{
		MarkdownContent: before,
		ImageRefs:       []types.ImageRef{{OriginalRef: "images/a.png", ImageData: candidatePNG(t)}},
		ParsedChunks:    []types.ParserChunkSpan{{Start: base, End: base + width}},
		ParsedSegments: []types.ParserDefinedSegment{{
			Start: base, End: base + blocks*width,
			ParsedChunks: []types.ParserChunkSpan{{Start: 0, End: width}},
		}},
	}
	for i := range blocks {
		r.SourceBlocks = append(r.SourceBlocks, types.SourceBlock{Start: base + i*width, End: base + (i+1)*width})
	}
	f := &candidateFiles{}
	images, err := ResolveCandidateImages(context.Background(), r, f, 1)
	require.NoError(t, err)
	require.Len(t, images, 1)
	require.Equal(t, 1, f.calls)
	wantPrefix := strings.ReplaceAll(beforePrefix, "images/a.png", "local://1/candidate/verified.png")
	wantBase := utf8.RuneCountInString(wantPrefix)
	require.Equal(t, wantPrefix+strings.Repeat(unit, blocks), r.MarkdownContent)
	require.Equal(t, wantBase, r.ParsedChunks[0].Start)
	require.Equal(t, wantBase+width, r.ParsedChunks[0].End)
	require.Equal(t, wantBase, r.ParsedSegments[0].Start)
	require.Equal(t, wantBase+blocks*width, r.ParsedSegments[0].End)
	require.Equal(t, 0, r.ParsedSegments[0].ParsedChunks[0].Start)
	require.Equal(t, width, r.ParsedSegments[0].ParsedChunks[0].End)
	for i, block := range r.SourceBlocks {
		require.Equal(t, wantBase+i*width, block.Start)
		require.Equal(t, wantBase+(i+1)*width, block.End)
	}
}

type cancelCandidateFiles struct {
	candidateFiles
	cancel context.CancelFunc
}

func (f *cancelCandidateFiles) SaveBytes(ctx context.Context, data []byte, tenant uint64,
	name string, temp bool,
) (string, error) {
	path, err := f.candidateFiles.SaveBytes(ctx, data, tenant, name, temp)
	f.cancel()
	return path, err
}

func TestCandidateImagesCancellationAfterUploadStopsRemapping(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	before := "\u4fdd\u7559 ![image](images/a.png) \u6b63\u6587"
	r := &types.ReadResult{
		MarkdownContent: before,
		ImageRefs:       []types.ImageRef{{OriginalRef: "images/a.png", ImageData: candidatePNG(t)}},
		SourceBlocks:    []types.SourceBlock{{Start: 0, End: utf8.RuneCountInString(before)}},
	}
	f := &cancelCandidateFiles{cancel: cancel}
	_, err := ResolveCandidateImages(ctx, r, f, 1)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, before, r.MarkdownContent)
}

func TestCandidateImageRuneBoundariesRetainExactRejection(t *testing.T) {
	before := "\u7532\U0001f642url\u4e59X\u4e19"
	first := strings.Index(before, "url")
	second := strings.Index(before, "X")
	position, err := candidateImagePositions(context.Background(), before, []candidateImageEdit{
		{start: first, end: first + len("url"), replacement: "\u66ff\u6362"},
		{start: second, end: second + 1, replacement: "abcdef"},
	})
	require.NoError(t, err)
	for input, want := range map[int]int{-1: -1, 0: 0, 2: 2, 5: 4, 6: 5, 7: 11, 8: 12, 99: 103} {
		got, err := position(input)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
	for _, input := range []int{3, 4} {
		_, err := position(input)
		require.ErrorIs(t, err, ErrCandidateImageData)
	}
}
