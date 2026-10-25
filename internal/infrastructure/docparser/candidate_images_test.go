package docparser

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type candidateFiles struct {
	interfaces.FileService
	calls int
	fail  bool
}

func (f *candidateFiles) SaveBytes(_ context.Context, _ []byte, _ uint64, _ string, _ bool) (string, error) {
	f.calls++
	if f.fail {
		return "", errors.New("upload failed")
	}
	return "local://1/candidate/verified.png", nil
}

func candidatePNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 80, 80))))
	return b.Bytes()
}

func TestCandidateImagesExactUnicodeSpans(t *testing.T) {
	first := "中文 ![图](images/a.png) 后文\n"
	second := "<img src=\"images/a.png\">"
	firstEnd := utf8.RuneCountInString(first)
	r := &types.ReadResult{
		MarkdownContent: first + second,
		ImageRefs:       []types.ImageRef{{OriginalRef: "images/a.png", ImageData: candidatePNG(t)}},
		ParsedSegments: []types.ParserDefinedSegment{
			{Start: 0, End: firstEnd, ParsedChunks: []types.ParserChunkSpan{{Start: 0, End: firstEnd}}},
			{Start: utf8.RuneCountInString(first), End: utf8.RuneCountInString(first + second)},
		}, SourceBlocks: []types.SourceBlock{{Start: 0, End: utf8.RuneCountInString(first)}},
	}
	f := &candidateFiles{}
	images, err := ResolveCandidateImages(context.Background(), r, f, 1)
	require.NoError(t, err)
	require.Len(t, images, 1)
	require.Equal(t, 1, f.calls)
	expected := strings.ReplaceAll(first+second, "images/a.png", "local://1/candidate/verified.png")
	require.Equal(t, expected, r.MarkdownContent)
	boundary := utf8.RuneCountInString(strings.ReplaceAll(first, "images/a.png", "local://1/candidate/verified.png"))
	require.Equal(t, boundary, r.ParsedSegments[0].End)
	require.Equal(t, boundary, r.ParsedSegments[0].ParsedChunks[0].End)
	require.Equal(t, boundary, r.ParsedSegments[1].Start)
	require.Equal(t, boundary, r.SourceBlocks[0].End)
}

func TestCandidateImagesFailureNeverFallsBackOrFetches(t *testing.T) {
	imageResult := types.ReadResult{
		MarkdownContent: "![x](a.png)", ImageRefs: []types.ImageRef{{OriginalRef: "a.png", ImageData: candidatePNG(t)}},
	}
	brokenSpan := imageResult
	brokenSpan.ParsedChunks = []types.ParserChunkSpan{{Start: 0, End: 7}}
	for _, tc := range []struct {
		name   string
		result types.ReadResult
		files  candidateFiles
	}{
		{"upload", imageResult, candidateFiles{fail: true}},
		{"remote", types.ReadResult{MarkdownContent: "![x](http://169.254.169.254/a.png)"}, candidateFiles{}},
		{"missing bytes", types.ReadResult{
			MarkdownContent: "![x](a.png)", ImageRefs: []types.ImageRef{{OriginalRef: "a.png"}},
		}, candidateFiles{}},
		{"ambiguous span", brokenSpan, candidateFiles{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := tc.result.MarkdownContent
			_, err := ResolveCandidateImages(context.Background(), &tc.result, &tc.files, 1)
			require.Error(t, err)
			require.Equal(t, before, tc.result.MarkdownContent)
		})
	}
}
