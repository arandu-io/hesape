package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// candidates are where a browser is looked for after CHROME_PATH: the macOS
// application bundles, then the names a Linux distribution installs on PATH.
var candidates = []string{
	"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
	"/Applications/Chromium.app/Contents/MacOS/Chromium",
	"/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
	"google-chrome",
	"google-chrome-stable",
	"chromium",
	"chromium-browser",
	"chrome",
}

// errNoBrowser is wrapped by launch when no Chrome or Chromium is installed.
// A test skips on a developer's machine and fails in CI, because a browser
// suite that skips in CI is a suite that never runs.
var errNoBrowser = errors.New("browser: no Chrome or Chromium was found")

// errDocumentChanged is wrapped by evaluate when the document the expression
// ran in went away under it, as it does during a navigation. The page treats
// it as "not yet" while it waits.
var errDocumentChanged = errors.New("browser: the document changed during the evaluation")

func findChrome(explicit string, places []string) (string, error) {
	if explicit != "" {
		path, err := exec.LookPath(explicit)
		if err != nil {
			return "", fmt.Errorf("browser: CHROME_PATH=%s is not an executable: %w", explicit, err)
		}
		return path, nil
	}
	for _, place := range places {
		if path, err := exec.LookPath(place); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("%w: looked in CHROME_PATH, then %s; install Chrome or set CHROME_PATH",
		errNoBrowser, strings.Join(places, ", "))
}

// eventKind says what an event reports.
type eventKind int

const (
	// eventConsole is a console message, or the browser's own log entry about
	// the page.
	eventConsole eventKind = iota + 1
	// eventException is an exception the page did not catch, including an
	// unhandled promise rejection.
	eventException
	// eventRequest is a request leaving the page.
	eventRequest
	// eventResponse is the status of the answer to a request.
	eventResponse
	// eventRequestEnded is a request that finished or failed.
	eventRequestEnded
	// eventNavigationStarted is the main frame starting to load a document.
	eventNavigationStarted
	// eventNavigationFinished is the main frame done loading, whether the
	// document loaded or the navigation was abandoned.
	eventNavigationFinished
)

// event is something the page did that the test did not ask for directly.
type event struct {
	kind eventKind
	time time.Time

	// level and source qualify a console event; text is the message or the
	// exception.
	level, source, text string

	// requestID ties a request to its response and its end.
	requestID    string
	method       string
	url          string
	resourceType string
	status       int
}

// chrome is one Chrome process with one page, driven over its debugging pipe.
type chrome struct {
	conn    *conn
	cmd     *exec.Cmd
	exited  chan struct{}
	dir     string
	stderr  *tail
	session string
	frame   string

	mu       sync.Mutex
	listener func(event)
	loads    chan string
	methods  map[string]string

	closeOnce sync.Once
	closeErr  error
}

// launch starts a browser and attaches to its page. ctx bounds the start only;
// the browser runs until close.
func launch(ctx context.Context) (*chrome, error) {
	if runtime.GOOS == "windows" {
		return nil, errors.New("browser: the debugging pipe needs inherited file descriptors 3 and 4, which Windows does not provide")
	}
	path, err := findChrome(os.Getenv("CHROME_PATH"), candidates)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "hesape-browser-")
	if err != nil {
		return nil, fmt.Errorf("browser: profile directory: %w", err)
	}

	// The browser reads commands from fd 3 and writes replies to fd 4.
	browserIn, toBrowser, err := os.Pipe()
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("browser: pipe: %w", err)
	}
	fromBrowser, browserOut, err := os.Pipe()
	if err != nil {
		_ = browserIn.Close()
		_ = toBrowser.Close()
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("browser: pipe: %w", err)
	}

	c := &chrome{
		dir:     dir,
		stderr:  &tail{},
		exited:  make(chan struct{}),
		loads:   make(chan string, 32),
		methods: map[string]string{},
	}
	c.cmd = exec.Command(path, arguments(dir)...)
	c.cmd.ExtraFiles = []*os.File{browserIn, browserOut}
	c.cmd.Stderr = c.stderr
	if err := c.cmd.Start(); err != nil {
		for _, f := range []*os.File{browserIn, browserOut, toBrowser, fromBrowser} {
			_ = f.Close()
		}
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("browser: start %s: %w", path, err)
	}
	_ = browserIn.Close()
	_ = browserOut.Close()
	go func() {
		_ = c.cmd.Wait()
		close(c.exited)
	}()
	c.conn = newConn(fromBrowser, toBrowser, c.explain)
	go func() {
		<-c.conn.done
		_ = fromBrowser.Close()
	}()

	if err := c.attach(ctx); err != nil {
		_ = c.close()
		return nil, fmt.Errorf("browser: launch %s: %w", path, err)
	}
	return c, nil
}

