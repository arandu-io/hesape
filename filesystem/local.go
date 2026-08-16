package filesystem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// LocalFilesystemAdapter stores files in a directory.
//
// It is the [Adapter] that needs nothing installed, which makes it the right
// one for development and for a single machine. For anything with more than one
// replica, github.com/arandu-io/hesape/filesystem/s3 is the same contract over
// the S3 protocol -- and Cloudflare R2 is the default there.
//
// Like every Adapter it is handed stored paths and never a Grant, so the tenant
// prefix is not something this file could forget: it was applied by [Key]
// before the call.
type LocalFilesystemAdapter struct {
	root string
	// diskName and serveSigned are what lets a link be turned back into a disk.
	// See [LocalFilesystemAdapter.DiskName]
	// and [LocalFilesystemAdapter.ShouldServeSignedUrls]; both are set at wiring
	// time and read afterwards.
	diskName    string
	serveSigned bool
}

// NewLocalFilesystemAdapter returns an adapter rooted at a directory.
//
// The root is created if it does not exist, because the alternative is an
// application that boots fine and fails on the first upload.
func NewLocalFilesystemAdapter(root string) (*LocalFilesystemAdapter, error) {
	if root == "" {
		return nil, errors.New("filesystem: the local adapter needs a root directory")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("filesystem: resolving %s: %w", root, err)
	}
	if err := os.MkdirAll(absolute, 0o755); err != nil {
		return nil, fmt.Errorf("filesystem: creating %s: %w", absolute, err)
	}
	return &LocalFilesystemAdapter{root: absolute}, nil
}

var _ Adapter = (*LocalFilesystemAdapter)(nil)

// Root returns the directory this adapter writes under.
//
// It is here for an error message and for a test that wants to look at what
// landed on disk. It is not a way to build a path: what goes under the root is
// decided by [Key], and this returns the part of the answer that carries no
// tenant.
func (a *LocalFilesystemAdapter) Root() string { return a.root }

// partialPrefix names an upload in flight.
//
// It is a prefix rather than a suffix so List can skip it by looking at the base
// name only, and it starts with a dot so it sorts away from real keys. A stored
// path can never begin with it: [CleanKey] resolves the key, and this prefix is
// only ever produced here.
const partialPrefix = ".hesape-partial-"

// Put writes a file, creating whatever directories it needs.
//
// The content type is not stored: on disk it is the extension, and keeping a
// sidecar file per object to hold one string is a second thing to keep in sync.
// Get infers it, which is what a static file server does anyway.
func (a *LocalFilesystemAdapter) Put(_ context.Context, storedPath string, body io.Reader, _ string) error {
	full, err := a.file(storedPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fmt.Errorf("filesystem: creating the directory for %s: %w", storedPath, err)
	}

	// Written to a temporary name and renamed, so a reader never sees a
	// half-written file and a crash mid-upload leaves nothing to clean up.
	//
	// The name is unique per call. It used to be the path plus ".partial", which
	// is the same name for every concurrent upload of the same key: two requests
	// opened it with O_TRUNC, interleaved their bytes into one file, and both
	// renamed it into place. The stored object was neither upload. Two people
	// replacing the same attachment is not a rare race -- it is a retry. Found
	// by audit.
	f, err := os.CreateTemp(filepath.Dir(full), partialPrefix+"*")
	if err != nil {
		return fmt.Errorf("filesystem: writing %s: %w", storedPath, err)
	}
	tmp := f.Name()
	// Nobody reads a partial file, but CreateTemp makes it 0600 and the stored
	// object is 0644 like every other.
	if err := f.Chmod(0o644); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("filesystem: writing %s: %w", storedPath, err)
	}
	if _, err := io.Copy(f, body); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("filesystem: writing %s: %w", storedPath, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("filesystem: writing %s: %w", storedPath, err)
	}
	if err := os.Rename(tmp, full); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("filesystem: writing %s: %w", storedPath, err)
	}
	return nil
}

// Get reads a file back. The caller closes File.Body.
func (a *LocalFilesystemAdapter) Get(ctx context.Context, storedPath string) (File, error) {
	info, err := a.Stat(ctx, storedPath)
	if err != nil {
		return File{}, err
	}
	full, err := a.file(storedPath)
	if err != nil {
		return File{}, err
	}
	f, err := os.Open(full)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return File{}, ErrNotFound
		}
		return File{}, fmt.Errorf("filesystem: reading %s: %w", storedPath, err)
	}
	return File{Info: info, Body: f}, nil
}

