package docparser

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func testWordZip(t *testing.T, names ...string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, name := range names {
		entry, err := writer.Create(name)
		require.NoError(t, err)
		_, err = entry.Write([]byte("<document/>"))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	return buffer.Bytes()
}

func testWordCompound(name string) []byte {
	data := make([]byte, 1536)
	copy(data, []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1})
	binary.LittleEndian.PutUint16(data[24:], 0x3e)
	binary.LittleEndian.PutUint16(data[26:], 3)
	binary.LittleEndian.PutUint16(data[28:], 0xfffe)
	binary.LittleEndian.PutUint16(data[30:], 9)
	binary.LittleEndian.PutUint16(data[32:], 6)
	binary.LittleEndian.PutUint32(data[44:], 1)
	binary.LittleEndian.PutUint32(data[56:], 4096)
	for _, offset := range []int{60, 68} {
		binary.LittleEndian.PutUint32(data[offset:], 0xfffffffe)
	}
	for offset := 76; offset < 512; offset += 4 {
		binary.LittleEndian.PutUint32(data[offset:], 0xffffffff)
	}
	binary.LittleEndian.PutUint32(data[76:], 1)
	for offset := 1024; offset < 1536; offset += 4 {
		binary.LittleEndian.PutUint32(data[offset:], 0xffffffff)
	}
	binary.LittleEndian.PutUint32(data[1024:], 0xfffffffe)
	binary.LittleEndian.PutUint32(data[1028:], 0xfffffffd)
	entry := func(offset int, name string, kind byte, child uint32) {
		encoded := utf16.Encode([]rune(name))
		for i, v := range encoded {
			binary.LittleEndian.PutUint16(data[offset+i*2:], v)
		}
		binary.LittleEndian.PutUint16(data[offset+64:], uint16((len(encoded)+1)*2))
		data[offset+66] = kind
		data[offset+67] = 1
		binary.LittleEndian.PutUint32(data[offset+68:], 0xffffffff)
		binary.LittleEndian.PutUint32(data[offset+72:], 0xffffffff)
		binary.LittleEndian.PutUint32(data[offset+76:], child)
		binary.LittleEndian.PutUint32(data[offset+116:], 0xfffffffe)
	}
	entry(512, "Root Entry", 5, 1)
	entry(640, name, 2, 0xffffffff)
	binary.LittleEndian.PutUint32(data[760:], 8)
	return data
}

func TestWordContainerUsesPackageEvidenceWithoutChangingTheSource(t *testing.T) {
	data := testWordZip(t, "[Content_Types].xml", "word/document.xml")
	before := bytes.Clone(data)
	format, err := wordContainerFormat(t.Context(), data, "doc")
	require.NoError(t, err)
	require.Equal(t, "docx", format)
	require.Equal(t, before, data)
	format, err = wordContainerFormat(t.Context(), data, "docx")
	require.NoError(t, err)
	require.Equal(t, "docx", format)
	format, err = wordContainerFormat(t.Context(), testWordCompound("WordDocument"), "docx")
	require.NoError(t, err)
	require.Equal(t, "doc", format)
	for _, declared := range []string{"doc", "docx"} {
		_, err = wordContainerFormat(t.Context(), testWordCompound("EncryptedPackage"), declared)
		require.ErrorContains(t, err, "EncryptedPackage")
		_, err = NewAnydocReader(
			nil,
			nil,
		).Read(t.Context(), &types.ReadRequest{FileName: "protected." + declared, FileType: declared, FileContent: testWordCompound("EncryptedPackage")})
		require.ErrorContains(
			t,
			err,
			"EncryptedPackage",
			"protected data must not reach the native converter",
		)
	}
}

func TestWordContainerDoesNotPromoteUnrelatedOrAmbiguousPackages(t *testing.T) {
	for _, data := range [][]byte{testWordZip(t, "xl/workbook.xml"), []byte("plain text"), testWordCompound("Workbook")} {
		format, err := wordContainerFormat(t.Context(), data, "doc")
		require.NoError(t, err)
		require.Equal(t, "doc", format)
	}
	format, err := wordContainerFormat(
		t.Context(),
		testWordZip(t, "word/document.xml", "[Content_Types].xml"),
		"pdf",
	)
	require.NoError(t, err)
	require.Equal(t, "pdf", format)
	_, err = wordContainerFormat(
		t.Context(),
		testWordZip(t, "word/document.xml", "word/document.xml", "[Content_Types].xml"),
		"doc",
	)
	require.ErrorContains(t, err, "ambiguous")
}

func TestWordContainerBoundsMalformedHeadersAndCancellation(t *testing.T) {
	for _, offset := range []int{40, 44, 64, 72} {
		data := testWordCompound("WordDocument")
		binary.LittleEndian.PutUint32(data[offset:], 0xffffffff)
		_, err := wordContainerFormat(t.Context(), data, "docx")
		require.ErrorContains(t, err, "resource limit")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := wordContainerFormat(ctx, testWordCompound("WordDocument"), "docx")
	require.ErrorIs(t, err, context.Canceled)
	format, err := wordContainerFormat(t.Context(), []byte(strings.Repeat("a", (32<<20)+1)), "doc")
	require.NoError(t, err)
	require.Equal(
		t,
		"doc",
		format,
		"oversized optional inspection keeps the original parser limits",
	)
}