// arguments are the browser's flags: a profile of its own, the pipe, and
// every background service that could make a run differ from the last turned
// off.
func arguments(dir string) []string {
	args := []string{
		"--headless=new",
		"--remote-debugging-pipe",
		"--user-data-dir=" + dir,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-background-networking",
		"--disable-background-timer-throttling",
		"--disable-backgrounding-occluded-windows",
		"--disable-renderer-backgrounding",
		"--disable-breakpad",
		"--disable-component-update",
		"--disable-default-apps",
		"--disable-extensions",
		"--disable-sync",
		"--disable-features=Translate,OptimizationHints,MediaRouter,DialMediaRouteProvider",
		"--metrics-recording-only",
		"--mute-audio",
		"--password-store=basic",
		"--use-mock-keychain",
		"--force-color-profile=srgb",
		"--window-size=1440,900",
	}
	if runtime.GOOS == "linux" && os.Geteuid() == 0 {
		// Chrome refuses to start its sandbox as root, which is how most CI
		// containers run.
		args = append(args, "--no-sandbox")
	}
	return append(args, "about:blank")
}

// explain adds what the browser printed before the pipe closed, which is
// where a browser that could not start says why.
func (c *chrome) explain(err error) error {
	if out := strings.TrimSpace(c.stderr.String()); out != "" {
		return fmt.Errorf("%w; the browser wrote: %s", err, out)
	}
	return err
}

// attach finds the page target, attaches a flat session to it and enables
// the domains the events are read from.
func (c *chrome) attach(ctx context.Context) error {
	var targets struct {
		TargetInfos []struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
		} `json:"targetInfos"`
	}
	if err := c.conn.call(ctx, "", "Target.getTargets", nil, &targets); err != nil {
		return err
	}
	target := ""
	for _, info := range targets.TargetInfos {
		if info.Type == "page" {
			target = info.TargetID
			break
		}
	}
	if target == "" {
		var created struct {
			TargetID string `json:"targetId"`
		}
		if err := c.conn.call(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank"}, &created); err != nil {
			return err
		}
		target = created.TargetID
	}
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err := c.conn.call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": target, "flatten": true}, &attached); err != nil {
		return err
	}
	c.session = attached.SessionID
	c.conn.listen(c.session, c.handle)

	for _, method := range []string{"Page.enable", "Runtime.enable", "Network.enable", "Log.enable"} {
		if err := c.call(ctx, method, nil, nil); err != nil {
			return err
		}
	}
	if err := c.call(ctx, "Page.setLifecycleEventsEnabled", map[string]any{"enabled": true}, nil); err != nil {
		return err
	}
	var tree struct {
		FrameTree struct {
			Frame struct {
				ID string `json:"id"`
			} `json:"frame"`
		} `json:"frameTree"`
	}
	if err := c.call(ctx, "Page.getFrameTree", nil, &tree); err != nil {
		return err
	}
	c.mu.Lock()
	c.frame = tree.FrameTree.Frame.ID
	c.mu.Unlock()
	return nil
}

func (c *chrome) call(ctx context.Context, method string, params, result any) error {
	return c.conn.call(ctx, c.session, method, params, result)
}

// listen hands every event from now on to fn. It is called once, before the
// first navigation, and fn must not block and must not call the browser.
func (c *chrome) listen(fn func(event)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.listener = fn
}

func (c *chrome) emit(e event) {
	c.mu.Lock()
	fn := c.listener
	c.mu.Unlock()
	if fn != nil {
		e.time = time.Now()
		fn(e)
	}
}