// Stat answers what Get would carry, without opening the file.
func (a *LocalFilesystemAdapter) Stat(_ context.Context, storedPath string) (Info, error) {
	full, err := a.file(storedPath)
	if err != nil {
		return Info{}, err
	}
	info, err := os.Stat(full)
	if errors.Is(err, fs.ErrNotExist) {
		return Info{}, ErrNotFound
	}
	if err != nil {
		return Info{}, fmt.Errorf("filesystem: reading %s: %w", storedPath, err)
	}
	// A directory is not a file, and answering with one would let a caller
	// download the name of a folder.
	if info.IsDir() {
		return Info{}, ErrNotFound
	}
	return Info{
		Key:         storedPath,
		Size:        info.Size(),
		ContentType: TypeOf(storedPath),
		ModifiedAt:  info.ModTime(),
	}, nil
}

// Exists reports whether the path is a file that is there.
func (a *LocalFilesystemAdapter) Exists(_ context.Context, storedPath string) (bool, error) {
	full, err := a.file(storedPath)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(full)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("filesystem: checking %s: %w", storedPath, err)
	}
	return !info.IsDir(), nil
}

// Delete removes a file. Removing what is not there is not an error: the caller
// wanted it gone, and it is.
func (a *LocalFilesystemAdapter) Delete(_ context.Context, storedPath string) error {
	full, err := a.file(storedPath)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("filesystem: deleting %s: %w", storedPath, err)
	}
	return nil
}

// List returns the stored paths under a prefix.
//
// The prefix is a string match and not a directory: "invoices/" and "invoices"
// select different sets, and [Disk] is what decides which one a caller meant.
// The walk starts at the deepest directory the prefix names, so listing one
// tenant does not read another tenant's directory entries.
func (a *LocalFilesystemAdapter) List(_ context.Context, prefix string) ([]string, error) {
	// The directory the walk starts in, resolved through the same containment
	// check every other operation goes through rather than joined by hand.
	// Joining by hand was the one path into this package that never verified the
	// result stayed under the root -- so List was the operation that would have
	// walked out of it. Found by audit.
	dir := prefix
	if !strings.HasSuffix(dir, "/") {
		if i := strings.LastIndex(dir, "/"); i >= 0 {
			dir = dir[:i+1]
		} else {
			dir = ""
		}
	}
	start, err := a.resolve(dir)
	if err != nil {
		return nil, err
	}

	var out []string
	err = filepath.WalkDir(start, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				// A prefix nothing was ever written under has no directory, and
				// an empty list is the right answer rather than an error.
				return filepath.SkipAll
			}
			return err
		}
		// An upload in flight is not a stored object yet, and the name is
		// matched on the base rather than the whole path: a directory named
		// after the prefix would otherwise hide everything under it.
		if entry.IsDir() || strings.HasPrefix(entry.Name(), partialPrefix) {
			return nil
		}
		relative, err := filepath.Rel(a.root, p)
		if err != nil {
			return err
		}
		stored := filepath.ToSlash(relative)
		if strings.HasPrefix(stored, prefix) {
			out = append(out, stored)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("filesystem: listing %s: %w", prefix, err)
	}
	return out, nil
}

// file resolves a stored path to a file on disk, and refuses one that escapes.
//
// The escape check happens twice: [CleanKey] rejects "../" in the key, and this
// rejects a resolved path outside the root. Two checks because the first is
// about the key and the second is about the filesystem -- an Adapter is a public
// interface, so this one is also what stands between a hand-built path and the
// rest of the disk.
func (a *LocalFilesystemAdapter) file(storedPath string) (string, error) {
	full, err := a.resolve(storedPath)
	if err != nil {
		return "", err
	}
	// The root itself is a directory, and every operation here is about a file.
	if full == a.root {
		return "", ErrBadKey
	}
	return full, nil
}

// resolve joins a stored path onto the root and proves the result stayed under
// it. The root itself is a legal answer, which is what makes an empty prefix
// mean "everything".
func (a *LocalFilesystemAdapter) resolve(storedPath string) (string, error) {
	// A NUL byte truncates a path in every syscall that takes one, so a path
	// carrying it can name a different file than it appears to.
	if strings.ContainsRune(storedPath, 0) {
		return "", ErrBadKey
	}
	full := filepath.Join(a.root, filepath.FromSlash(storedPath))
	if full != a.root && !strings.HasPrefix(full, a.root+string(filepath.Separator)) {
		return "", ErrBadKey
	}
	return full, nil
}
