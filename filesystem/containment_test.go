package filesystem_test

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/filesystem"
)

// TestTheContainmentProbeMatrix runs one key of each shape that has ever been
// used to leave a storage root, through the Disk an application holds, and
// reads the directory afterwards.
//
// The matrix is the point. Each of these is refused somewhere, and each is
// refused by a different line: a relative traversal by the path resolution, an
// absolute path by the leading slash, a percent-encoded traversal by nothing at
// all -- it is not a traversal until something decodes it, and nothing here
// does. Written one test per shape, the one nobody thought of is the one with
// no test, and the answer for it is unrecorded rather than known.
//
// A case that is not refused is not thereby a finding: it has to land inside
// the tenant's own prefix, and the sweep at the end is what says it did. The
// directory next to the root is read too, because a write that escapes leaves
// the file there and no error anywhere.
func TestTheContainmentProbeMatrix(t *testing.T) {
	for _, probe := range []struct {
		shape   string
		key     string
		refused bool
	}{
		{"a relative traversal", "../escaped.txt", true},
		{"a traversal past a real segment", "invoices/../../escaped.txt", true},
		{"the parent itself", "..", true},
		// One leading slash is trimmed and the rest of the key is kept, so this
		// lands at <tenant>/etc/passwd -- inside the prefix, naming nothing of
		// the machine's. Refusing it would refuse "/avatar.png" from an upload
		// form, which is the same shape and is what people write. Two slashes,
		// or one with a traversal behind it, survive the trim as an absolute
		// path and are refused.
		{"an absolute path", "/etc/passwd", false},
		{"an absolute path written twice", "//etc/passwd", true},
		{"an absolute path with a traversal", "/../etc/passwd", true},
		{"the root itself", "/", true},
		{"the empty key", "", true},
		{"a key naming another tenant's prefix", "../" + otherTenant + "/secret.txt", true},

		// Not a traversal until something decodes it, and nothing on this path
		// does: it is stored under a directory whose name happens to read like
		// one. Recorded as contained rather than refused, because that is what
		// it is -- and because a later change that adds decoding anywhere on
		// this path turns this line red, which is the moment to look.
		{"a percent-encoded traversal", "%2e%2e%2fescaped.txt", false},
		{"a percent-encoded traversal, mixed", "..%2f..%2fescaped.txt", false},

		// A tenant identifier written as an ordinary segment is an ordinary
		// segment: it lands under this tenant's prefix, where it names nothing
		// of the other tenant's.
		{"another tenant's name as a segment", otherTenant + "/secret.txt", false},
	} {
		t.Run(probe.shape, func(t *testing.T) {
			adapter := localAdapter(t)
			disk := filesystem.NewDisk("local", adapter)
			ctx := context.Background()

			err := disk.Put(ctx, grant(tenant), probe.key, strings.NewReader("probe"), "")
			if refused := errors.Is(err, filesystem.ErrBadKey); refused != probe.refused {
				t.Fatalf("Put(%q) refused=%v, want %v (%v)", probe.key, refused, probe.refused, err)
			}

			outside := writtenOutside(t, adapter.Root(), tenant)
			if len(outside) != 0 {
				t.Errorf("Put(%q) put %v where nothing under this tenant's prefix names it", probe.key, outside)
			}

			// A key that was not refused has to have landed somewhere, and the
			// sweep above only says where it did not. This says it did.
			if !probe.refused {
				if stored := storedFiles(t, filepath.Join(adapter.Root(), tenant)); len(stored) != 1 {
					t.Errorf("Put(%q) left %v under the tenant prefix, want exactly one file", probe.key, stored)
				}
			}
		})
	}
}

// writtenOutside returns every file under the root that is not inside the named
// tenant's prefix, and every entry that appeared beside the root.
//
// Reading the disk rather than trusting the error is the whole probe: a write
// that escaped the root reports no error, and the file it left is the only
// evidence there is.
func writtenOutside(t *testing.T, root, tenant string) []string {
	t.Helper()

	var stray []string

	prefix := filepath.Join(root, tenant) + string(filepath.Separator)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || strings.HasPrefix(path, prefix) {
			return nil
		}
		stray = append(stray, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}

	// The root is a directory inside a temporary one, and a path that left it
	// by one segment lands here rather than anywhere a walk of the root sees.
	entries, err := os.ReadDir(filepath.Dir(root))
	if err != nil {
		t.Fatalf("reading beside %s: %v", root, err)
	}
	for _, entry := range entries {
		if entry.Name() != filepath.Base(root) {
			stray = append(stray, filepath.Join(filepath.Dir(root), entry.Name()))
		}
	}

	return stray
}

