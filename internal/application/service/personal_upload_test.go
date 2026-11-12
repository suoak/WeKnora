package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

type personalTestScanner func(context.Context, []byte) error

func (f personalTestScanner) Scan(c context.Context, b []byte) error { return f(c, b) }

func TestPersonalFileValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		ok   bool
	}{
		{"note.txt", []byte("plain UTF-8 knowledge"), true}, {"note.md", []byte("# knowledge"), true},
		{"note.pdf", []byte("%PDF-1.7\n1 0 obj<</Type/Catalog>>endobj\n%%EOF"), true},
		{"../note.txt", []byte("text"), false}, {"C:\\file.txt", []byte("text"), false},
		{"note.pdf", []byte("not PDF"), false}, {"note.txt", []byte{0, 1, 2}, false},
		{"note.md", []byte("![x](http://127.0.0.1/secret)"), false},
		{"note.md", []byte("resource://private-file"), false},
		{"note.pdf", []byte("%PDF-1.7\n/JavaScript\n%%EOF"), false},
		{"note.exe", []byte("MZ"), false}, {"note.txt", make([]byte, PersonalUploadMaxBytes+1), false},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.ok, validatePersonalFile(tc.name, tc.data) == nil) })
	}
	office := func(part, relationship string) []byte {
		var b bytes.Buffer
		w := zip.NewWriter(&b)
		for n, c := range map[string]string{"[Content_Types].xml": "<Types/>", part: "<document/>", "_rels/.rels": relationship} {
			f, e := w.Create(n)
			require.NoError(t, e)
			_, e = f.Write([]byte(c))
			require.NoError(t, e)
		}
		require.NoError(t, w.Close())
		return b.Bytes()
	}
	require.NoError(t, validatePersonalFile("note.docx", office("word/document.xml", "<Relationships/>")))
	require.NoError(t, validatePersonalFile("note.xlsx", office("xl/workbook.xml", "<Relationships/>")))
	require.Error(t, validatePersonalFile("note.docx", office("word/document.xml", `<Relationships><Relationship TargetMode="External" Target="file:///etc/passwd"/></Relationships>`)))
	require.Error(t, validatePersonalFile("note.xlsx", office("word/document.xml", "<Relationships/>")))
}

func TestClamdScannerRequiresExactCleanVerdict(t *testing.T) {
	for _, reply := range []string{"stream: OK\x00", "stream: malware FOUND\x00", "INSTREAM size limit exceeded\x00", "stream: OK"} {
		t.Run(reply, func(t *testing.T) {
			listener, e := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, e)
			defer listener.Close()
			done := make(chan []byte, 1)
			go func() {
				conn, e := listener.Accept()
				if e != nil {
					return
				}
				defer conn.Close()
				header := make([]byte, 10)
				if _, e = io.ReadFull(conn, header); e != nil {
					return
				}
				if string(header) != "zINSTREAM\x00" {
					return
				}
				var data []byte
				for {
					var length uint32
					if binary.Read(conn, binary.BigEndian, &length) != nil {
						return
					}
					if length == 0 {
						break
					}
					b := make([]byte, length)
					if _, e = io.ReadFull(conn, b); e != nil {
						return
					}
					data = append(data, b...)
				}
				done <- data
				conn.Write([]byte(reply))
			}()
			err := (ClamdScanner{Address: listener.Addr().String()}).Scan(context.Background(), []byte("scanned contents"))
			require.Equal(t, reply == "stream: OK\x00", err == nil)
			require.Equal(t, []byte("scanned contents"), <-done)
		})
	}
	require.Error(t, (ClamdScanner{}).Scan(context.Background(), []byte("text")))
}
