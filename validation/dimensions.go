package validation

import (
	"encoding/binary"
	"image"
	"io"
	"os"

	// Registered so that image.DecodeConfig reads the three formats the
	// standard library carries. Only the header is decoded, never the pixels:
	// the bytes belong to whoever uploaded them.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

// imageDimensions measures an image, which is what `dimensions` compares.
//
// A File that carries its own size -- a Dimensioner -- is asked rather than
// read, and only that fails back to decoding the header of the bytes.
func imageDimensions(f File) (width, height int, ok bool) {
	if d, isDimensioner := f.(Dimensioner); isDimensioner {
		return d.Dimensions()
	}
	path := f.GetRealPath()
	if path == "" {
		return 0, 0, false
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, false
	}
	defer file.Close()

	head := make([]byte, 32)
	n, _ := io.ReadFull(file, head)
	head = head[:n]

	// BMP and WEBP have no decoder in the standard library and both are in the
	// list `image` accepts, so their headers are read here. Neither reaches the
	// pixels: the size is in the first thirty bytes of each.
	if w, h, read := bmpDimensions(head); read {
		return w, h, true
	}
	if w, h, read := webpDimensions(head); read {
		return w, h, true
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return 0, 0, false
	}
	config, _, err := image.DecodeConfig(file)
	if err != nil {
		return 0, 0, false
	}
	return config.Width, config.Height, true
}

// bmpDimensions reads a BITMAPINFOHEADER: the width and the height are two
// signed 32-bit numbers at a fixed offset. A negative height is a bitmap stored
// top down, and its magnitude is the height.
func bmpDimensions(head []byte) (width, height int, ok bool) {
	if len(head) < 26 || head[0] != 'B' || head[1] != 'M' {
		return 0, 0, false
	}
	w := int32(binary.LittleEndian.Uint32(head[18:22]))
	h := int32(binary.LittleEndian.Uint32(head[22:26]))
	if h < 0 {
		h = -h
	}
	if w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return int(w), int(h), true
}

// webpDimensions reads the three shapes a WEBP file comes in: the extended
// header, the lossy frame and the lossless frame. Each keeps the canvas size
// within the first thirty bytes.
func webpDimensions(head []byte) (width, height int, ok bool) {
	if len(head) < 16 || string(head[0:4]) != "RIFF" || string(head[8:12]) != "WEBP" {
		return 0, 0, false
	}
	switch string(head[12:16]) {
	case "VP8X":
		if len(head) < 30 {
			return 0, 0, false
		}
		w := int(head[24]) | int(head[25])<<8 | int(head[26])<<16
		h := int(head[27]) | int(head[28])<<8 | int(head[29])<<16
		return w + 1, h + 1, true
	case "VP8 ":
		if len(head) < 30 || head[23] != 0x9d || head[24] != 0x01 || head[25] != 0x2a {
			return 0, 0, false
		}
		w := int(binary.LittleEndian.Uint16(head[26:28]) & 0x3fff)
		h := int(binary.LittleEndian.Uint16(head[28:30]) & 0x3fff)
		return w, h, true
	case "VP8L":
		if len(head) < 25 || head[20] != 0x2f {
			return 0, 0, false
		}
		bits := binary.LittleEndian.Uint32(head[21:25])
		w := int(bits&0x3fff) + 1
		h := int((bits>>14)&0x3fff) + 1
		return w, h, true
	}
	return 0, 0, false
}
