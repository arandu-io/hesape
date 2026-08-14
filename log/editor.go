package log

import (
	"strconv"
	"strings"
)

// PathRewrite translates a path the process sees into the path the editor sees.
//
// It answers the base_path the PHP configuration carries beside the editor name,
// and it exists for one case: the binary was built and runs in a container, so
// every recorded frame names /app/handler.go, while the editor is on the machine
// outside and knows the file as /Users/ana/project/handler.go. Without the
// translation the link opens nothing, and it opens nothing silently.
type PathRewrite struct {
	// From is the root the running process sees, "/app" above.
	From string
	// To is the root the editor sees, "/Users/ana/project" above.
	To string
}

// editorHrefs is the editor table, keyed by the name the configuration carries.
//
// The first fourteen are Laravel's, with its spellings. The last three are not:
// cursor, goland and zed are what the people writing Go use, and a table that
// only knows the editors a PHP framework ships for would send every one of them
// to a dead link.
//
// {file} and {line} are filled in. They are not a template language anybody
// configures -- the set is closed, and an unknown name gets no link at all.
var editorHrefs = map[string]string{
	"atom":                   "atom://core/open/file?filename={file}&line={line}",
	"emacs":                  "emacs://open?url=file://{file}&line={line}",
	"idea":                   "idea://open?file={file}&line={line}",
	"macvim":                 "mvim://open/?url=file://{file}&line={line}",
	"nova":                   "nova://core/open/file?filename={file}&line={line}",
	"phpstorm":               "phpstorm://open?file={file}&line={line}",
	"sublime":                "subl://open?url=file://{file}&line={line}",
	"textmate":               "txmt://open?url=file://{file}&line={line}",
	"vscode":                 "vscode://file{file}:{line}",
	"vscode_insiders":        "vscode-insiders://file{file}:{line}",
	"vscode_remote":          "vscode://vscode-remote{file}:{line}",
	"vscode_insiders_remote": "vscode-insiders://vscode-remote{file}:{line}",
	"vscodium":               "vscodium://file{file}:{line}",
	"xdebug":                 "xdebug://{file}@{line}",
	"cursor":                 "cursor://file{file}:{line}",
	"goland":                 "jetbrains://goland/navigate/reference?path={file}:{line}",
	"zed":                    "zed://file{file}:{line}",
}

// EditorLink is Frame::editorHref, which resolves the href out of the editor
// table of the ResolvesDumpSource trait.
//
// EditorLink builds the link that opens a file straight in the IDE, at the line.
// It returns "" when there is no link to build, and the caller renders the frame
// without one.
//
// It lives here rather than with the error page because two things need it now
// -- the error page and the console -- and a second copy is a second place to
// add the next editor. The editor name comes from the log configuration.
//
// The scheme has to reach the template as template.URL: html/template rewrites
// an unknown scheme to #ZgotmplZ, which turns every link on the page into a dead
// one and gives no hint why.
//
// # What this used to do
//
// It knew four names and sent everything else to VS Code. So emacs, phpstorm,
// sublime and idea -- four of the fourteen the PHP table holds -- all produced
// vscode://, which is a link that opens nothing for anybody who configured them:
// the configuration was read, echoed back in the href, and ignored. An unset
// editor got vscode:// too, on the argument that a wrong scheme fails visibly
// where a missing link fails silently. It does not fail visibly. Clicking a
// vscode:// link with no VS Code installed does nothing at all, and it does
// nothing in a way that reads as "the debug page is broken" rather than "the
// editor is not configured". PHP emits no link there, and neither does this now.
//
// An unknown name is the same answer as an unset one. The table is a closed set,
// so a name outside it is a typo in the configuration, and a typo that produced
// a link into a scheme nobody registered would look exactly like a working one.
//
// rewrite is optional and only the first is read; without it the path is used as
// it was recorded. See [PathRewrite] for the case it is there for -- a link
// built inside a container that has to open a file outside it.
func EditorLink(editor, file string, line int, rewrite ...PathRewrite) string {
	href, known := editorHrefs[editor]
	if !known {
		return ""
	}
	if len(rewrite) > 0 && rewrite[0].From != "" {
		file = strings.Replace(file, rewrite[0].From, rewrite[0].To, 1)
	}
	return strings.NewReplacer("{file}", file, "{line}", strconv.Itoa(line)).Replace(href)
}
