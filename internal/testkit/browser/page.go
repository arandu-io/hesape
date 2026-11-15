package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// Viewport is the size of the page's visible area in CSS pixels.
type Viewport struct {
	Width, Height int
}

// Page is one browser page under a test. It speaks as the person using the
// application does: visit, click the button named Save, fill the field
// labelled Email, expect the text.
//
// Every action waits before it acts and after: for its element to be the only
// visible match, enabled, still and not covered; and for the page to settle --
// the document loaded, YPY quiet (every request settled or ended with nothing
// to swap), the network idle and animations finished. There is no sleep to
// add, and no wait to call.
//
// The page fails the test by itself on an exception the page did not catch, a
// console.error, a response of 500 or more, and a Content-Security-Policy
// violation.
type Page struct {
	t        testing.TB
	c        *chrome
	base     string
	timeout  time.Duration
	quiet    time.Duration
	viewport Viewport

	mu         sync.Mutex
	inflight   map[string]string
	navigating bool
	netAt      time.Time
	pending    []string
	changed    chan struct{}
}

// Option configures a Page.
type Option func(*Page)

// BaseURL is the address relative paths are visited against, such as the
// address of the server the test started.
func BaseURL(url string) Option {
	return func(p *Page) { p.base = strings.TrimRight(url, "/") }
}

// WithViewport is the viewport the page opens with. The default is 1440x900.
func WithViewport(v Viewport) Option { return func(p *Page) { p.viewport = v } }

const (
	defaultTimeout = 5 * time.Second
	defaultQuiet   = 100 * time.Millisecond
	pollInterval   = 25 * time.Millisecond
)

// Open launches Chrome with a profile of its own and opens a page on it. The
// browser is closed when the test ends. With no Chrome installed the test is
// skipped on a developer's machine and fails when CI is set, because a browser
// suite that skips in CI is a suite that never runs.
func Open(t testing.TB, opts ...Option) *Page {
	t.Helper()
	c, err := launch(t.Context())
	if err != nil {
		if errors.Is(err, errNoBrowser) && os.Getenv("CI") == "" {
			t.Skipf("%v", err)
		}
		t.Fatalf("browser: launch: %v", err)
	}
	t.Cleanup(func() {
		if err := c.close(); err != nil {
			t.Errorf("browser: close: %v", err)
		}
	})

	p := &Page{
		t:        t,
		c:        c,
		timeout:  defaultTimeout,
		quiet:    defaultQuiet,
		viewport: Viewport{Width: 1440, Height: 900},
		inflight: map[string]string{},
		changed:  make(chan struct{}),
		netAt:    time.Now(),
	}
	for _, opt := range opts {
		opt(p)
	}
	c.listen(p.event)
	t.Cleanup(p.finish)

	ctx, cancel := context.WithTimeout(t.Context(), p.timeout)
	defer cancel()
	if err := c.addInitScript(ctx, pageScript); err != nil {
		t.Fatalf("browser: install the page script: %v", err)
	}
	if err := c.setViewport(ctx, p.viewport); err != nil {
		t.Fatalf("browser: viewport: %v", err)
	}
	if err := c.setLightMotion(ctx); err != nil {
		t.Fatalf("browser: media: %v", err)
	}
	return p
}

// event records what the browser reports. It runs on the browser's goroutine,
// so it only records: failing the test happens on the test's goroutine, at the
// end of the step.
func (p *Page) event(e event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch e.kind {
	case eventConsole:
		if e.source == "console-api" && e.level == "error" {
			if strings.HasPrefix(e.text, cspPrefix) {
				p.pending = append(p.pending, "Content-Security-Policy "+e.text)
			} else {
				p.pending = append(p.pending, "console.error: "+e.text)
			}
		}
	case eventException:
		p.pending = append(p.pending, "uncaught exception: "+e.text)
	case eventRequest:
		if e.resourceType != "EventSource" && e.resourceType != "WebSocket" {
			p.inflight[e.requestID] = e.resourceType
		}
		p.netAt = e.time
	case eventResponse:
		if e.status >= 500 {
			method := e.method
			if method == "" {
				method = "request to"
			}
			p.pending = append(p.pending, fmt.Sprintf("%s %s answered %d", method, e.url, e.status))
		}
	case eventRequestEnded:
		delete(p.inflight, e.requestID)
		p.netAt = e.time
	case eventNavigationStarted:
		p.navigating = true
		p.netAt = e.time
	case eventNavigationFinished:
		p.navigating = false
		p.netAt = e.time
	}
	close(p.changed)
	p.changed = make(chan struct{})
}

