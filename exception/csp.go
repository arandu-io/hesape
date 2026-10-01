package exception

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
)

// The policies the built-in pages answer with, computed once from the styles
// they inline.
//
// An application's Content-Security-Policy allows style-src 'self', which
// refuses an inline <style>, and these pages cannot link a stylesheet: they are
// drawn when the application could not answer, including when its assets are
// what failed. Under the application's policy they rendered as unstyled text.
// Each answers instead with a policy of its own that allows exactly the one
// style block it carries, by hash, and nothing else: no script, no frame, no
// form target, no base URL.
var (
	statusCSP = pagePolicy(statusStyle)
	debugCSP  = pagePolicy(debugStyle)
)

// pagePolicy is the Content-Security-Policy for a page whose only resource is
// the inline style block style -- the exact text between <style> and </style>,
// which is what a browser hashes.
func pagePolicy(style string) string {
	sum := sha256.Sum256([]byte(style))
	return "default-src 'none'; style-src 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) +
		"'; img-src data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
}

// setPagePolicy replaces whatever Content-Security-Policy the application's
// middleware set with policy, for this response only.
func setPagePolicy(w http.ResponseWriter, policy string) {
	w.Header().Set("Content-Security-Policy", policy)
}
