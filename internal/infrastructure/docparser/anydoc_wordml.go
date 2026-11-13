package docparser

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/types"
)

const wordMLNamespace = "http://schemas.microsoft.com/office/word/2003/wordml"

func isWordML(content []byte) bool {
	if len(content) == 0 || len(content) > 8<<20 {
		return false
	}
	decoder := xml.NewDecoder(bytes.NewReader(content))
	for range 8 {
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		switch value := token.(type) {
		case xml.StartElement:
			return value.Name == (xml.Name{Space: wordMLNamespace, Local: "wordDocument"})
		case xml.Directive:
			return false
		}
	}
	return false
}

// A .doc filename can contain Word 2003 XML, rather than an OLE2 file. The
// private converter proves text, embedded-image and hyperlink coverage, then
// returns the complete DOCX package for the ordinary native parser. The fetched
// original XML remains the source snapshot and is never replaced in storage.
func (r *AnydocReader) readWordML(ctx context.Context, req *types.ReadRequest, format string,
) (*types.ReadResult, bool, error) {
	if format != "doc" || !feishusafety.Scoped(ctx) || !isWordML(req.FileContent) {
		return nil, false, nil
	}
	if !r.canFallback() {
		return nil, true, fmt.Errorf("WordML converter unavailable")
	}
	call, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	converted, err := r.fallback.Read(call, &types.ReadRequest{
		FileContent: req.FileContent, FileName: "source.xml", FileType: "wordml", ParserEngine: BuiltinEngineName,
	})
	if err != nil {
		return nil, true, fmt.Errorf("WordML conversion: %w", err)
	}
	if converted == nil || converted.Error != "" || len(converted.ImageRefs) != 0 {
		return nil, true, fmt.Errorf("WordML conversion response invalid")
	}
	digest := sha256.Sum256(req.FileContent)
	if converted.Metadata["wordml_source_sha256"] != hex.EncodeToString(digest[:]) {
		return nil, true, fmt.Errorf("WordML conversion source binding invalid")
	}
	encoded := converted.Metadata["wordml_docx_base64"]
	if len(encoded) == 0 || len(encoded) > base64.StdEncoding.EncodedLen(8<<20) {
		return nil, true, fmt.Errorf("WordML converted package outside bounds")
	}
	content, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(content) > 8<<20 || !bytes.HasPrefix(content, []byte("PK\x03\x04")) {
		return nil, true, fmt.Errorf("WordML converted package invalid")
	}
	native := *req
	native.FileContent, native.FileType, native.FileName = content, "docx", "converted.docx"
	result, err := r.Read(ctx, &native)
	if err != nil {
		return nil, true, fmt.Errorf("WordML native parse: %w", err)
	}
	result.Metadata["source_format"] = "wordml"
	return result, true, nil
}
