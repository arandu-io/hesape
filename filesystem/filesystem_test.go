package filesystem_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/arandu-io/hesape/filesystem"
)

func write(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("preparing %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

func TestGetAndPutRoundTrip(t *testing.T) {
	f := filesystem.NewFilesystem()
	path := filepath.Join(t.TempDir(), "note.txt")

	if err := f.Put(path, []byte("first"), false); err != nil {
		t.Fatalf("put: %v", err)
	}
	body, err := f.Get(path, false)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(body) != "first" {
		t.Fatalf("got %q, want %q", body, "first")
	}
	if !f.Exists(path) || f.Missing(path) {
		t.Fatal("the file exists and the two answers disagree")
	}
}

func TestGetSaysNotFoundRatherThanTheOperatingSystemsWords(t *testing.T) {
	f := filesystem.NewFilesystem()

	_, err := f.Get(filepath.Join(t.TempDir(), "absent"), false)
	if !errors.Is(err, filesystem.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestPutAndGetUnderALockSeeTheSameBytes(t *testing.T) {
	f := filesystem.NewFilesystem()
	path := filepath.Join(t.TempDir(), "locked.txt")

	if err := f.Put(path, []byte("under a lock"), true); err != nil {
		t.Fatalf("put: %v", err)
	}
	body, err := f.Get(path, true)
	if err != nil {
		t.Fatalf("shared get: %v", err)
	}
	if string(body) != "under a lock" {
		t.Fatalf("got %q", body)
	}
}

func TestPutUnderALockTruncatesWhatWasThere(t *testing.T) {
	f := filesystem.NewFilesystem()
	path := write(t, filepath.Join(t.TempDir(), "old.txt"), "a much longer previous value")

	if err := f.Put(path, []byte("short"), true); err != nil {
		t.Fatalf("put: %v", err)
	}
	body, err := f.Get(path, false)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// Without the truncate, the tail of the previous value survives past the
	// new one and the file reads as "shortr previous value".
	if string(body) != "short" {
		t.Fatalf("got %q, want %q", body, "short")
	}
}

func TestConcurrentLockedWritesDoNotInterleave(t *testing.T) {
	f := filesystem.NewFilesystem()
	path := filepath.Join(t.TempDir(), "contended.txt")

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body := strings.Repeat(string(rune('a'+i)), 64)
			if err := f.Put(path, []byte(body), true); err != nil {
				t.Errorf("put: %v", err)
			}
		}()
	}
	wg.Wait()

	body, err := f.Get(path, true)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(body) != 64 {
		t.Fatalf("the file holds %d bytes, so two writers interleaved: %q", len(body), body)
	}
	if strings.Trim(string(body), string(body[0])) != "" {
		t.Fatalf("the file holds more than one writer's bytes: %q", body)
	}
}

func TestAnExclusiveLockRefusesASecondOneWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "held.txt")

	held, err := filesystem.NewLockableFile(path, os.O_CREATE|os.O_RDWR)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer held.Close()
	if err := held.GetExclusiveLock(true); err != nil {
		t.Fatalf("lock: %v", err)
	}

	second, err := filesystem.NewLockableFile(path, os.O_CREATE|os.O_RDWR)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer second.Close()

	err = second.GetExclusiveLock(false)
	if !errors.Is(err, filesystem.ErrLockTimeout) {
		t.Fatalf("got %v, want ErrLockTimeout", err)
	}

	if err := held.ReleaseLock(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := second.GetExclusiveLock(false); err != nil {
		t.Fatalf("the lock was released and the second one still cannot take it: %v", err)
	}
}

func TestReplaceIsAtomicAndKeepsTheModeThatWasThere(t *testing.T) {
	f := filesystem.NewFilesystem()
	path := write(t, filepath.Join(t.TempDir(), "config.json"), "{}")
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	if err := f.Replace(path, []byte(`{"a":1}`), 0); err != nil {
		t.Fatalf("replace: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("the mode became %v, so the replacement handed the file the temporary's permissions", info.Mode().Perm())
	}
	body, err := f.Get(path, false)
	if err != nil || string(body) != `{"a":1}` {
		t.Fatalf("got %q, %v", body, err)
	}
}

func TestJsonReadsAnObject(t *testing.T) {
	f := filesystem.NewFilesystem()
	path := write(t, filepath.Join(t.TempDir(), "m.json"), `{"name":"arandu","count":2}`)

	out, err := f.Json(path, false)
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	if out["name"] != "arandu" {
		t.Fatalf("got %v", out)
	}
}

func TestLinesDropsTheTrailingNewlineAndNothingElse(t *testing.T) {
	f := filesystem.NewFilesystem()
	path := write(t, filepath.Join(t.TempDir(), "l.txt"), "one\ntwo\n\nthree\n")

	lines, err := f.Lines(path)
	if err != nil {
		t.Fatalf("lines: %v", err)
	}
	want := []string{"one", "two", "", "three"}
	if len(lines) != len(want) {
		t.Fatalf("got %q, want %q", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("got %q, want %q", lines, want)
		}
	}
}

func TestHashAndHasSameHash(t *testing.T) {
	f := filesystem.NewFilesystem()
	dir := t.TempDir()
	first := write(t, filepath.Join(dir, "a.txt"), "same")
	second := write(t, filepath.Join(dir, "b.txt"), "same")
	third := write(t, filepath.Join(dir, "c.txt"), "different")

	if !f.HasSameHash(first, second) {
		t.Fatal("two files with the same bytes hash differently")
	}
	if f.HasSameHash(first, third) {
		t.Fatal("two files with different bytes hash the same")
	}
	sum, err := f.Hash(first, "sha256")
	if err != nil || len(sum) != 64 {
		t.Fatalf("sha256 gave %q, %v", sum, err)
	}
	if _, err := f.Hash(first, "crc32"); err == nil {
		t.Fatal("an algorithm this does not know was accepted")
	}
}

func TestReplaceInFileAndAppendAndPrepend(t *testing.T) {
	f := filesystem.NewFilesystem()
	path := write(t, filepath.Join(t.TempDir(), "s.txt"), "hello NAME")

	if err := f.ReplaceInFile("NAME", "world", path); err != nil {
		t.Fatalf("replace in file: %v", err)
	}
	if err := f.Append(path, []byte("!"), true); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := f.Prepend(path, []byte(">> ")); err != nil {
		t.Fatalf("prepend: %v", err)
	}
	body, _ := f.Get(path, false)
	if string(body) != ">> hello world!" {
		t.Fatalf("got %q", body)
	}
}

func TestNameBasenameDirnameAndExtension(t *testing.T) {
	f := filesystem.NewFilesystem()

	if got := f.Name("/tmp/report.q1.pdf"); got != "report.q1" {
		t.Fatalf("name: %q", got)
	}
	if got := f.Basename("/tmp/report.pdf"); got != "report.pdf" {
		t.Fatalf("basename: %q", got)
	}
	if got := f.Dirname("/tmp/x/report.pdf"); got != "/tmp/x" {
		t.Fatalf("dirname: %q", got)
	}
	// Illuminate answers "pdf" and not ".pdf", which is what a caller comparing
	// against a configured list has written down.
	if got := f.Extension("/tmp/report.pdf"); got != "pdf" {
		t.Fatalf("extension: %q", got)
	}
}

func TestTypeAndMimeTypeAndSize(t *testing.T) {
	f := filesystem.NewFilesystem()
	dir := t.TempDir()
	path := write(t, filepath.Join(dir, "page.html"), "<p>hi</p>")

	kind, err := f.Type(path)
	if err != nil || kind != "file" {
		t.Fatalf("type: %q, %v", kind, err)
	}
	kind, err = f.Type(dir)
	if err != nil || kind != "dir" {
		t.Fatalf("type of a directory: %q, %v", kind, err)
	}
	mime, err := f.MimeType(path)
	if err != nil || !strings.HasPrefix(mime, "text/html") {
		t.Fatalf("mime: %q, %v", mime, err)
	}
	size, err := f.Size(path)
	if err != nil || size != 9 {
		t.Fatalf("size: %d, %v", size, err)
	}
	if _, err := f.LastModified(path); err != nil {
		t.Fatalf("last modified: %v", err)
	}
	if _, err := f.MimeType(filepath.Join(dir, "absent")); !errors.Is(err, filesystem.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestListingRespectsDepthAndHiddenFiles(t *testing.T) {
	f := filesystem.NewFilesystem()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "top.txt"), "x")
	write(t, filepath.Join(dir, ".hidden"), "x")
	write(t, filepath.Join(dir, "sub", "deep.txt"), "x")
	write(t, filepath.Join(dir, "sub", "deeper", "deepest.txt"), "x")

	shallow, err := f.Files(dir, false, 0)
	if err != nil {
		t.Fatalf("files: %v", err)
	}
	if len(shallow) != 1 || filepath.Base(shallow[0]) != "top.txt" {
		t.Fatalf("depth 0 gave %v", shallow)
	}

	withHidden, err := f.Files(dir, true, 0)
	if err != nil || len(withHidden) != 2 {
		t.Fatalf("hidden gave %v, %v", withHidden, err)
	}

	all, err := f.AllFiles(dir, false)
	if err != nil || len(all) != 3 {
		t.Fatalf("all files gave %v, %v", all, err)
	}

	dirs, err := f.Directories(dir, 0)
	if err != nil || len(dirs) != 1 {
		t.Fatalf("directories gave %v, %v", dirs, err)
	}
	allDirs, err := f.AllDirectories(dir)
	if err != nil || len(allDirs) != 2 {
		t.Fatalf("all directories gave %v, %v", allDirs, err)
	}
}

func TestDirectoryLifecycle(t *testing.T) {
	f := filesystem.NewFilesystem()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	write(t, filepath.Join(source, "a.txt"), "a")
	write(t, filepath.Join(source, "nested", "b.txt"), "b")

	target := filepath.Join(root, "target")
	if err := f.CopyDirectory(source, target); err != nil {
		t.Fatalf("copy directory: %v", err)
	}
	body, err := f.Get(filepath.Join(target, "nested", "b.txt"), false)
	if err != nil || string(body) != "b" {
		t.Fatalf("the nested file did not come across: %q, %v", body, err)
	}

	if err := f.CleanDirectory(target); err != nil {
		t.Fatalf("clean: %v", err)
	}
	if !f.IsEmptyDirectory(target, false) {
		t.Fatal("the directory was cleaned and is not empty")
	}
	if !f.IsDirectory(target) {
		t.Fatal("cleaning removed the directory itself")
	}

	if err := f.DeleteDirectory(target, false); err != nil {
		t.Fatalf("delete directory: %v", err)
	}
	if f.IsDirectory(target) {
		t.Fatal("the directory is still there")
	}
}

func TestMakeDirectoryBeatsTheUmask(t *testing.T) {
	f := filesystem.NewFilesystem()
	path := filepath.Join(t.TempDir(), "made")

	if err := f.MakeDirectory(path, 0o755, true, false); err != nil {
		t.Fatalf("make directory: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("the directory arrived as %v, so the umask took bits off the mode that was asked for", info.Mode().Perm())
	}
	// Asking again is not an error: EnsureDirectoryExists is the idempotent one.
	if err := f.EnsureDirectoryExists(path, 0o755, true); err != nil {
		t.Fatalf("ensure: %v", err)
	}
}

func TestMoveDirectoryRefusesToMergeUnlessToldTo(t *testing.T) {
	f := filesystem.NewFilesystem()
	root := t.TempDir()
	from := filepath.Join(root, "from")
	to := filepath.Join(root, "to")
	write(t, filepath.Join(from, "a.txt"), "a")
	write(t, filepath.Join(to, "old.txt"), "old")

	if err := f.MoveDirectory(from, to, false); err == nil {
		t.Fatal("a destination that was already there was merged into silently")
	}

	write(t, filepath.Join(from, "a.txt"), "a")
	if err := f.MoveDirectory(from, to, true); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if f.Exists(filepath.Join(to, "old.txt")) {
		t.Fatal("the overwrite left the previous contents behind")
	}
}

func TestDeleteDirectoriesLeavesTheFiles(t *testing.T) {
	f := filesystem.NewFilesystem()
	root := t.TempDir()
	write(t, filepath.Join(root, "keep.txt"), "keep")
	write(t, filepath.Join(root, "gone", "x.txt"), "x")

	deleted, err := f.DeleteDirectories(root)
	if err != nil || !deleted {
		t.Fatalf("delete directories: %v, %v", deleted, err)
	}
	if !f.Exists(filepath.Join(root, "keep.txt")) {
		t.Fatal("the file was removed too")
	}
	if f.IsDirectory(filepath.Join(root, "gone")) {
		t.Fatal("the directory is still there")
	}
}

func TestLinkAndRelativeLink(t *testing.T) {
	f := filesystem.NewFilesystem()
	root := t.TempDir()
	target := write(t, filepath.Join(root, "store", "file.txt"), "linked")

	link := filepath.Join(root, "public", "file.txt")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := f.Link(target, link); err != nil {
		t.Fatalf("link: %v", err)
	}
	body, err := f.Get(link, false)
	if err != nil || string(body) != "linked" {
		t.Fatalf("through the link: %q, %v", body, err)
	}

	relative := filepath.Join(root, "public", "relative.txt")
	if err := f.RelativeLink(target, relative); err != nil {
		t.Fatalf("relative link: %v", err)
	}
	// The point of a relative link is that the target is written relative to the
	// directory the link is in, so moving the tree does not break it.
	got, err := os.Readlink(relative)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if filepath.IsAbs(got) {
		t.Fatalf("the relative link points at %q, which is absolute", got)
	}
}

func TestChmodReadsWithZeroAndSetsWithAnything(t *testing.T) {
	f := filesystem.NewFilesystem()
	path := write(t, filepath.Join(t.TempDir(), "m.txt"), "m")

	if _, err := f.Chmod(path, 0o600); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	mode, err := f.Chmod(path, 0)
	if err != nil {
		t.Fatalf("read chmod: %v", err)
	}
	if mode != 0o600 {
		t.Fatalf("got %v, want 0600", mode)
	}
}

func TestGlobAndPredicates(t *testing.T) {
	f := filesystem.NewFilesystem()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.md"), "a")
	write(t, filepath.Join(dir, "b.md"), "b")
	write(t, filepath.Join(dir, "c.txt"), "c")

	matches, err := f.Glob(filepath.Join(dir, "*.md"))
	if err != nil || len(matches) != 2 {
		t.Fatalf("glob: %v, %v", matches, err)
	}
	if !f.IsFile(matches[0]) || f.IsDirectory(matches[0]) {
		t.Fatal("a file reads as a directory")
	}
	if !f.IsReadable(matches[0]) || !f.IsWritable(matches[0]) {
		t.Fatal("a file just written is not readable or not writable")
	}
	if !f.IsWritable(dir) {
		t.Fatal("a temporary directory is not writable")
	}
}

func TestDeleteTakesSeveralAndForgivesTheAbsent(t *testing.T) {
	f := filesystem.NewFilesystem()
	dir := t.TempDir()
	first := write(t, filepath.Join(dir, "1"), "1")
	second := write(t, filepath.Join(dir, "2"), "2")

	if err := f.Delete(first, second, filepath.Join(dir, "never")); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if f.Exists(first) || f.Exists(second) {
		t.Fatal("the files are still there")
	}
}

func TestMoveAndCopy(t *testing.T) {
	f := filesystem.NewFilesystem()
	dir := t.TempDir()
	src := write(t, filepath.Join(dir, "src.txt"), "body")

	copied := filepath.Join(dir, "copy.txt")
	if err := f.Copy(src, copied); err != nil {
		t.Fatalf("copy: %v", err)
	}
	moved := filepath.Join(dir, "moved.txt")
	if err := f.Move(copied, moved); err != nil {
		t.Fatalf("move: %v", err)
	}
	if f.Exists(copied) || !f.Exists(moved) || !f.Exists(src) {
		t.Fatal("move did not move, or copy did not leave the source alone")
	}
}

func TestJoinPathsDropsTheEmptySegments(t *testing.T) {
	// base//views and base/views name the same file and are two different
	// strings, which is two cache keys.
	if got := filesystem.JoinPaths("base", "", "views"); got != filepath.Join("base", "views") {
		t.Fatalf("got %q", got)
	}
	if got := filesystem.JoinPaths("", "views"); got != "views" {
		t.Fatalf("got %q", got)
	}
}

func TestGuessExtension(t *testing.T) {
	f := filesystem.NewFilesystem()
	path := write(t, filepath.Join(t.TempDir(), "page.html"), "<p></p>")

	ext, err := f.GuessExtension(path)
	if err != nil {
		t.Fatalf("guess: %v", err)
	}
	if ext == "" || strings.HasPrefix(ext, ".") {
		t.Fatalf("got %q, want an extension without the dot", ext)
	}
}
