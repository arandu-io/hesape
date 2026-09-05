package publish_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/arandu-io/hesape/publish"
)

const withBlock = `package views

func Page() string { return "one" }

// arandu:begin custom
// Anything the shape does not say.
// arandu:end custom
`

const withBlockChanged = `package views

func Page() string { return "two" }

// arandu:begin custom
// Anything the shape does not say.
// arandu:end custom
`

func tree(files map[string]string) fstest.MapFS {
	out := fstest.MapFS{}
	for name, content := range files {
		out[name] = &fstest.MapFile{Data: []byte(content)}
	}
	return out
}

// source publishes Go files, whose custom block is written in Go comments. The
// view tests build their own, because a view carries its markers in view
// syntax.
func source(files map[string]string) publish.Source {
	return publish.Source{Files: tree(files), To: "app", Origin: "example:config"}
}

// publishOnce plans and applies, and fails on anything unexpected.
func publishOnce(t *testing.T, root string, lock *publish.Lock, opts publish.Options, sources ...publish.Source) []publish.Change {
	t.Helper()
	changes, err := publish.Plan(root, lock, opts, sources...)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if _, err := publish.Apply(root, lock, changes); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return changes
}

func read(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return string(content)
}

func TestPublishingCreatesWhatIsNotThere(t *testing.T) {
	root := t.TempDir()
	lock := publish.NewLock()

	changes := publishOnce(t, root, lock, publish.Options{}, source(map[string]string{
		"Http/HomeController.go": withBlock,
		"Http/about.txt":         "about",
	}))

	if len(changes) != 2 {
		t.Fatalf("got %d changes, want 2: %+v", len(changes), changes)
	}
	for _, change := range changes {
		if change.Action != publish.Create {
			t.Errorf("%s = %s, want create", change.Path, change.Action)
		}
	}
	if got := read(t, filepath.Join(root, "app", "Http", "about.txt")); got != "about" {
		t.Errorf("about.txt = %q", got)
	}
	if len(lock.Paths()) != 2 {
		t.Errorf("the lock recorded %v", lock.Paths())
	}
}

// TestAPlanWritesNothing. A publication nobody can look at first is one people
// run in a branch and read afterwards.
func TestAPlanWritesNothing(t *testing.T) {
	root := t.TempDir()

	changes, err := publish.Plan(root, publish.NewLock(), publish.Options{}, source(map[string]string{
		"Http/HomeController.go": withBlock,
	}))
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(changes) != 1 || changes[0].Action != publish.Create {
		t.Fatalf("changes = %+v", changes)
	}
	if _, err := os.Stat(filepath.Join(root, "app", "Http", "HomeController.go")); !os.IsNotExist(err) {
		t.Error("planning wrote the file")
	}
	if got := string(changes[0].Content()); got != withBlock {
		t.Error("the preview does not carry what would be written")
	}
	if changes[0].Existing() != nil {
		t.Error("nothing is there, and the preview says something is")
	}
}

// TestPublishingTwiceChangesNothing. Running the command because you are not
// sure whether you ran it has to be free.
func TestPublishingTwiceChangesNothing(t *testing.T) {
	root := t.TempDir()
	lock := publish.NewLock()
	files := map[string]string{"Http/HomeController.go": withBlock}

	publishOnce(t, root, lock, publish.Options{}, source(files))
	target := filepath.Join(root, "app", "Http", "HomeController.go")
	first := read(t, target)

	changes, err := publish.Plan(root, lock, publish.Options{}, source(files))
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(changes) != 1 || changes[0].Action != publish.Unchanged {
		t.Fatalf("changes = %+v, want one unchanged", changes)
	}

	applied, err := publish.Apply(root, lock, changes)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(applied) != 0 {
		t.Errorf("the second run wrote %+v", applied)
	}
	if read(t, target) != first {
		t.Error("the second run changed the file")
	}
}