// TestASymlinkAcrossTheTenantPrefixIsFollowedAndOnlyTheRootIsConfined records
// the exposure the adapter declares, as an executed fact instead of a sentence.
//
// The root is held open and every name resolves against it, so a link out of
// the root stops the operation. A link from one tenant's directory into
// another's never leaves the root, so it is followed, and this reproduces that:
// a link planted in one tenant's directory reads the other's file through a
// Grant that names only the first.
//
// What contains it is not the resolution but the reach: nothing an application
// can ask a Disk to do creates a symbolic link, so planting one means already
// holding the filesystem the disk is on -- and whoever holds that reads both
// directories without a link. The probe plants it with os.Symlink for exactly
// that reason, and the second half fixes that the Disk cannot.
func TestASymlinkAcrossTheTenantPrefixIsFollowedAndOnlyTheRootIsConfined(t *testing.T) {
	adapter := localAdapter(t)
	disk := filesystem.NewDisk("local", adapter)
	ctx := context.Background()

	if err := disk.Put(ctx, grant(otherTenant), "secret.txt", strings.NewReader("the other tenant's bytes"), ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(adapter.Root(), tenant), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	link := filepath.Join(adapter.Root(), tenant, "borrowed.txt")
	// A relative target, because it is the one that stays inside the root as
	// written. An absolute target names a path the open root did not resolve, and
	// is refused whatever it points at -- which is the stricter answer and is
	// fixed below.
	target := filepath.Join("..", otherTenant, "secret.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("this filesystem does not make symbolic links: %v", err)
	}

	file, err := disk.Get(ctx, grant(tenant), "borrowed.txt")
	if err != nil {
		t.Fatalf("a link that stays inside the root is followed, and this one was not: %v", err)
	}
	body, err := io.ReadAll(file.Body)
	file.Body.Close()
	if err != nil {
		t.Fatalf("reading through the link: %v", err)
	}
	if string(body) != "the other tenant's bytes" {
		t.Fatalf("read %q through the link, and the exposure this records is that it reads the target", body)
	}

	// The containment: the key that plants it cannot be written through the
	// disk, so the link above needed a hand outside this API.
	err = disk.Put(ctx, grant(tenant), "../"+otherTenant+"/planted.txt", strings.NewReader("x"), "")
	if !errors.Is(err, filesystem.ErrBadKey) {
		t.Errorf("a key crossing into another tenant was accepted: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(adapter.Root(), otherTenant, "planted.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("something landed in the other tenant's directory: %v", err)
	}

	// And the stricter half: a link whose target is written as an absolute path
	// is refused even when the path it names is inside the root, because the
	// open root resolves names it walked and this one was handed to it whole.
	absolute := filepath.Join(adapter.Root(), tenant, "absolute.txt")
	if err := os.Symlink(filepath.Join(adapter.Root(), otherTenant, "secret.txt"), absolute); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	if f, err := disk.Get(ctx, grant(tenant), "absolute.txt"); err == nil {
		body, _ := io.ReadAll(f.Body)
		f.Body.Close()
		t.Errorf("a link with an absolute target read %q", body)
	}
}

// storedFiles returns every file under dir, which is how a probe says where a
// key that was accepted actually landed.
func storedFiles(t *testing.T, dir string) []string {
	t.Helper()

	var found []string
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			found = append(found, path)
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("walking %s: %v", dir, err)
	}
	return found
}

// TestASignedLinkCarriesTheTenantAllTheWayToTheBytes follows one temporary URL
// from the Grant that issued it to the response it serves.
//
// The link is a bearer token: whoever holds it reaches the file without a
// session, so the tenant has to travel inside it and be applied again at the
// end. The disk here holds a file of the same key for two tenants, which is the
// only arrangement in which a missing prefix is visible -- with one tenant on
// the disk, a link that dropped the prefix would serve the right bytes by
// accident.
func TestASignedLinkCarriesTheTenantAllTheWayToTheBytes(t *testing.T) {
	adapter := localAdapter(t)
	disk := filesystem.NewDisk("local", adapter)
	signer := filesystem.NewURLSigner(signer(t), "/files")
	ctx := context.Background()

	if err := disk.Put(ctx, grant(tenant), "invoice.pdf", strings.NewReader("mine"), ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := disk.Put(ctx, grant(otherTenant), "invoice.pdf", strings.NewReader("theirs"), ""); err != nil {
		t.Fatalf("Put: %v", err)
	}

	link, err := signer.TemporaryURL(ctx, grant(tenant), disk, "invoice.pdf", time.Minute)
	if err != nil {
		t.Fatalf("TemporaryURL: %v", err)
	}

	redeemed, name, key, err := signer.Redeem(strings.TrimPrefix(link, "/files/"))
	if err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	if name != "local" || key != "invoice.pdf" {
		t.Fatalf("the token names disk %q key %q, want local and invoice.pdf", name, key)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/files/whatever", nil)
	if err := filesystem.Serve(w, r, redeemed, disk, key, filesystem.ServeOptions{}); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if got := w.Body.String(); got != "mine" {
		t.Fatalf("the link served %q, and the tenant it was issued for holds \"mine\"", got)
	}
}
