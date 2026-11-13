package docparser

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type wordMLConverter struct {
	result *types.ReadResult
}

func (f wordMLConverter) Read(context.Context, *types.ReadRequest) (*types.ReadResult, error) {
	return f.result, nil
}

func TestScopedWordMLRequiresBoundConvertedPackage(t *testing.T) {
	source := []byte(`<wordDocument xmlns="` + wordMLNamespace + `"><body/></wordDocument>`)
	require.True(t, isWordML(source))
	require.False(t, isWordML([]byte(`<html/>`)))
	require.False(t, isWordML([]byte(`<!DOCTYPE wordDocument>`+string(source))))
	ctx := feishusafety.WithScope(t.Context(), "diagnostic")
	req := &types.ReadRequest{FileType: "doc", FileName: "source.doc", FileContent: source}
	reader := NewAnydocReader(nil, nil)
	_, handled, err := reader.readWordML(ctx, req, "doc")
	require.True(t, handled)
	require.ErrorContains(t, err, "unavailable")
	_, handled, err = reader.readWordML(t.Context(), req, "doc")
	require.False(t, handled)
	require.NoError(t, err)
	for _, converted := range []*types.ReadResult{
		nil, {Error: "failed"}, {Metadata: map[string]string{"wordml_docx_base64": "AAAA"}},
	} {
		reader.fallback = wordMLConverter{result: converted}
		_, handled, err = reader.readWordML(ctx, req, "doc")
		require.True(t, handled)
		require.Error(t, err)
	}
	require.False(t, isWordML([]byte(strings.Repeat(" ", 8<<20+1))))
}