// failures takes the automatic failures queued since the last call.
func (p *Page) failures() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.pending
	p.pending = nil
	return out
}

// step runs one action or expectation under the step timeout and fails the
// test on its error or on any automatic failure that arrived meanwhile.
func (p *Page) step(action, target string, fn func(ctx context.Context) error) {
	p.t.Helper()
	ctx, cancel := context.WithTimeout(p.t.Context(), p.timeout)
	err := fn(ctx)
	cancel()
	if err != nil && ctx.Err() != nil {
		err = fmt.Errorf("%w (waited %s)", err, p.timeout)
	}
	p.report(action, target, err)
}

// report fails the test with the step's error and the automatic failures, the
// automatic ones first: a 500 is the cause of the text that did not appear.
func (p *Page) report(action, target string, err error) {
	p.t.Helper()
	found := p.failures()
	if len(found) == 0 && err == nil {
		return
	}
	var b strings.Builder
	if len(found) > 0 {
		b.WriteString("browser: the page failed: " + strings.Join(found, "; "))
	}
	if err != nil {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("browser: " + action)
		if target != "" {
			b.WriteString(" " + target)
		}
		b.WriteString(": " + err.Error())
	}
	p.t.Fatalf("%s", b.String())
}

// finish runs when the test ends: it reports the automatic failures no step
// reported.
func (p *Page) finish() {
	if found := p.failures(); len(found) > 0 {
		p.t.Errorf("browser: the page failed after the last step: %s", strings.Join(found, "; "))
	}
}

// changes returns the channel closed by the next event.
func (p *Page) changes() <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.changed
}

// pause waits for the next event, for d, or for ctx, whichever comes first. It
// is how every wait here polls: a page that reports something is looked at
// again at once, a quiet one every d.
func (p *Page) pause(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-p.changes():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// acted notes that the test just did something to the page. Whatever the
// action sets going -- a request, a navigation -- starts a moment later, so the
// network counts as active from now: the settle that follows waits out the
// quiet period instead of returning before anything has begun.
func (p *Page) acted() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.netAt = time.Now()
}

// networkBusy says why the network is not idle, or "" when it is.
func (p *Page) networkBusy() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch {
	case p.navigating:
		return "a navigation is loading"
	case len(p.inflight) > 0:
		return fmt.Sprintf("%d request(s) in flight", len(p.inflight))
	case time.Since(p.netAt) < p.quiet:
		return "the network was active " + time.Since(p.netAt).Round(time.Millisecond).String() + " ago"
	}
	return ""
}

type pageState struct {
	Ready     string `json:"ready"`
	YPY       bool   `json:"ypy"`
	Animating bool   `json:"animating"`
	Leaving   int64  `json:"leaving"`
}

const stateExpression = `(window.__hesape ? window.__hesape.state() : {ready: document.readyState, ypy: true, animating: false, leaving: -1})`

// settle waits until the page is still: the document complete and not being
// left, YPY quiet, the network idle and no finite animation running.
func (p *Page) settle(ctx context.Context) error {
	waiting := ""
	for {
		if busy := p.networkBusy(); busy != "" {
			waiting = busy
		} else {
			var s pageState
			err := p.c.evaluate(ctx, stateExpression, &s)
			switch {
			case errors.Is(err, errDocumentChanged):
				waiting = "the document was being replaced"
			case err != nil && ctx.Err() == nil:
				return err
			case err != nil:
			case s.Ready != "complete":
				waiting = "the document is " + s.Ready
			case s.Leaving >= 0 && s.Leaving < 2000:
				waiting = "the document is being left for another"
			case !s.YPY:
				waiting = "YPY requests have not settled"
			case s.Animating:
				waiting = "animations are running"
			default:
				return nil
			}
		}
		if err := p.pause(ctx, pollInterval); err != nil {
			return fmt.Errorf("the page did not settle: %s", waiting)
		}
	}
}

type found struct {
	Status      string  `json:"status"`
	Detail      string  `json:"detail"`
	Description string  `json:"description"`
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
}

