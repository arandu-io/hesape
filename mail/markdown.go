package mail

import (
	"errors"
	"strings"
	"sync"

	"github.com/arandu-io/hesape/str"
)

// errNoMarkdownView is what a Markdown built without a view factory answers,
// rather than panicking on a nil interface three frames down.
var errNoMarkdownView = errors.New("mail: the markdown renderer has no view factory")

// MarkdownConfig is how a [Markdown] is configured: the theme, and the paths
// its components are looked up in.
type MarkdownConfig struct {
	// Theme is the name of the theme view, "default" when empty.
	Theme string
	// Paths are the view roots the mail components are looked up in, before
	// this package's own.
	Paths []string
}

// Inliner moves a stylesheet into a rendered document.
//
// A mail client is not a browser: Outlook drops most of a <style> element and
// several webmail clients drop it entirely, so a theme that is only a
// stylesheet arrives unstyled. Moving each rule onto the element it matches is
// what makes an HTML e-mail look the same twice.
//
// The default inliner does not do that. Doing it needs an HTML parser, and this
// package carries no third-party dependency, so what ships here keeps the
// stylesheet in a <style> element and an application that wants true inlining
// supplies its own.
type Inliner interface {
	Convert(html, css string) string
}

// styleElementInliner is the default [Inliner]: it puts the theme's stylesheet
// into the document rather than onto its elements.
type styleElementInliner struct{}

func (styleElementInliner) Convert(html, css string) string {
	if strings.TrimSpace(css) == "" {
		return html
	}
	style := "<style type=\"text/css\">\n" + css + "\n</style>"

	if at := strings.Index(html, "</head>"); at >= 0 {
		return html[:at] + style + html[at:]
	}
	return style + html
}

// Markdown is the renderer behind a markdown mailable, which draws the HTML
// part and the plain-text part from one template.
//
// That is the whole reason markdown mailables exist: writing the two parts by
// hand means writing the message twice and letting them drift.
type Markdown struct {
	view           Renderer
	theme          string
	componentPaths []string
	inliner        Inliner
}

// securedEncoding is the process-wide switch [Parse] reads. It is behind a
// mutex because it is set at boot and read from every request.
var securedEncoding struct {
	sync.RWMutex
	on bool
}

// NewMarkdown builds a renderer. An empty theme means "default".
func NewMarkdown(view Renderer, options MarkdownConfig) *Markdown {
	m := &Markdown{view: view, theme: options.Theme}
	if m.theme == "" {
		m.theme = "default"
	}
	m.LoadComponentsFrom(options.Paths)
	return m
}

func (m *Markdown) clone() *Markdown {
	out := *m
	out.componentPaths = append([]string(nil), m.componentPaths...)
	return &out
}

// Render draws the markdown template into HTML, with the theme's stylesheet
// applied by the inliner. The inliner is optional and overrides the one set
// with [Markdown.SetInliner] for this call alone.
func (m *Markdown) Render(view string, data any, inliner ...Inliner) (string, error) {
	if m.view == nil {
		return "", errNoMarkdownView
	}
	contents, err := m.view.RenderToString(view, data)
	if err != nil {
		return "", err
	}

	// The theme is a view whose whole output is CSS. A theme that is not
	// registered is not an error: the message goes out unstyled rather than not
	// at all.
	css, _ := m.view.RenderToString(m.themeView(), data)

	use := m.inliner
	if len(inliner) > 0 && inliner[0] != nil {
		use = inliner[0]
	}
	if use == nil {
		use = styleElementInliner{}
	}
	return use.Convert(contents, css), nil
}

// RenderText draws the markdown template into the plain-text part.
//
// Runs of three or more newlines are collapsed to two: a template full of
// conditionals leaves blank lines behind wherever a branch did not fire, and a
// message that opens with nine of them looks broken.
func (m *Markdown) RenderText(view string, data any) (string, error) {
	if m.view == nil {
		return "", errNoMarkdownView
	}
	contents, err := m.view.RenderToString(view, data)
	if err != nil {
		return "", err
	}
	return collapseBlankLines(contents), nil
}

func collapseBlankLines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return s
}

// themeView is the view name the theme is registered under: mail.themes.<theme>
// unless the theme already names a path of its own.
func (m *Markdown) themeView() string {
	if strings.Contains(m.theme, ".") {
		return m.theme
	}
	return "mail.themes." + m.theme
}

// HTMLComponentPaths is where the HTML components are looked up.
func (m *Markdown) HTMLComponentPaths() []string {
	return suffixed(m.paths(), "/html")
}