// handle translates the session's events. It runs on the reading goroutine.
func (c *chrome) handle(method string, raw json.RawMessage) {
	switch method {
	case "Runtime.consoleAPICalled":
		var p struct {
			Type string `json:"type"`
			Args []struct {
				Type                string          `json:"type"`
				Value               json.RawMessage `json:"value"`
				UnserializableValue string          `json:"unserializableValue"`
				Description         string          `json:"description"`
			} `json:"args"`
		}
		if json.Unmarshal(raw, &p) != nil {
			return
		}
		parts := make([]string, 0, len(p.Args))
		for _, a := range p.Args {
			switch {
			case a.Type == "string" && len(a.Value) > 0:
				var s string
				_ = json.Unmarshal(a.Value, &s)
				parts = append(parts, s)
			case len(a.Value) > 0:
				parts = append(parts, string(a.Value))
			case a.UnserializableValue != "":
				parts = append(parts, a.UnserializableValue)
			default:
				parts = append(parts, a.Description)
			}
		}
		level := "log"
		if p.Type == "error" || p.Type == "assert" {
			level = "error"
		}
		c.emit(event{kind: eventConsole, level: level, source: "console-api", text: strings.Join(parts, " ")})
	case "Runtime.exceptionThrown":
		var p struct {
			Details struct {
				Text      string `json:"text"`
				Exception struct {
					Description string `json:"description"`
				} `json:"exception"`
			} `json:"exceptionDetails"`
		}
		if json.Unmarshal(raw, &p) != nil {
			return
		}
		text := p.Details.Exception.Description
		if text == "" {
			text = p.Details.Text
		}
		c.emit(event{kind: eventException, text: text})
	case "Network.requestWillBeSent":
		var p struct {
			RequestID string `json:"requestId"`
			Type      string `json:"type"`
			Request   struct {
				URL    string `json:"url"`
				Method string `json:"method"`
			} `json:"request"`
			RedirectResponse *struct {
				URL    string `json:"url"`
				Status int    `json:"status"`
			} `json:"redirectResponse"`
		}
		if json.Unmarshal(raw, &p) != nil {
			return
		}
		c.mu.Lock()
		prior := c.methods[p.RequestID]
		c.methods[p.RequestID] = p.Request.Method
		c.mu.Unlock()
		if p.RedirectResponse != nil {
			c.emit(event{kind: eventResponse, requestID: p.RequestID, method: prior, url: p.RedirectResponse.URL,
				resourceType: p.Type, status: p.RedirectResponse.Status})
		}
		c.emit(event{kind: eventRequest, requestID: p.RequestID, method: p.Request.Method, url: p.Request.URL, resourceType: p.Type})
	case "Network.responseReceived":
		var p struct {
			RequestID string `json:"requestId"`
			Type      string `json:"type"`
			Response  struct {
				URL    string `json:"url"`
				Status int    `json:"status"`
			} `json:"response"`
		}
		if json.Unmarshal(raw, &p) != nil {
			return
		}
		c.mu.Lock()
		method := c.methods[p.RequestID]
		c.mu.Unlock()
		c.emit(event{kind: eventResponse, requestID: p.RequestID, method: method, url: p.Response.URL,
			resourceType: p.Type, status: p.Response.Status})
	case "Network.loadingFinished", "Network.loadingFailed":
		var p struct {
			RequestID string `json:"requestId"`
		}
		if json.Unmarshal(raw, &p) != nil {
			return
		}
		c.mu.Lock()
		delete(c.methods, p.RequestID)
		c.mu.Unlock()
		c.emit(event{kind: eventRequestEnded, requestID: p.RequestID})
	case "Page.frameStartedLoading", "Page.frameStoppedLoading":
		var p struct {
			FrameID string `json:"frameId"`
		}
		if json.Unmarshal(raw, &p) != nil || !c.isMain(p.FrameID) {
			return
		}
		kind := eventNavigationStarted
		if method == "Page.frameStoppedLoading" {
			kind = eventNavigationFinished
		}
		c.emit(event{kind: kind})
	case "Page.lifecycleEvent":
		var p struct {
			FrameID  string `json:"frameId"`
			LoaderID string `json:"loaderId"`
			Name     string `json:"name"`
		}
		if json.Unmarshal(raw, &p) != nil || p.Name != "load" || !c.isMain(p.FrameID) {
			return
		}
		select {
		case c.loads <- p.LoaderID:
		default:
		}
	}
}

