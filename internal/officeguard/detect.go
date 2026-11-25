// Package officeguard performs bounded, read-only Office password detection.
// Readable means no opening encryption was found, not that parsing will succeed.
package officeguard

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"
)

// State describes format evidence, not a guarantee that document parsing succeeds.
type State string

// These states keep opening encryption, rights management and edit protection distinct.
const (
	Encrypted State = "encrypted"
	DRM       State = "drm"
	Readable  State = "readable"
	Protected State = "protected"
	Invalid   State = "invalid"
	Unknown   State = "unknown"
	NotOffice State = "not_office"
)

const (
	// MaxRead caps bytes read from the input per inspection.
	MaxRead  = 4 << 20
	maxSteps = 8192
)

type budget struct {
	ctx         context.Context
	r           io.ReaderAt
	size        int64
	left, steps int
	end         time.Time
	limited     bool
}

func (b *budget) ReadAt(p []byte, off int64) (int, error) {
	b.steps++
	if b.ctx.Err() != nil || time.Now().After(b.end) || len(p) > b.left || b.steps > maxSteps {
		b.limited = true
		return 0, io.ErrNoProgress
	}
	if off < 0 || off > b.size-int64(len(p)) {
		return 0, io.ErrUnexpectedEOF
	}
	b.left -= len(p)
	n, err := b.r.ReadAt(p, off)
	// ReaderAt cannot be forcibly interrupted. Recheck after it returns so the
	// final read cannot turn an expired/canceled inspection into a verdict.
	if b.ctx.Err() != nil || time.Now().After(b.end) {
		b.limited = true
		return n, io.ErrNoProgress
	}
	return n, err
}

func (b *budget) read(off int64, n int) ([]byte, error) {
	p := make([]byte, n)
	_, err := b.ReadAt(p, off)
	return p, err
}
func u16(p []byte, i int) uint16 { return binary.LittleEndian.Uint16(p[i:]) }
func u32(p []byte, i int) uint32 { return binary.LittleEndian.Uint32(p[i:]) }

var ole = []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}

// Detect sniffs containers even when the filename has been disguised. Unsupported,
// malformed and budget-exhausted files are never reported as password encrypted.
func Detect(ctx context.Context, r io.ReaderAt, size int64, name string) (state State) {
	b := &budget{ctx: ctx, r: r, size: size, left: MaxRead, end: time.Now().Add(750 * time.Millisecond)}
	defer func() {
		if b.limited {
			state = Unknown
		}
	}()
	ext := strings.ToLower(filepath.Ext(name))
	office := strings.Contains("|.docx|.xlsx|.xlsm|.pptx|.doc|.xls|.ppt|.docm|.pptm|.wps|.et|.dps|", "|"+ext+"|")
	if size < 8 {
		if office {
			return Invalid
		}
		return NotOffice
	}
	h, err := b.read(0, 8)
	if err != nil {
		return Unknown
	}
	if string(h) == string(ole) {
		return detectCFB(b)
	}
	if string(h[:4]) == "PK\x03\x04" {
		return detectZIP(b, office)
	}
	if office {
		return Unknown
	}
	return NotOffice
}

type entry struct {
	name               string
	start              uint32
	size               uint64
	kind               byte
	left, right, child uint32
}
type compound struct {
	b    *budget
	ss   int
	fats []uint32
	dirs []entry
	root []uint32
	mini []uint32
}

func (c *compound) sector(id uint32) ([]byte, error) {
	return c.b.read((int64(id)+1)*int64(c.ss), c.ss)
}

func (c *compound) next(id uint32) (uint32, error) {
	idx := int(id) / (c.ss / 4)
	if idx >= len(c.fats) {
		return 0, io.ErrUnexpectedEOF
	}
	p, e := c.sector(c.fats[idx])
	if e != nil {
		return 0, e
	}
	return u32(p, int(id)%(c.ss/4)*4), nil
}

func (c *compound) chain(id uint32, limit int) ([]uint32, error) {
	out := []uint32{}
	seen := map[uint32]bool{}
	for id != 0xfffffffe {
		if len(out) >= limit || seen[id] {
			return nil, io.ErrNoProgress
		}
		seen[id] = true
		out = append(out, id)
		var e error
		id, e = c.next(id)
		if e != nil {
			return nil, e
		}
	}
	return out, nil
}

