package validation

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// This file is what the `encoding` rule knows how to judge: whether a run of
// bytes is well formed in one of the encodings a form or an upload is likely to
// claim.
//
// The set is closed and written out. Judging an encoding means carrying its
// tables, and the standard library carries the Unicode ones and nothing else, so
// a name outside the set is a boot failure naming the ones there are -- the same
// answer Illuminate gives a name PHP does not list, only earlier.

// encodingFileLimit bounds how much of an upload `encoding` reads. A file larger
// than this is refused rather than judged by its first part, because a prefix
// that is well formed says nothing about the rest.
const encodingFileLimit = 8 << 20

// encodings maps every accepted spelling, lower case, to the check it runs.
var encodings = map[string]func([]byte) bool{
	"utf-8":        utf8.Valid,
	"utf8":         utf8.Valid,
	"ascii":        isASCIIBytes,
	"us-ascii":     isASCIIBytes,
	"iso-8859-1":   func([]byte) bool { return true },
	"latin1":       func([]byte) bool { return true },
	"8bit":         func([]byte) bool { return true },
	"windows-1252": isWindows1252,
	"cp1252":       isWindows1252,
	"utf-16":       func(b []byte) bool { return isUTF16(b, false) },
	"utf-16be":     func(b []byte) bool { return isUTF16(b, false) },
	"utf-16le":     func(b []byte) bool { return isUTF16(b, true) },
	"utf-32":       func(b []byte) bool { return isUTF32(b, false) },
	"utf-32be":     func(b []byte) bool { return isUTF32(b, false) },
	"utf-32le":     func(b []byte) bool { return isUTF32(b, true) },
}

// encodingNames lists the spellings `encoding` accepts, for the message that
// refuses another.
func encodingNames() []string {
	names := make([]string, 0, len(encodings))
	for name := range encodings {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func isASCIIBytes(b []byte) bool {
	for _, c := range b {
		if c >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// isWindows1252 refuses the five bytes the code page leaves unassigned.
func isWindows1252(b []byte) bool {
	for _, c := range b {
		switch c {
		case 0x81, 0x8d, 0x8f, 0x90, 0x9d:
			return false
		}
	}
	return true
}

func isUTF16(b []byte, littleEndian bool) bool {
	if len(b)%2 != 0 {
		return false
	}
	units := make([]uint16, len(b)/2)
	for i := range units {
		if littleEndian {
			units[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
		} else {
			units[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
		}
	}
	for i := 0; i < len(units); i++ {
		switch {
		case utf16.IsSurrogate(rune(units[i])):
			if i+1 >= len(units) || utf16.DecodeRune(rune(units[i]), rune(units[i+1])) == utf8.RuneError {
				return false
			}
			i++
		}
	}
	return true
}

func isUTF32(b []byte, littleEndian bool) bool {
	if len(b)%4 != 0 {
		return false
	}
	for i := 0; i < len(b); i += 4 {
		var r uint32
		if littleEndian {
			r = uint32(b[i]) | uint32(b[i+1])<<8 | uint32(b[i+2])<<16 | uint32(b[i+3])<<24
		} else {
			r = uint32(b[i])<<24 | uint32(b[i+1])<<16 | uint32(b[i+2])<<8 | uint32(b[i+3])
		}
		if r > utf8.MaxRune || (r >= 0xd800 && r <= 0xdfff) {
			return false
		}
	}
	return true
}

// readBounded reads the bytes of an upload from where it was written, and
// reports false when it cannot be read or is larger than encodingFileLimit.
func readBounded(f File) ([]byte, bool) {
	path := f.GetRealPath()
	if path == "" {
		return nil, false
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer func() { _ = file.Close() }()

	content, err := io.ReadAll(io.LimitReader(file, encodingFileLimit+1))
	if err != nil || len(content) > encodingFileLimit {
		return nil, false
	}
	return content, true
}

// checkEncoding refuses an encoding the rule cannot judge, at boot.
func checkEncoding(c *checkCtx) error {
	if _, known := encodings[strings.ToLower(c.r.args[0])]; !known {
		return fmt.Errorf("rule %q does not know the encoding %q; it judges %s",
			c.r.name, c.r.args[0], quoteAll(encodingNames()))
	}
	return nil
}