func (c *chrome) isMain(frame string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return frame != "" && frame == c.frame
}

// navigate loads url and waits for the main frame's load event of that
// navigation.
func (c *chrome) navigate(ctx context.Context, url string) error {
	for drained := false; !drained; {
		select {
		case <-c.loads:
		default:
			drained = true
		}
	}
	var nav struct {
		LoaderID  string `json:"loaderId"`
		ErrorText string `json:"errorText"`
	}
	if err := c.call(ctx, "Page.navigate", map[string]any{"url": url}, &nav); err != nil {
		return err
	}
	if nav.ErrorText != "" {
		return fmt.Errorf("navigate to %s: %s", url, nav.ErrorText)
	}
	if nav.LoaderID == "" {
		return nil // a same-document navigation has no load to wait for
	}
	for {
		select {
		case loader := <-c.loads:
			if loader == nav.LoaderID {
				return nil
			}
		case <-ctx.Done():
			return fmt.Errorf("navigate to %s: the page did not finish loading: %w", url, ctx.Err())
		case <-c.conn.done:
			return fmt.Errorf("navigate to %s: %w", url, c.conn.closed())
		}
	}
}

// documentGone are the errors Chrome answers an evaluation with when the
// document it ran in was replaced under it.
var documentGone = []string{
	"Execution context was destroyed",
	"Cannot find context with specified id",
	"Cannot find default execution context",
	"Inspected target navigated or closed",
	"Promise was collected",
}