func (c *compound) prefix(e entry, n int) ([]byte, error) {
	if uint64(n) > e.size {
		n = int(e.size)
	}
	out := make([]byte, 0, n)
	id := e.start
	seen := map[uint32]bool{}
	for len(out) < n {
		if seen[id] {
			return nil, io.ErrNoProgress
		}
		seen[id] = true
		var p []byte
		var err error
		if e.size < 4096 {
			rootIdx := int(id) * 64 / c.ss
			mi := int(id) / (c.ss / 4)
			if rootIdx >= len(c.root) || mi >= len(c.mini) {
				return nil, io.ErrUnexpectedEOF
			}
			p, err = c.b.read((int64(c.root[rootIdx])+1)*int64(c.ss)+int64(id)%int64(c.ss/64)*64, 64)
			if err != nil {
				return nil, err
			}
			fat, er := c.sector(c.mini[mi])
			if er != nil {
				return nil, er
			}
			id = u32(fat, int(id)%(c.ss/4)*4)
		} else {
			p, err = c.sector(id)
			if err != nil {
				return nil, err
			}
			id, err = c.next(id)
			if err != nil {
				return nil, err
			}
		}
		take := n - len(out)
		if take > len(p) {
			take = len(p)
		}
		out = append(out, p[:take]...)
	}
	return out, nil
}