// TextComponentPaths is where the plain-text components are looked up.
func (m *Markdown) TextComponentPaths() []string {
	return suffixed(m.paths(), "/text")
}

func suffixed(paths []string, suffix string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		out = append(out, p+suffix)
	}
	return out
}

// paths is the registered component paths plus the default one, without
// repeats.
func (m *Markdown) paths() []string {
	out := make([]string, 0, len(m.componentPaths)+1)
	seen := map[string]bool{}
	for _, p := range append(append([]string{}, m.componentPaths...), defaultComponentPath) {
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// defaultComponentPath is where the mail components are looked for when the
// configuration names no path of its own. It is a view prefix rather than a
// directory, because the components are views the application registers.
const defaultComponentPath = "mail"

// LoadComponentsFrom replaces the registered component paths.
func (m *Markdown) LoadComponentsFrom(paths []string) {
	m.componentPaths = append([]string(nil), paths...)
}

// Theme sets the theme this renderer draws with.
func (m *Markdown) Theme(theme string) *Markdown {
	m.theme = theme
	return m
}

// GetTheme is the theme this renderer draws with.
func (m *Markdown) GetTheme() string { return m.theme }

// SetInliner replaces the stylesheet inliner for every render this renderer
// does, where the last argument to [Markdown.Render] replaces it for one.
func (m *Markdown) SetInliner(in Inliner) *Markdown {
	m.inliner = in
	return m
}

// MarkdownConverter is what [Converter] answers: the configured markdown
// renderer, which is the one in hesape/str, because a markdown library is a
// dependency this package does not carry.
type MarkdownConverter struct {
	// AllowUnsafeLinks lets javascript: and data: URLs through, and defaults to
	// false.
	AllowUnsafeLinks bool
	// HTMLInput decides what happens to raw HTML in the source: "escape" turns
	// it into text.
	HTMLInput string
}

// Converter builds a markdown renderer. The configuration is optional, and the
// zero value escapes nothing and strips unsafe links.
func Converter(config ...MarkdownConverter) *MarkdownConverter {
	out := &MarkdownConverter{}
	if len(config) > 0 {
		*out = config[0]
	}
	return out
}

// Convert renders markdown into HTML.
func (c *MarkdownConverter) Convert(text string) string {
	if c.HTMLInput == "escape" {
		text = escaped(text)
	}
	html := str.Markdown(text)
	if !c.AllowUnsafeLinks {
		html = stripUnsafeLinks(html)
	}
	return html
}

// stripUnsafeLinks is what AllowUnsafeLinks turns off. A javascript: or data:
// href in a mail body is a link that runs when a webmail client renders it, and
// the source of that body is often a value somebody typed.
func stripUnsafeLinks(html string) string {
	for _, scheme := range []string{"javascript:", "data:", "vbscript:"} {
		for _, attr := range []string{`href="`, `href='`, `src="`, `src='`} {
			html = strings.ReplaceAll(html, attr+scheme, attr)
			html = strings.ReplaceAll(html, attr+strings.ToUpper(scheme), attr)
		}
	}
	return html
}

// Parse renders markdown into HTML. The encoded argument is optional.
//
// With secured encoding on, or with encoded true, the source has its brackets
// and angle brackets escaped before conversion: the markdown being parsed came
// from a value somebody typed, and a [label](javascript:...) in it is a link
// the reader did not write.
func Parse(text string, encoded ...bool) string {
	secured := SecuredEncoding()
	if len(encoded) > 0 && encoded[0] {
		secured = true
	}
	if !secured {
		return Converter().Convert(text)
	}

	replaced := strings.NewReplacer("[", `\[`, "<", `\<`).Replace(text)
	return Converter(MarkdownConverter{HTMLInput: "escape"}).Convert(replaced)
}

// WithSecuredEncoding turns on the escaping [Parse] describes, for every parse
// from here on. It is process-wide state, so it belongs at boot rather than in
// a request.
func WithSecuredEncoding() {
	securedEncoding.Lock()
	defer securedEncoding.Unlock()
	securedEncoding.on = true
}

// WithoutSecuredEncoding turns it off again.
func WithoutSecuredEncoding() {
	securedEncoding.Lock()
	defer securedEncoding.Unlock()
	securedEncoding.on = false
}

// SecuredEncoding reports whether the escaping is on.
func SecuredEncoding() bool {
	securedEncoding.RLock()
	defer securedEncoding.RUnlock()
	return securedEncoding.on
}

// FlushState puts the process-wide state back where it started, and is what a
// test calls between cases.
func FlushState() { WithoutSecuredEncoding() }
