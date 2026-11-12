package service

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"io"
	"net"
	"path"
	"strings"
	"time"
	"unicode/utf8"
)

const PersonalUploadMaxBytes = 8 << 20

var ErrPersonalUpload = errors.New("file rejected: unsupported, unsafe, oversized or unscannable content")

// FileScanner must scan the complete bounded byte stream; failures deny upload.
type FileScanner interface {
	Scan(context.Context, []byte) error
}

// ClamdScanner uses INSTREAM, never a client path or shell command. See
// https://docs.clamav.net/manual/Usage/ClamdProtocol.html . Address is operator
// configuration, never an RPC argument. Only an explicit clean verdict passes.
type ClamdScanner struct{ Address string }

func (s ClamdScanner) Scan(ctx context.Context, data []byte) error {
	if s.Address == "" || len(data) == 0 || len(data) > PersonalUploadMaxBytes {
		return ErrPersonalUpload
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	network, address := "tcp", s.Address
	if strings.HasPrefix(address, "unix:") {
		network = "unix"
		address = strings.TrimPrefix(address, "unix:")
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
	if err != nil {
		return ErrPersonalUpload
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if _, err = io.Copy(conn, strings.NewReader("zINSTREAM\x00")); err != nil {
		return ErrPersonalUpload
	}
	for offset := 0; offset < len(data); {
		n := len(data) - offset
		if n > 32<<10 {
			n = 32 << 10
		}
		var header [4]byte
		binary.BigEndian.PutUint32(header[:], uint32(n))
		if _, err = io.Copy(conn, bytes.NewReader(header[:])); err != nil {
			return ErrPersonalUpload
		}
		if _, err = io.Copy(conn, bytes.NewReader(data[offset:offset+n])); err != nil {
			return ErrPersonalUpload
		}
		offset += n
	}
	if _, err = conn.Write([]byte{0, 0, 0, 0}); err != nil {
		return ErrPersonalUpload
	}
	result, err := bufio.NewReader(io.LimitReader(conn, 512)).ReadString(0)
	if err != nil || result != "stream: OK\x00" {
		return ErrPersonalUpload
	}
	return nil
}

func validatePersonalFile(name string, data []byte) error {
	if len(name) == 0 || len(name) > 180 || path.Base(name) != name || strings.ContainsAny(name, "\\:\x00\r\n") || len(data) == 0 || len(data) > PersonalUploadMaxBytes {
		return ErrPersonalUpload
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".txt", ".md":
		if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
			return ErrPersonalUpload
		}
		// Uploaded Markdown must not cause the parser to fetch remote/local images
		// or claim preexisting file resources from the target workspace.
		lower := strings.ToLower(string(data))
		for _, token := range []string{"![", "<img", "<iframe", "resource://", "file://", "/files/"} {
			if strings.Contains(lower, token) {
				return ErrPersonalUpload
			}
		}
	case ".pdf":
		if !bytes.HasPrefix(data, []byte("%PDF-")) || !bytes.Contains(data[max(0, len(data)-1024):], []byte("%%EOF")) {
			return ErrPersonalUpload
		}
		// Reject encryption and active/embedded content before antivirus/parser.
		for _, token := range []string{"/Encrypt", "/JavaScript", "/JS", "/Launch", "/EmbeddedFile", "/OpenAction", "/AA", "/XFA", "/URI", "/GoToR"} {
			if bytes.Contains(data, []byte(token)) {
				return ErrPersonalUpload
			}
		}
	case ".docx", ".xlsx":
		z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil || len(z.File) > 1024 {
			return ErrPersonalUpload
		}
		seen := map[string]bool{}
		total := int64(0)
		required := "word/document.xml"
		if strings.EqualFold(path.Ext(name), ".xlsx") {
			required = "xl/workbook.xml"
		}
		for _, f := range z.File {
			n := f.Name
			lower := strings.ToLower(n)
			if seen[n] || path.Clean(n) != strings.TrimSuffix(n, "/") || strings.HasPrefix(n, "/") || strings.ContainsAny(n, "\\:") || strings.HasPrefix(n, "../") || f.Flags&1 != 0 {
				return ErrPersonalUpload
			}
			seen[n] = true
			if f.UncompressedSize64 > 16<<20 || total+int64(f.UncompressedSize64) > 32<<20 || f.UncompressedSize64 > max(uint64(1), f.CompressedSize64)*200 {
				return ErrPersonalUpload
			}
			total += int64(f.UncompressedSize64)
			for _, bad := range []string{"vbaproject", "activex", "embeddings/", "externallinks/", "customui/"} {
				if strings.Contains(lower, bad) {
					return ErrPersonalUpload
				}
			}
			r, e := f.Open()
			if e != nil {
				return ErrPersonalUpload
			}
			b, e := io.ReadAll(io.LimitReader(r, 16<<20+1))
			r.Close()
			if e != nil || len(b) > 16<<20 {
				return ErrPersonalUpload
			}
			if strings.HasSuffix(lower, ".xml") || strings.HasSuffix(lower, ".rels") {
				d := xml.NewDecoder(bytes.NewReader(b))
				for {
					t, e := d.Token()
					if e == io.EOF {
						break
					}
					if e != nil {
						return ErrPersonalUpload
					}
					if directive, ok := t.(xml.Directive); ok && strings.Contains(strings.ToUpper(string(directive)), "DOCTYPE") {
						return ErrPersonalUpload
					}
					if start, ok := t.(xml.StartElement); ok {
						for _, a := range start.Attr {
							if a.Name.Local == "TargetMode" && strings.EqualFold(a.Value, "External") {
								return ErrPersonalUpload
							}
						}
					}
				}
			}
		}
		if !seen["[Content_Types].xml"] || !seen[required] {
			return ErrPersonalUpload
		}
	default:
		return ErrPersonalUpload
	}
	return nil
}