func detectCFB(b *budget) State {
	h, err := b.read(0, 512)
	if err != nil {
		return Invalid
	}
	ver, shift := u16(h, 26), u16(h, 30)
	validVersion := (ver == 3 && shift == 9) || (ver == 4 && shift == 12)
	if u16(h, 28) != 0xfffe || !validVersion || u16(h, 32) != 6 ||
		u32(h, 56) != 4096 {
		return Invalid
	}
	c := &compound{b: b, ss: 1 << shift}
	nf, nd, nm := u32(h, 44), u32(h, 72), u32(h, 64)
	if nf > 4096 || nd > 64 || nm > 1024 {
		b.limited = true
		return Unknown
	}
	for i := 76; i < 512 && len(c.fats) < int(nf); i += 4 {
		c.fats = append(c.fats, u32(h, i))
	}
	id := u32(h, 68)
	for i := uint32(0); i < nd && len(c.fats) < int(nf); i++ {
		p, e := c.sector(id)
		if e != nil {
			return Invalid
		}
		for j := 0; j < c.ss-4 && len(c.fats) < int(nf); j += 4 {
			c.fats = append(c.fats, u32(p, j))
		}
		id = u32(p, c.ss-4)
	}
	if len(c.fats) != int(nf) {
		return Invalid
	}
	dirs, e := c.chain(u32(h, 48), 256)
	if e != nil {
		return Invalid
	}
	for _, id := range dirs {
		p, e := c.sector(id)
		if e != nil {
			return Invalid
		}
		for j := 0; j < c.ss; j += 128 {
			q := p[j : j+128]
			kind := q[66]
			if kind != 0 && kind != 1 && kind != 2 && kind != 5 {
				return Invalid
			}
			n := int(u16(q, 64))
			name := ""
			if kind != 0 {
				if n < 2 || n > 64 || n%2 != 0 {
					return Invalid
				}
				chars := []uint16{}
				for k := 0; k < n-2; k += 2 {
					chars = append(chars, u16(q, k))
				}
				name = string(utf16.Decode(chars))
			}
			size := binary.LittleEndian.Uint64(q[120:])
			if ver == 3 {
				size = uint64(u32(q, 120))
			}
			c.dirs = append(c.dirs, entry{name, u32(q, 116), size, kind, u32(q, 68), u32(q, 72), u32(q, 76)})
		}
	}
	if len(c.dirs) == 0 || c.dirs[0].kind != 5 {
		return Invalid
	}
	// Only live root streams count; stale directory entries and embedded objects do not.
	streams := map[string]entry{}
	todo := []uint32{c.dirs[0].child}
	seen := map[uint32]bool{}
	for len(todo) > 0 {
		id := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		if id == 0xffffffff {
			continue
		}
		if int64(id) >= int64(len(c.dirs)) || seen[id] {
			return Invalid
		}
		seen[id] = true
		d := c.dirs[id]
		todo = append(todo, d.left, d.right)
		if d.kind == 2 {
			if _, ok := streams[d.name]; ok {
				return Invalid
			}
			streams[d.name] = d
		}
	}
	if nm > 0 {
		c.mini, err = c.chain(u32(h, 60), 1024)
		if err != nil {
			return Invalid
		}
		c.root, err = c.chain(c.dirs[0].start, 4096)
		if err != nil {
			return Invalid
		}
	}
	info, ei := streams["EncryptionInfo"]
	pkg, ep := streams["EncryptedPackage"]
	if ep && !ei && pkg.size >= 8 && c.hasDRM() {
		return DRM
	}
	if ei && ep && info.size >= 8 && pkg.size >= 8 {
		p, e := c.prefix(info, 8)
		if e != nil {
			return Invalid
		}
		major, minor := u16(p, 0), u16(p, 2)
		// MS-OFFCRYPTO: Standard 2/3/4.2, Extensible 3/4.3, Agile 4.4.
		// A Cartesian range would incorrectly recognize unknown 2.3/2.4/3.4.
		if (minor == 2 && major >= 2 && major <= 4) ||
			(minor == 3 && (major == 3 || major == 4)) || (major == 4 && minor == 4) {
			return Encrypted
		}
		return Unknown
	}
	if ei || ep {
		return Invalid
	}
	if d, ok := streams["WordDocument"]; ok {
		p, e := c.prefix(d, 32)
		if e != nil || len(p) < 32 || u16(p, 0) != 0xa5ec {
			return Invalid
		}
		if u16(p, 10)&0x100 != 0 {
			return Encrypted
		}
		if u16(p, 10)&0x800 != 0 {
			return Protected
		}
		return Readable
	}
	for _, name := range []string{"Workbook", "Book"} {
		if d, ok := streams[name]; ok {
			p, e := c.prefix(d, 65536)
			if e != nil {
				return Invalid
			}
			if len(p) < 8 || (u16(p, 0) != 0x809 && u16(p, 0) != 0x409 && u16(p, 0) != 0x209 && u16(p, 0) != 9) {
				return Unknown
			}
			protected := false
			for i := 0; i+4 <= len(p); {
				tag, n := u16(p, i), int(u16(p, i+2))
				if i+4+n > len(p) {
					return Unknown
				}
				if tag == 0x2f && n >= 4 {
					return Encrypted
				}
				if (tag == 0x12 || tag == 0x19) && n >= 2 && u16(p, i+4) != 0 {
					protected = true
				}
				if tag == 0xa {
					if protected {
						return Protected
					}
					return Readable
				}
				i += 4 + n
			}
			return Unknown
		}
	}
	if d, ok := streams["Current User"]; ok {
		p, e := c.prefix(d, 20)
		if e != nil || len(p) < 20 || u16(p, 2) != 0xff6 || u32(p, 8) != 20 {
			return Invalid
		}
		switch u32(p, 12) {
		case 0xf3d1c4df:
			return Encrypted
		case 0xe391c05f:
			return Readable
		}
		return Unknown
	}
	return Unknown
}

