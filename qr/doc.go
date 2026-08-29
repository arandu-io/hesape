// Package qr encodes text into a QR symbol and renders it as inline SVG.
//
// The package is deliberately narrow. It encodes a single byte-mode segment at
// error correction level H, in the version range that content of 64 to 250
// bytes needs, and it renders that symbol as SVG markup meant to be written
// straight into a page. It does not produce raster images, does not write
// files, and offers no other encoding mode, correction level, or symbol
// family.
//
// Content is written as raw bytes with no ECI designator, so a scanner reads
// it back byte for byte. Callers that need the text to survive every reader
// should keep to ASCII.
//
// The rendered markup carries no style element, no style attribute, no script,
// and no reference to anything outside the document, so it can be served under
// a strict content security policy.
package qr
