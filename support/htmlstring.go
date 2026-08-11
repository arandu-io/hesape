package support

// Htmlable answers to Illuminate\Contracts\Support\Htmlable: a value that knows
// how to present itself as HTML that must not be escaped again.
type Htmlable interface {
	// ToHtml answers to Htmlable::toHtml.
	ToHtml() string
}

// Jsonable answers to Illuminate\Contracts\Support\Jsonable. The PHP takes an
// $options flag and returns a string; encoding/json has no flags and fails with
// an error, so this returns (string, error).
type Jsonable interface {
	// ToJson answers to Jsonable::toJson.
	ToJson() (string, error)
}

// HtmlString answers to Illuminate\Support\HtmlString: a string already safe to
// write into a template, which the view layer must not escape.
type HtmlString struct {
	html string
}

// NewHtmlString answers to HtmlString::__construct. The PHP default is the
// empty string, which is the zero value here.
func NewHtmlString(html string) *HtmlString { return &HtmlString{html: html} }

// ToHtml answers to HtmlString::toHtml.
func (h *HtmlString) ToHtml() string {
	if h == nil {
		return ""
	}
	return h.html
}

// IsEmpty answers to HtmlString::isEmpty.
func (h *HtmlString) IsEmpty() bool { return h.ToHtml() == "" }

// IsNotEmpty answers to HtmlString::isNotEmpty.
func (h *HtmlString) IsNotEmpty() bool { return !h.IsEmpty() }

// String answers to HtmlString::__toString.
func (h *HtmlString) String() string { return h.ToHtml() }
