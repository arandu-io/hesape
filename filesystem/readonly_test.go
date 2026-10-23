package filesystem_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/filesystem"
)

type allowAll struct{}

func (allowAll) Can(context.Context, auth.Subject, auth.Action, struct{}) error { return nil }

// viewingGrant is a Grant issued to a customer an operator is only viewing as.
func viewingGrant(t *testing.T, tenant string) auth.Grant {
	t.Helper()
	viewed, err := auth.Impersonate(
		auth.Subject{ID: "op-1", Tenant: tenant, Roles: []string{"admin"}},
		auth.Subject{ID: "user-9", Tenant: tenant})
	if err != nil {
		t.Fatal(err)
	}
	g, err := auth.Authorize(context.Background(), allowAll{}, viewed, "file.manage", struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// TestAViewingGrantChangesNoFile: the read-only mode of an impersonation held
// only in the database, so an operator viewing a customer's account could
// still write, overwrite and delete the customer's files. Every Disk method
// that writes refuses, and the file is as it was.
func TestAViewingGrantChangesNoFile(t *testing.T) {
	assertNoFileChanged(t, viewingGrant(t, "acme"), auth.ErrReadOnly)
}

// readsOnly allows everything and says none of its actions write.
type readsOnly struct{ allowAll }

func (readsOnly) Writes(auth.Action) bool { return false }

// TestAReadGrantChangesNoFile: a Grant a policy issued for an action it says
// does not write stores, moves and deletes nothing on a Disk, and reads.
func TestAReadGrantChangesNoFile(t *testing.T) {
	g, err := auth.Authorize(context.Background(), readsOnly{}, auth.Subject{ID: "bea", Tenant: "acme"}, "file.view", struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	assertNoFileChanged(t, g, auth.ErrReadGrant)
}

// assertNoFileChanged runs every Disk write with g on tenant acme, wants each
// refused with want, and wants g to list and read the file left as it was.
func assertNoFileChanged(t *testing.T, g auth.Grant, want error) {
	t.Helper()
	ctx := context.Background()
	const tenant = "acme"
	disk := filesystem.NewDisk("local", mustLocal(t))
	owner := auth.SystemGrant("file.manage", tenant)
	if err := disk.Put(ctx, owner, "a.txt", strings.NewReader("original"), ""); err != nil {
		t.Fatal(err)
	}
	asked := false
	disk.BuildTemporaryUploadURLsUsing(func(context.Context, auth.Grant, string, time.Duration) (string, http.Header, error) {
		asked = true
		return "https://store.example/upload", nil, nil
	})

	writes := map[string]func() error{
		"Put":           func() error { return disk.Put(ctx, g, "a.txt", strings.NewReader("changed"), "") },
		"WriteStream":   func() error { return disk.WriteStream(ctx, g, "b.txt", strings.NewReader("new"), "") },
		"Append":        func() error { return disk.Append(ctx, g, "a.txt", "more") },
		"Prepend":       func() error { return disk.Prepend(ctx, g, "a.txt", "more") },
		"Copy":          func() error { return disk.Copy(ctx, g, "a.txt", "c.txt") },
		"Move":          func() error { return disk.Move(ctx, g, "a.txt", "d.txt") },
		"Delete":        func() error { return disk.Delete(ctx, g, "a.txt") },
		"DeleteDir":     func() error { return disk.DeleteDirectory(ctx, g, "") },
		"MakeDirectory": func() error { return disk.MakeDirectory(ctx, g, "reports") },
		"SetVisibility": func() error { return disk.SetVisibility(ctx, g, "a.txt", filesystem.VisibilityPublic) },
		"PutFileAs": func() error {
			_, err := disk.PutFileAs(ctx, g, "", filesystem.Upload{Open: func() (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("new")), nil
			}}, "e.txt")
			return err
		},
		"TemporaryUploadURL": func() error {
			_, _, err := disk.TemporaryUploadURL(ctx, g, "f.txt", time.Minute)
			return err
		},
	}
	for name, write := range writes {
		if err := write(); !errors.Is(err, want) {
			t.Errorf("%s answered %v, want %v", name, err, want)
		}
	}
	if asked {
		t.Error("the upload URL callback was asked for a Grant that cannot write")
	}

	files, err := disk.AllFiles(ctx, g, "")
	if err != nil {
		t.Fatalf("the Grant cannot list: %v", err)
	}
	if len(files) != 1 || files[0] != "a.txt" {
		t.Fatalf("the tenant holds %v after the refused writes, want only a.txt", files)
	}
	f, err := disk.Get(ctx, g, "a.txt")
	if err != nil {
		t.Fatalf("the Grant cannot read: %v", err)
	}
	defer f.Body.Close()
	body, _ := io.ReadAll(f.Body)
	if string(body) != "original" {
		t.Fatalf("the file holds %q after the refused writes", body)
	}
}

func mustLocal(t *testing.T) *filesystem.LocalFilesystemAdapter {
	t.Helper()
	a, err := filesystem.NewLocalFilesystemAdapter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return a
}
