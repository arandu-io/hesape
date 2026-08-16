package client

import (
	"fmt"
	"net/http"
	"strings"
)

// Request wraps *http.Request and adds inspection methods for method, URL,
// headers, body, and content-type detection, for inspecting outgoing HTTP
// client requests in stubs and assertions.
type Request struct {
	req        *http.Request
	bodyBytes  []byte
	data       map[string]any
	attributes map[string]any
}

// NewRequest creates a Request from an *http.Request. The body is read
// eagerly so that it can be inspected multiple times.
func NewRequest(req *http.Request) *Request {
	r := &Request{
		req:        req.Clone(req.Context()),
		attributes: make(map[string]any),
	}
	return r
}

// Method returns the HTTP method.
func (r *Request) Method() string {
	if r == nil || r.req == nil {
		return ""
	}
	return r.req.Method
}

// URL returns the full URL string.
func (r *Request) URL() string {
	if r == nil || r.req == nil || r.req.URL == nil {
		return ""
	}
	return r.req.URL.String()
}

// HasHeader reports whether the request has the given header, and
// optionally whether it has the given value.
func (r *Request) HasHeader(key string, value string) bool {
	if r == nil || r.req == nil {
		return false
	}
	if value == "" {
		return r.req.Header.Get(key) != ""
	}
	return r.req.Header.Get(key) == value
}

// HasHeaders reports whether the request has all the given headers.
func (r *Request) HasHeaders(headers map[string]string) bool {
	if r == nil || r.req == nil {
		return false
	}
	for k, v := range headers {
		if r.req.Header.Get(k) != v {
			return false
		}
	}
	return true
}

// Header returns the values for a given header key.
func (r *Request) Header(key string) []string {
	if r == nil || r.req == nil {
		return nil
	}
	return r.req.Header.Values(key)
}

// Headers returns all headers.
func (r *Request) Headers() http.Header {
	if r == nil || r.req == nil {
		return nil
	}
	return r.req.Header
}

// Body returns the request body as a string.
func (r *Request) Body() string {
	if r == nil || r.bodyBytes == nil {
		return ""
	}
	return string(r.bodyBytes)
}

// IsJSON reports whether the Content-Type is application/json.
func (r *Request) IsJSON() bool {
	return r.hasContentType("application/json")
}

// IsForm reports whether the Content-Type is application/x-www-form-urlencoded.
func (r *Request) IsForm() bool {
	return r.hasContentType("application/x-www-form-urlencoded")
}

// IsMultipart reports whether the Content-Type is multipart/form-data.
func (r *Request) IsMultipart() bool {
	if r == nil || r.req == nil {
		return false
	}
	ct := r.req.Header.Get("Content-Type")
	return strings.HasPrefix(ct, "multipart/form-data")
}

// Data returns the decoded request data (form or JSON).
func (r *Request) Data() map[string]any {
	return r.data
}

// WithData sets the decoded request data.
func (r *Request) WithData(data map[string]any) *Request {
	r.data = data
	return r
}

// Attributes returns the request attributes.
func (r *Request) Attributes() map[string]any {
	return r.attributes
}

// SetRequestAttributes sets the request attributes.
func (r *Request) SetRequestAttributes(attrs map[string]any) *Request {
	r.attributes = attrs
	return r
}

// HTTPRequest returns the underlying *http.Request.
func (r *Request) HTTPRequest() *http.Request {
	return r.req
}

func (r *Request) hasContentType(ct string) bool {
	if r == nil || r.req == nil {
		return false
	}
	got := r.req.Header.Get("Content-Type")
	return strings.HasPrefix(strings.ToLower(got), ct)
}

// HasFile reports whether the request has a file with the given name
// in its multipart body.
func (r *Request) HasFile(name, value, filename string) bool {
	if r == nil || r.req == nil {
		return false
	}
	if !r.IsMultipart() {
		return false
	}
	// Parse multipart form from body.
	_ = r.req.ParseMultipartForm(32 << 20)
	if r.req.MultipartForm == nil {
		return false
	}
	files := r.req.MultipartForm.File[name]
	if len(files) == 0 {
		return false
	}
	if filename != "" {
		for _, f := range files {
			if f.Filename == filename {
				return true
			}
		}
		return false
	}
	return true
}

// String returns a human-readable representation of the request.
func (r *Request) String() string {
	if r == nil || r.req == nil {
		return "<nil request>"
	}
	return fmt.Sprintf("%s %s", r.req.Method, r.req.URL.String())
}
