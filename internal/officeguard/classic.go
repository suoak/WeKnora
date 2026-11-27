package officeguard

import (
	"bytes"
	"encoding/binary"
	"unicode/utf16"
)

func wide(s string) []byte {
	p := make([]byte, 0, len(s)*2)
	for _, v := range utf16.Encode([]rune(s)) {
		p = binary.LittleEndian.AppendUint16(p, v)
	}
	return p
}

// MS-OFFCRYPTO 2.2.1-2.2.3: follow live, root-relative paths and verify
// the map -> definition -> transform relationship, without reading licenses.
func (c *compound) hasClassicDRM() bool {
	definition, ok := c.path([]string{"\x06DataSpaces", "DataSpaceInfo", "\x09DRMDataSpace"})
	if !ok || definition.size < 40 {
		return false
	}
	p, err := c.prefix(definition, 40)
	if err != nil || len(p) != 40 || u32(p, 0) != 8 || u32(p, 4) != 1 ||
		u32(p, 8) != 26 || !bytes.Equal(p[12:38], wide("\x09DRMTransform")) {
		return false
	}
	mapping, ok := c.path([]string{"\x06DataSpaces", "DataSpaceMap"})
	if !ok || mapping.size < 80 || mapping.size > 4096 {
		return false
	}
	p, err = c.prefix(mapping, int(mapping.size))
	if err != nil || u32(p, 0) != 8 || u32(p, 4) < 1 || u32(p, 4) > 2 {
		return false
	}
	// Each map entry must reference exactly one root stream. Optional viewer
	// content has its own data space and is never required for DRM detection.
	found := false
	pos := 8
	for i := uint32(0); i < u32(p, 4); i++ {
		if pos+12 > len(p) {
			return false
		}
		n := int(u32(p, pos))
		if n < 12 || n > len(p)-pos || u32(p, pos+4) != 1 || u32(p, pos+8) != 0 {
			return false
		}
		q := p[pos+12 : pos+n]
		readName := func() ([]byte, bool) {
			if len(q) < 4 {
				return nil, false
			}
			l := int(u32(q, 0))
			padded := (l + 3) &^ 3
			if l < 0 || l%2 != 0 || padded > len(q)-4 {
				return nil, false
			}
			v := q[4 : 4+l]
			q = q[4+padded:]
			return v, true
		}
		stream, a := readName()
		space, b := readName()
		if !a || !b || len(q) != 0 {
			return false
		}
		if bytes.Equal(stream, wide("\x09DRMContent")) && bytes.Equal(space, wide("\x09DRMDataSpace")) && !found {
			found = true
		} else if !bytes.Equal(stream, wide("\x09DRMViewerContent")) || !bytes.Equal(space, wide("\x09LZXDRMDataSpace")) {
			return false
		}
		pos += n
	}
	if !found || pos != len(p) {
		return false
	}
	primary, ok := c.path([]string{"\x06DataSpaces", "TransformInfo", "\x09DRMTransform", "\x06Primary"})
	if !ok || primary.size < 168 {
		return false
	}
	p, err = c.prefix(primary, 168)
	return err == nil && len(p) == 168 && u32(p, 0) == 88 && u32(p, 4) == 1 &&
		u32(p, 8) == 76 && bytes.Equal(p[12:88], wide("{C73DFACD-061F-43B0-8B64-0C620D2A8B50}")) &&
		u32(p, 88) == 62 && bytes.Equal(p[92:154], wide("Microsoft.Metadata.DRMTransform")) &&
		u32(p, 156) == 1 && u32(p, 160) == 1 && u32(p, 164) == 1
}
