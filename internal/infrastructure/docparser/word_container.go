package docparser

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/richardlehane/mscfb"
)

// Resolve only the Word family; the original uploaded name and bytes remain
// untouched. Other engines and unrelated file families keep their selection.
func wordContainerFormat(ctx context.Context, data []byte, declared string) (string, error) {
	if declared != "doc" && declared != "docx" {
		return declared, nil
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	// Inspection is optional above this bound; the existing native parser and
	// upload/resource limits still handle larger documents in the usual way.
	if len(data) > 32<<20 {
		return declared, nil
	}
	if bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		if declared == "docx" {
			return declared, nil
		}
		archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return "", fmt.Errorf("not a zip file: invalid Word package: %w", err)
		}
		if len(archive.File) > 4096 {
			return "", fmt.Errorf("Word package resource limit exceeded")
		}
		word, manifest := false, false
		for _, file := range archive.File {
			if err = ctx.Err(); err != nil {
				return "", err
			}
			if file.Flags&1 != 0 {
				return "", fmt.Errorf("Word file is encrypted; password required")
			}
			switch file.Name {
			case "word/document.xml":
				if word {
					return "", fmt.Errorf("not a zip file: ambiguous Word document part")
				}
				word = true
			case "[Content_Types].xml":
				if manifest {
					return "", fmt.Errorf("not a zip file: ambiguous Word content types")
				}
				manifest = true
			}
		}
		if word && manifest {
			return "docx", nil
		}
		return declared, nil
	}
	if !bytes.HasPrefix(data, []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}) {
		return declared, nil
	}
	if len(data) < 512 {
		return "", fmt.Errorf("Word compound header is truncated")
	}
	shift := binary.LittleEndian.Uint16(data[30:32])
	if shift != 9 && shift != 12 {
		return "", fmt.Errorf("Word compound sector size is invalid")
	}
	sectors := len(data)/(1<<shift) - 1
	// The library allocates some tables from header counts. Validate them
	// against the actual source size before opening this untrusted container.
	for _, offset := range []int{40, 44, 64, 72} {
		if sectors < 0 ||
			uint64(binary.LittleEndian.Uint32(data[offset:offset+4])) > uint64(sectors) {
			return "", fmt.Errorf("Word compound resource limit exceeded")
		}
	}
	reader, err := mscfb.New(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("Word compound inspection failed: %w", err)
	}
	word := false
	for count := 0; ; count++ {
		if err = ctx.Err(); err != nil {
			return "", err
		}
		if count >= 4096 {
			return "", fmt.Errorf("Word compound resource limit exceeded")
		}
		entry, nextErr := reader.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return "", fmt.Errorf("Word compound directory failed: %w", nextErr)
		}
		if len(entry.Path) != 0 || entry.FileInfo().IsDir() {
			continue
		}
		switch entry.Name {
		case "EncryptedPackage":
			return "", fmt.Errorf("EncryptedPackage: Word document is encrypted or protected")
		case "WordDocument":
			word = true
		}
	}
	if word {
		return "doc", nil
	}
	return declared, nil
}