// hasDRM requires live IRM data-space paths and their transform definition.
// Names elsewhere, stale entries, and a lone EncryptedPackage are insufficient.
// MS-OFFCRYPTO 2.2.2 / 2.2.3 define these IRM-specific structures.
func (c *compound) hasDRM() bool {
	find := func(parent uint32, name string, kind byte) (uint32, bool) {
		if int64(parent) >= int64(len(c.dirs)) {
			return 0, false
		}
		todo := []uint32{c.dirs[parent].child}
		seen := map[uint32]bool{}
		found, count := uint32(0), 0
		for len(todo) > 0 {
			id := todo[len(todo)-1]
			todo = todo[:len(todo)-1]
			if id == 0xffffffff {
				continue
			}
			if int64(id) >= int64(len(c.dirs)) || seen[id] {
				return 0, false
			}
			seen[id] = true
			d := c.dirs[id]
			todo = append(todo, d.left, d.right)
			if d.name == name && d.kind == kind {
				found = id
				count++
			}
		}
		return found, count == 1
	}
	path := func(names []string) (entry, bool) {
		id := uint32(0)
		for i, name := range names {
			kind := byte(1)
			if i == len(names)-1 {
				kind = 2
			}
			next, ok := find(id, name, kind)
			if !ok {
				return entry{}, false
			}
			id = next
		}
		return c.dirs[id], true
	}
	definition, ok := path([]string{"\x06DataSpaces", "DataSpaceInfo", "DRMEncryptedDataSpace"})
	if !ok || definition.size < 56 {
		return false
	}
	p, err := c.prefix(definition, 56)
	if err != nil || len(p) < 56 || u32(p, 0) != 8 || u32(p, 4) != 1 || u32(p, 8) != 42 {
		return false
	}
	encoded := func(s string) []byte {
		out := make([]byte, 0, len(s)*2)
		for _, v := range utf16.Encode([]rune(s)) {
			out = append(out, byte(v), byte(v>>8))
		}
		return out
	}
	if !bytes.Equal(p[12:54], encoded("DRMEncryptedTransform")) {
		return false
	}
	primary, ok := path([]string{"\x06DataSpaces", "TransformInfo", "DRMEncryptedTransform", "\x06Primary"})
	if !ok {
		return false
	}
	p, err = c.prefix(primary, 512)
	return err == nil && bytes.Contains(p, encoded("Microsoft.Metadata.DRMTransform"))
}

func detectZIP(b *budget, office bool) State {
	// Reject excessive directory sizes before archive/zip allocates its file list.
	n := int64(65557)
	if n > b.size {
		n = b.size
	}
	tail, e := b.read(b.size-n, int(n))
	if e != nil {
		return Invalid
	}
	found := -1
	for i := len(tail) - 22; i >= 0; i-- {
		if u32(tail, i) == 0x06054b50 && i+22+int(u16(tail, i+20)) == len(tail) {
			found = i
			break
		}
	}
	if found < 0 {
		return Invalid
	}
	q := tail[found:]
	if u16(q, 4) != 0 || u16(q, 6) != 0 {
		return Unknown
	}
	if u16(q, 10) > 8192 || u32(q, 12) > 1<<20 {
		b.limited = true
		return Unknown
	}
	zr, e := zip.NewReader(b, b.size)
	if e != nil {
		return Invalid
	}
	known := false
	protection := false
	encrypted := false
	inflated := uint64(0)
	for _, f := range zr.File {
		if f.Name == "word/document.xml" || f.Name == "xl/workbook.xml" || f.Name == "ppt/presentation.xml" {
			known = true
		}
		if f.Flags&1 != 0 {
			encrypted = true
		}
	}
	if encrypted && (office || known) {
		return Encrypted
	}
	if !known {
		if office {
			return Unknown
		}
		return NotOffice
	}
	for _, f := range zr.File {
		if f.Name != "word/settings.xml" && f.Name != "xl/workbook.xml" &&
			(!strings.HasPrefix(f.Name, "xl/worksheets/") || !strings.HasSuffix(f.Name, ".xml")) {
			continue
		}
		if f.UncompressedSize64 > 256<<10 || f.CompressedSize64 > 256<<10 {
			return Unknown
		}
		inflated += f.UncompressedSize64
		if inflated > 1<<20 {
			return Unknown
		}
		r, e := f.Open()
		if e != nil {
			return Invalid
		}
		p, e := io.ReadAll(io.LimitReader(r, 256<<10+1))
		_ = r.Close()
		if e != nil || len(p) > 256<<10 {
			return Unknown
		}
		if strings.Contains(string(p), "documentProtection") || strings.Contains(string(p), "writeProtection") ||
			strings.Contains(string(p), "sheetProtection") ||
			strings.Contains(string(p), "workbookProtection") ||
			strings.Contains(string(p), "fileSharing") {
			protection = true
		}
		if b.ctx.Err() != nil || time.Now().After(b.end) {
			b.limited = true
			return Unknown
		}
	}
	if protection {
		return Protected
	}
	return Readable
}