// TestACustomBlockSurvivesRepublication. Without this the mechanism is a
// one-time tool: nobody runs a command twice after it has eaten their work.
func TestACustomBlockSurvivesRepublication(t *testing.T) {
	root := t.TempDir()
	lock := publish.NewLock()

	publishOnce(t, root, lock, publish.Options{}, source(map[string]string{"Http/HomeController.go": withBlock}))

	target := filepath.Join(root, "app", "Http", "HomeController.go")
	edited := strings.Replace(read(t, target),
		"// Anything the shape does not say.\n",
		"// A transition only this project has.\n", 1)
	if err := os.WriteFile(target, []byte(edited), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	changes := publishOnce(t, root, lock, publish.Options{}, source(map[string]string{"Http/HomeController.go": withBlockChanged}))
	if len(changes) != 1 || changes[0].Action != publish.Update {
		t.Fatalf("changes = %+v, want one update", changes)
	}

	got := read(t, target)
	if !strings.Contains(got, "A transition only this project has.") {
		t.Errorf("the custom block was lost:\n%s", got)
	}
	if !strings.Contains(got, `return "two"`) {
		t.Errorf("the new file was not published:\n%s", got)
	}
	if strings.Contains(got, "Anything the shape does not say") {
		t.Errorf("the published block replaced the project's:\n%s", got)
	}
}

// TestAFileChangedOutsideItsBlockIsReportedNotReplaced. The lock is what makes
// this possible: "the file is there" cannot tell the file we wrote from the one
// somebody rewrote.
func TestAFileChangedOutsideItsBlockIsReportedNotReplaced(t *testing.T) {
	root := t.TempDir()
	lock := publish.NewLock()
	publishOnce(t, root, lock, publish.Options{}, source(map[string]string{"Http/HomeController.go": withBlock}))

	target := filepath.Join(root, "app", "Http", "HomeController.go")
	edited := strings.Replace(read(t, target), `return "one"`, `return "mine"`, 1)
	if err := os.WriteFile(target, []byte(edited), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	changes := publishOnce(t, root, lock, publish.Options{}, source(map[string]string{"Http/HomeController.go": withBlockChanged}))
	if len(changes) != 1 || changes[0].Action != publish.Conflict {
		t.Fatalf("changes = %+v, want one conflict", changes)
	}
	if read(t, target) != edited {
		t.Error("a conflict was written over")
	}
}

func TestAFileNobodyPublishedIsAConflict(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "app", "Http", "HomeController.go")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(target, []byte("something somebody wrote\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	changes := publishOnce(t, root, publish.NewLock(), publish.Options{},
		source(map[string]string{"Http/HomeController.go": withBlock}))
	if len(changes) != 1 || changes[0].Action != publish.Conflict {
		t.Fatalf("changes = %+v, want one conflict", changes)
	}
	if read(t, target) != "something somebody wrote\n" {
		t.Error("a file this mechanism never wrote was overwritten")
	}
}

// TestForceGivesUpTheEditOutsideTheBlockAndNotTheBlock. Force is the reason the
// merge runs before the decision rather than after it.
func TestForceGivesUpTheEditOutsideTheBlockAndNotTheBlock(t *testing.T) {
	root := t.TempDir()
	lock := publish.NewLock()
	publishOnce(t, root, lock, publish.Options{}, source(map[string]string{"Http/HomeController.go": withBlock}))

	target := filepath.Join(root, "app", "Http", "HomeController.go")
	edited := read(t, target)
	edited = strings.Replace(edited, `return "one"`, `return "mine"`, 1)
	edited = strings.Replace(edited, "// Anything the shape does not say.\n", "// Mine.\n", 1)
	if err := os.WriteFile(target, []byte(edited), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	changes := publishOnce(t, root, lock, publish.Options{Force: true},
		source(map[string]string{"Http/HomeController.go": withBlockChanged}))
	if len(changes) != 1 || changes[0].Action != publish.Update {
		t.Fatalf("changes = %+v, want one update", changes)
	}

	got := read(t, target)
	if !strings.Contains(got, "// Mine.") {
		t.Errorf("force ate the custom block:\n%s", got)
	}
	if strings.Contains(got, `return "mine"`) {
		t.Errorf("force kept the edit it cannot carry forward:\n%s", got)
	}
}

// TestAViewCarriesItsMarkersInItsOwnSyntax. A Go comment below the package
// clause of a .kyse.go is text on the page.
func TestAViewCarriesItsMarkersInItsOwnSyntax(t *testing.T) {
	const first = "package views\n\n<h1>one</h1>\n{{-- arandu:begin custom --}}\n<p>shipped</p>\n{{-- arandu:end custom --}}\n"
	const second = "package views\n\n<h1>two</h1>\n{{-- arandu:begin custom --}}\n<p>shipped</p>\n{{-- arandu:end custom --}}\n"

	root := t.TempDir()
	lock := publish.NewLock()
	view := func(content string) publish.Source {
		return publish.Source{Files: tree(map[string]string{"views/page.kyse.go": content}), To: "resources", Origin: "example:view"}
	}
	publishOnce(t, root, lock, publish.Options{}, view(first))

	target := filepath.Join(root, "resources", "views", "page.kyse.go")
	edited := strings.Replace(read(t, target), "<p>shipped</p>", "<p>ours</p>", 1)
	if err := os.WriteFile(target, []byte(edited), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	publishOnce(t, root, lock, publish.Options{}, view(second))

	got := read(t, target)
	if !strings.Contains(got, "<p>ours</p>") || !strings.Contains(got, "<h1>two</h1>") {
		t.Errorf("the view block did not survive:\n%s", got)
	}
}

func TestTwoSourcesCannotPublishTheSamePath(t *testing.T) {
	root := t.TempDir()
	one := publish.Source{Files: tree(map[string]string{"Http/HomeController.go": withBlock}), To: "resources", Origin: "one"}
	two := publish.Source{Files: tree(map[string]string{"Http/HomeController.go": withBlock}), To: "resources", Origin: "two"}

	_, err := publish.Plan(root, publish.NewLock(), publish.Options{}, one, two)
	if err == nil {
		t.Fatal("two modules published the same path and nothing said so")
	}
	if !strings.Contains(err.Error(), "one") || !strings.Contains(err.Error(), "two") {
		t.Errorf("error = %v, want both origins named", err)
	}
}

// TestAPublicationCannotLeaveTheProject. A module is code somebody else wrote,
// and "../.." in its destination is the difference between publishing into a
// project and writing anywhere the process can reach.
func TestAPublicationCannotLeaveTheProject(t *testing.T) {
	root := t.TempDir()
	for _, to := range []string{"..", "../elsewhere", "resources/../../elsewhere"} {
		t.Run(to, func(t *testing.T) {
			_, err := publish.Plan(root, publish.NewLock(), publish.Options{}, publish.Source{
				Files: tree(map[string]string{"a.txt": "a"}),
				To:    to,
			})
			if err == nil {
				t.Fatalf("%q was accepted", to)
			}
		})
	}
}

func TestASourceWithNoFilesIsRefused(t *testing.T) {
	if _, err := publish.Plan(t.TempDir(), publish.NewLock(), publish.Options{}, publish.Source{Origin: "empty"}); err == nil {
		t.Fatal("a source with no tree was accepted")
	}
}

func TestApplyingWithoutALockIsRefused(t *testing.T) {
	// Applying without one writes files nothing remembers writing, and the next
	// publication reports every one of them as somebody else's.
	if _, err := publish.Apply(t.TempDir(), nil, nil); err == nil {
		t.Fatal("applying without a lock was accepted")
	}
}

func TestTheLockRoundTrips(t *testing.T) {
	root := t.TempDir()
	lock := publish.NewLock()
	publishOnce(t, root, lock, publish.Options{}, source(map[string]string{
		"Http/HomeController.go": withBlock,
		"Http/about.txt":         "about",
	}))

	path := filepath.Join(root, "storage", "publish.lock")
	if err := lock.Write(path); err != nil {
		t.Fatalf("Write: %v", err)
	}

	reread, err := publish.ReadLock(path)
	if err != nil {
		t.Fatalf("ReadLock: %v", err)
	}
	if len(reread.Files) != len(lock.Files) {
		t.Fatalf("reread %v, wrote %v", reread.Paths(), lock.Paths())
	}
	for path, entry := range lock.Files {
		if reread.Files[path] != entry {
			t.Errorf("%s = %+v, wrote %+v", path, reread.Files[path], entry)
		}
	}

	// A lock read from a project that has never published is empty rather than
	// an error: the first run has nothing to have written.
	empty, err := publish.ReadLock(filepath.Join(root, "storage", "nothing.lock"))
	if err != nil || len(empty.Files) != 0 {
		t.Errorf("ReadLock of a missing file = %v, %v", empty, err)
	}
}

func TestAConflictCarriesBothSidesForAPreview(t *testing.T) {
	root := t.TempDir()
	lock := publish.NewLock()
	publishOnce(t, root, lock, publish.Options{}, source(map[string]string{"Http/HomeController.go": withBlock}))

	target := filepath.Join(root, "app", "Http", "HomeController.go")
	edited := strings.Replace(read(t, target), `return "one"`, `return "mine"`, 1)
	if err := os.WriteFile(target, []byte(edited), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	changes, err := publish.Plan(root, lock, publish.Options{}, source(map[string]string{"Http/HomeController.go": withBlockChanged}))
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if got := string(changes[0].Existing()); got != edited {
		t.Error("the preview does not carry what is on disk")
	}
	if !strings.Contains(string(changes[0].Content()), `return "two"`) {
		t.Error("the preview does not carry what would replace it")
	}
}

func TestActionNames(t *testing.T) {
	for action, want := range map[publish.Action]string{
		publish.Create:    "create",
		publish.Update:    "update",
		publish.Unchanged: "unchanged",
		publish.Conflict:  "conflict",
	} {
		if got := action.String(); got != want {
			t.Errorf("String() = %q, want %q", got, want)
		}
	}
}
