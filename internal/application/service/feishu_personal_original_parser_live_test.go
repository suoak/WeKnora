//go:build anydoc && personal_exact_live

package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/infrastructure/docparser/anydoc"
	"github.com/stretchr/testify/require"
)

// The original file is a private, explicitly copied acceptance artifact. It is
// not committed or downloaded by CI. Report hashes/counts, never its text.
// Parser acceptance is deliberately distinct from protocol/index acceptance.
func TestPersonalOriginalExactDOCXParser(t *testing.T) {
	path := os.Getenv("PERSONAL_EXACT_DOCX_FIXTURE")
	require.NotEmpty(t, path, "explicit private fixture required; no production fallback")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	sum := sha256.Sum256(data)
	require.Equal(
		t,
		"58bc916ae4f29f6c1d39fd76913a150a2b312ab3d4e9a9680f83a74e48ae0ff9",
		hex.EncodeToString(sum[:]),
	)
	require.Len(t, data, 32799)
	require.True(t, anydoc.Available())
	result, err := anydoc.Convert(data, anydoc.Options{Format: "docx", WithAssets: true})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NoError(t, result.AssetsError)
	require.NotEmpty(t, strings.TrimSpace(result.Markdown))
	textHash := sha256.Sum256([]byte(result.Markdown))
	proof := map[string]any{
		"knowledge_id": "62b4dfac-8e5c-46c9-be87-86493aca4d40", "pending_operation": 192370,
		"file_sha256": hex.EncodeToString(sum[:]), "file_bytes": len(data),
		"parser": "anydoc", "parser_version": anydoc.Version(),
		"markdown_sha256": hex.EncodeToString(
			textHash[:],
		), "markdown_runes": utf8.RuneCountInString(result.Markdown),
		"image_assets": len(result.Assets), "parser_succeeded": true,
		"protocol_claim_verified": false, "embedding_verified": false, "publication_verified": false,
		"production_writes": false,
	}
	b, err := json.MarshalIndent(proof, "", "  ")
	require.NoError(t, err)
	out := os.Getenv("PERSONAL_EXACT_EVIDENCE_OUT")
	require.NotEmpty(t, out)
	f, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.Write(b)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	t.Logf(
		"original DOCX parsed: bytes=%d runes=%d assets=%d version=%s",
		len(data),
		utf8.RuneCountInString(result.Markdown),
		len(result.Assets),
		anydoc.Version(),
	)
}
