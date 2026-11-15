package docparser

import (
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/infrastructure/docparser/anydoc"
	"github.com/stretchr/testify/require"
)

func TestScopedNativeAssetResourceLimitCannotPublishTextFallback(t *testing.T) {
	converted := &anydoc.Result{Markdown: "incomplete image-bearing content", AssetsError: anydoc.ErrResourceLimit}
	result, err := materializeAnydocResult(feishusafety.WithScope(t.Context(), "source"),
		converted, "docx", "synthetic.docx")
	require.Nil(t, result)
	require.ErrorIs(t, err, anydoc.ErrResourceLimit)
	converted.AssetsError = errors.New("unclassified converter failure")
	result, err = materializeAnydocResult(feishusafety.WithScope(t.Context(), "source"),
		converted, "docx", "synthetic.docx")
	require.Nil(t, result)
	require.Error(t, err)
	require.NotErrorIs(t, err, anydoc.ErrResourceLimit)
}