// find waits until the locator resolves to one element ready for action:
// "click", "fill", "check", "visible" or "hidden".
func (p *Page) find(ctx context.Context, l Locator, action string) (found, error) {
	if problem := l.problem(); problem != "" {
		return found{}, errors.New(problem)
	}
	encoded, err := json.Marshal(l)
	if err != nil {
		return found{}, err
	}
	expr := fmt.Sprintf(`(window.__hesape ? window.__hesape.find(%s, %q) : {status: 'none', detail: 'the page script is not installed in this document; visit a page first'})`, encoded, action)
	waiting := ""
	for {
		var f found
		err := p.c.evaluate(ctx, expr, &f)
		switch {
		case errors.Is(err, errDocumentChanged):
			waiting = "the document was being replaced"
		case err != nil && ctx.Err() == nil:
			return found{}, err
		case err != nil:
		case f.Status == "ok":
			return f, nil
		case f.Status == "many":
			return found{}, errors.New(f.Detail)
		default:
			waiting = f.Detail
		}
		if err := p.pause(ctx, pollInterval); err != nil {
			if waiting == "" {
				waiting = "no answer from the page"
			}
			return found{}, errors.New(waiting)
		}
	}
}

// call evaluates one of the page script's functions and decodes its result.
func (p *Page) call(ctx context.Context, expr string, out any) error {
	return p.c.evaluate(ctx, "window.__hesape."+expr, out)
}

// resolve turns what Visit and ExpectURL accept into a URL: a URL as it is, or
// a path against BaseURL.
func (p *Page) resolve(target string) (string, error) {
	switch {
	case strings.Contains(target, "://") || strings.HasPrefix(target, "about:") || strings.HasPrefix(target, "data:"):
		return target, nil
	case strings.HasPrefix(target, "/"):
		if p.base == "" {
			return "", fmt.Errorf("%q is a path and the page has no BaseURL to visit it against", target)
		}
		return p.base + target, nil
	}
	return "", fmt.Errorf("%q is neither a URL nor a path that starts with /", target)
}

// Visit loads a page: a URL, or a path against BaseURL.
func (p *Page) Visit(target string) {
	p.t.Helper()
	p.step("visit", target, func(ctx context.Context) error {
		url, err := p.resolve(target)
		if err != nil {
			return err
		}
		if err := p.c.navigate(ctx, url); err != nil {
			return err
		}
		return p.settle(ctx)
	})
}

// Click clicks the element the locator finds with the pointer, at the centre
// of its first box, once it is the only visible match, enabled, still and not
// covered by anything.
func (p *Page) Click(l Locator) {
	p.t.Helper()
	p.step("click", l.String(), func(ctx context.Context) error {
		if err := p.settle(ctx); err != nil {
			return err
		}
		f, err := p.find(ctx, l, "click")
		if err != nil {
			return err
		}
		if err := p.c.click(ctx, f.X, f.Y); err != nil {
			return err
		}
		p.acted()
		return p.settle(ctx)
	})
}

// ClickRole clicks the element with the role and accessible name.
func (p *Page) ClickRole(role, name string) {
	p.t.Helper()
	p.Click(Role(role, name))
}

// Fill types value into the form control labelled text, replacing what was
// there: it clicks the control, selects its content and types.
func (p *Page) Fill(text, value string) {
	p.t.Helper()
	l := label(text)
	p.step("fill", l.String(), func(ctx context.Context) error {
		if err := p.settle(ctx); err != nil {
			return err
		}
		f, err := p.find(ctx, l, "fill")
		if err != nil {
			return err
		}
		if err := p.c.click(ctx, f.X, f.Y); err != nil {
			return err
		}
		var focus struct {
			OK     bool   `json:"ok"`
			Native bool   `json:"native"`
			Detail string `json:"detail"`
		}
		if err := p.call(ctx, "focusTarget()", &focus); err != nil {
			return err
		}
		if !focus.OK {
			return fmt.Errorf("%s did not take the focus: %s", f.Description, focus.Detail)
		}
		switch {
		case focus.Native:
			// A date, time, colour or range input takes no typed text.
			quoted, _ := json.Marshal(value)
			if err := p.call(ctx, "setTargetValue("+string(quoted)+")", nil); err != nil {
				return err
			}
		case value == "":
			if err := p.c.press(ctx, "Delete"); err != nil {
				return err
			}
		default:
			if err := p.c.insertText(ctx, value); err != nil {
				return err
			}
		}
		p.acted()
		return p.settle(ctx)
	})
}

