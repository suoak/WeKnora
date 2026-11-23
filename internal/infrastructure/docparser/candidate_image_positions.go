package docparser

import (
	"context"
	"sort"
	"unicode/utf8"
)

type candidateImageRuneEdit struct {
	start, end, delta int
}

// Scanner-produced edit boundaries are UTF-8 byte boundaries. Count their rune
// positions in one forward pass, then translate each parser boundary with a
// binary search. Recounting every whole-document prefix for every SourceBlock
// multiplied document length, image count and parser boundary count.
func candidateImagePositions(ctx context.Context, before string, edits []candidateImageEdit) (
	func(int) (int, error), error,
) {
	bounds := make([]candidateImageRuneEdit, 0, len(edits))
	byteCursor, runeCursor, delta := 0, 0, 0
	for _, edit := range edits {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if edit.start < byteCursor || edit.end < edit.start || edit.end > len(before) {
			return nil, candidateImageInvalid("overlapping candidate image references")
		}
		start := runeCursor + utf8.RuneCountInString(before[byteCursor:edit.start])
		end := start + utf8.RuneCountInString(before[edit.start:edit.end])
		delta += utf8.RuneCountInString(edit.replacement) - (end - start)
		bounds = append(bounds, candidateImageRuneEdit{start: start, end: end, delta: delta})
		byteCursor, runeCursor = edit.end, end
	}
	return func(offset int) (int, error) {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		index := sort.Search(len(bounds), func(i int) bool { return bounds[i].end > offset })
		if index < len(bounds) && offset > bounds[index].start {
			return 0, candidateImageInvalid("parser boundary inside image reference")
		}
		if index > 0 {
			offset += bounds[index-1].delta
		}
		return offset, nil
	}, nil
}