// evaluate runs expression in the page's main frame, awaits it when it is a
// promise, and decodes its JSON value into result, which may be nil.
func (c *chrome) evaluate(ctx context.Context, expression string, result any) error {
	var r struct {
		Result struct {
			Type  string          `json:"type"`
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	err := c.call(ctx, "Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true, "awaitPromise": true}, &r)
	if err != nil {
		for _, gone := range documentGone {
			if strings.Contains(err.Error(), gone) {
				return fmt.Errorf("%w: %w", errDocumentChanged, err)
			}
		}
		return err
	}
	if r.ExceptionDetails != nil {
		text := r.ExceptionDetails.Exception.Description
		if text == "" {
			text = r.ExceptionDetails.Text
		}
		return fmt.Errorf("javascript: %s", text)
	}
	if result == nil {
		return nil
	}
	if len(r.Result.Value) == 0 {
		return fmt.Errorf("javascript: the expression evaluated to %s, which has no JSON value", r.Result.Type)
	}
	if err := json.Unmarshal(r.Result.Value, result); err != nil {
		return fmt.Errorf("javascript: decode the value: %w", err)
	}
	return nil
}

// addInitScript runs source in every document from now on, before the
// document's own scripts and regardless of its CSP.
func (c *chrome) addInitScript(ctx context.Context, source string) error {
	return c.call(ctx, "Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": source}, nil)
}

// click moves the mouse to x, y in viewport pixels, presses the left button
// and releases it.
func (c *chrome) click(ctx context.Context, x, y float64) error {
	for _, e := range []map[string]any{
		{"type": "mouseMoved", "x": x, "y": y},
		{"type": "mousePressed", "x": x, "y": y, "button": "left", "buttons": 1, "clickCount": 1},
		{"type": "mouseReleased", "x": x, "y": y, "button": "left", "buttons": 0, "clickCount": 1},
	} {
		if err := c.call(ctx, "Input.dispatchMouseEvent", e, nil); err != nil {
			return err
		}
	}
	return nil
}

// insertText types text into the focused element as one input.
func (c *chrome) insertText(ctx context.Context, text string) error {
	return c.call(ctx, "Input.insertText", map[string]any{"text": text}, nil)
}

// keyDef is what Chrome needs to synthesise one key.
type keyDef struct {
	key, code, text string
	vk              int
}

var keys = map[string]keyDef{
	"Enter":      {"Enter", "Enter", "\r", 13},
	"Tab":        {"Tab", "Tab", "", 9},
	"Escape":     {"Escape", "Escape", "", 27},
	"Backspace":  {"Backspace", "Backspace", "", 8},
	"Delete":     {"Delete", "Delete", "", 46},
	"Space":      {" ", "Space", " ", 32},
	"ArrowLeft":  {"ArrowLeft", "ArrowLeft", "", 37},
	"ArrowUp":    {"ArrowUp", "ArrowUp", "", 38},
	"ArrowRight": {"ArrowRight", "ArrowRight", "", 39},
	"ArrowDown":  {"ArrowDown", "ArrowDown", "", 40},
	"Home":       {"Home", "Home", "", 36},
	"End":        {"End", "End", "", 35},
	"PageUp":     {"PageUp", "PageUp", "", 33},
	"PageDown":   {"PageDown", "PageDown", "", 34},
}

var modifiers = map[string]int{"Alt": 1, "Control": 2, "Ctrl": 2, "Meta": 4, "Shift": 8}

// press presses and releases one key: a key name such as "Enter", "Tab",
// "Escape" or "ArrowDown", or a single character, optionally prefixed by
// modifiers as in "Shift+Tab".
func (c *chrome) press(ctx context.Context, name string) error {
	parts := strings.Split(name, "+")
	mask := 0
	for _, m := range parts[:len(parts)-1] {
		bit, ok := modifiers[m]
		if !ok {
			return fmt.Errorf("press %q: unknown modifier %q", name, m)
		}
		mask |= bit
	}
	last := parts[len(parts)-1]
	k, ok := keys[last]
	if !ok {
		if len([]rune(last)) != 1 {
			return fmt.Errorf("press %q: unknown key %q", name, last)
		}
		k = keyDef{key: last, text: last}
		if r := []rune(strings.ToUpper(last))[0]; r >= 'A' && r <= 'Z' {
			k.code, k.vk = "Key"+string(r), int(r)
		} else if r >= '0' && r <= '9' {
			k.code, k.vk = "Digit"+string(r), int(r)
		}
	}
	if mask&(2|4|1) != 0 {
		k.text = "" // a chord types nothing
	}
	down := map[string]any{"type": "rawKeyDown", "key": k.key, "code": k.code, "windowsVirtualKeyCode": k.vk, "modifiers": mask}
	if k.text != "" {
		down["type"], down["text"], down["unmodifiedText"] = "keyDown", k.text, k.text
	}
	if err := c.call(ctx, "Input.dispatchKeyEvent", down, nil); err != nil {
		return err
	}
	return c.call(ctx, "Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": k.key, "code": k.code, "windowsVirtualKeyCode": k.vk, "modifiers": mask}, nil)
}

// setViewport resizes the page's viewport.
func (c *chrome) setViewport(ctx context.Context, v Viewport) error {
	return c.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{
		"width": v.Width, "height": v.Height, "deviceScaleFactor": 1, "mobile": false,
	}, nil)
}

// setLightMotion makes the media queries answer the way a test expects: the
// light colour scheme, and motion allowed so the animations a component
// declares are the ones that run.
func (c *chrome) setLightMotion(ctx context.Context) error {
	return c.call(ctx, "Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{
		{"name": "prefers-color-scheme", "value": "light"},
		{"name": "prefers-reduced-motion", "value": "no-preference"},
	}}, nil)
}

// close asks the browser to quit, kills it if it does not, and removes its
// profile directory.
func (c *chrome) close() error {
	c.closeOnce.Do(func() {
		if c.conn != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = c.conn.call(ctx, "", "Browser.close", nil, nil)
			cancel()
		}
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-c.exited:
		case <-timer.C:
			_ = c.cmd.Process.Kill()
			<-c.exited
		}
		timer.Stop()
		if w, ok := c.conn.w.(*os.File); ok {
			_ = w.Close()
		}
		if err := os.RemoveAll(c.dir); err != nil {
			c.closeErr = fmt.Errorf("browser: remove the profile directory: %w", err)
		}
	})
	return c.closeErr
}

// tail keeps the last few kilobytes the browser wrote to stderr.
type tail struct {
	mu  sync.Mutex
	buf []byte
}

const tailSize = 4 << 10

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > tailSize {
		t.buf = t.buf[len(t.buf)-tailSize:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(bytes.ToValidUTF8(t.buf, nil))
}