// Check ticks the checkbox or radio the locator finds, clicking it -- or its
// label, when the control itself is hidden behind a styled one -- unless it is
// ticked already.
func (p *Page) Check(l Locator) {
	p.t.Helper()
	p.step("check", l.String(), func(ctx context.Context) error {
		if err := p.settle(ctx); err != nil {
			return err
		}
		f, err := p.find(ctx, l, "check")
		if err != nil {
			return err
		}
		var have bool
		if err := p.call(ctx, "controlChecked()", &have); err != nil {
			return err
		}
		if have {
			return nil
		}
		if err := p.c.click(ctx, f.X, f.Y); err != nil {
			return err
		}
		p.acted()
		if err := p.settle(ctx); err != nil {
			return err
		}
		if err := p.call(ctx, "controlChecked()", &have); err != nil {
			return err
		}
		if !have {
			return fmt.Errorf("clicking %s left it unchecked", f.Description)
		}
		return nil
	})
}

// Press presses a key in the focused element: "Enter", "Tab", "Escape",
// "ArrowDown", a character, or a chord such as "Shift+Tab".
func (p *Page) Press(key string) {
	p.t.Helper()
	p.step("press", key, func(ctx context.Context) error {
		if err := p.settle(ctx); err != nil {
			return err
		}
		if err := p.c.press(ctx, key); err != nil {
			return err
		}
		p.acted()
		return p.settle(ctx)
	})
}

// poll evaluates check until it reports done, the page changing or not, and
// fails with the last explanation when the step runs out of time.
func (p *Page) poll(ctx context.Context, check func(ctx context.Context) (bool, string, error)) error {
	waiting := ""
	for {
		done, why, err := check(ctx)
		switch {
		case errors.Is(err, errDocumentChanged):
			waiting = "the document was being replaced"
		case err != nil && ctx.Err() == nil:
			return err
		case err != nil:
		case done:
			return nil
		default:
			waiting = why
		}
		if err := p.pause(ctx, pollInterval); err != nil {
			return errors.New(waiting)
		}
	}
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ExpectText waits until text is part of what the page reads, whitespace
// collapsed.
func (p *Page) ExpectText(text string) {
	p.t.Helper()
	want := strings.Join(strings.Fields(text), " ")
	p.step("expect text", text, func(ctx context.Context) error {
		return p.poll(ctx, func(ctx context.Context) (bool, string, error) {
			var have string
			err := p.call(ctx, "text()", &have)
			return strings.Contains(have, want), fmt.Sprintf("%q is not on the page, which reads: %q", text, clip(have, 300)), err
		})
	})
}

// ExpectURL waits until the page is at target: a URL compared whole, or a path
// compared with the path (and the query, when target has one).
func (p *Page) ExpectURL(target string) {
	p.t.Helper()
	p.step("expect url", target, func(ctx context.Context) error {
		want, err := p.resolve(target)
		if err != nil {
			return err
		}
		if p.base != "" && strings.HasPrefix(want, p.base+"/") && !strings.Contains(target, "://") {
			want = strings.TrimPrefix(want, p.base)
		}
		return p.poll(ctx, func(ctx context.Context) (bool, string, error) {
			var have struct {
				Href   string `json:"href"`
				Path   string `json:"path"`
				Search string `json:"search"`
			}
			err := p.c.evaluate(ctx, `({href: location.href, path: location.pathname, search: location.search})`, &have)
			got := have.Href
			if strings.HasPrefix(want, "/") {
				got = have.Path
				if strings.Contains(want, "?") {
					got += have.Search
				}
			}
			return got == want, fmt.Sprintf("the page is at %s, not %s", have.Href, want), err
		})
	})
}

// ExpectVisible waits until the locator finds exactly one visible element.
func (p *Page) ExpectVisible(l Locator) {
	p.t.Helper()
	p.step("expect visible", l.String(), func(ctx context.Context) error {
		_, err := p.find(ctx, l, "visible")
		return err
	})
}

// ExpectHidden waits until the locator finds no visible element.
func (p *Page) ExpectHidden(l Locator) {
	p.t.Helper()
	p.step("expect hidden", l.String(), func(ctx context.Context) error {
		_, err := p.find(ctx, l, "hidden")
		return err
	})
}

// Evaluate runs a JavaScript expression in the page and decodes its value
// into out, which may be nil. It is for reading what no expectation reads;
// acting through it skips everything a person's action goes through.
func (p *Page) Evaluate(expression string, out any) {
	p.t.Helper()
	ctx, cancel := context.WithTimeout(p.t.Context(), p.timeout)
	defer cancel()
	if err := p.c.evaluate(ctx, expression, out); err != nil {
		p.t.Fatalf("browser: evaluate: %v", err)
	}
}
