package filesystem_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/encryption"
	"github.com/arandu-io/hesape/filesystem"
)

func signer(t *testing.T) *encryption.Signer {
	t.Helper()
	key, err := encryption.ParseKey(encryption.GenerateKey())
	if err != nil {
		t.Fatal(err)
	}
	return encryption.NewSigner(key)
}

func TestATemporaryURLRedeemsToTheFileItNames(t *testing.T) {
	u := filesystem.NewURLSigner(signer(t), "/files")
	d := filesystem.NewDisk("local", newMem())
	ctx := context.Background()
	g := grant(tenant)
	put(t, d, g, "invoices/q1.pdf", "body")

	link, err := u.TemporaryURL(ctx, g, d, "invoices/q1.pdf", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(link, "/files/") {
		t.Fatalf("link = %q, want it under the base", link)
	}

	redeemed, disk, key, err := u.Redeem(strings.TrimPrefix(link, "/files/"))
	if err != nil {
		t.Fatal(err)
	}
	if key != "invoices/q1.pdf" {
		t.Fatalf("key = %q", key)
	}
	if disk != "local" {
		t.Fatalf("disk = %q, want local -- the route has to know which one", disk)
	}
	if auth.Tenant(redeemed) != tenant {
		t.Fatalf("tenant = %q, want %q", auth.Tenant(redeemed), tenant)
	}
	if got := read(t, d, redeemed, key); got != "body" {
		t.Fatalf("body = %q", got)
	}
}

// TestAKeyWithAnythingInItSurvivesTheRoundTrip: the payload is url.Values and
// not a separator, because a payload that can be read two ways is a payload an
// attacker picks the reading of.
func TestAKeyWithAnythingInItSurvivesTheRoundTrip(t *testing.T) {
	u := filesystem.NewURLSigner(signer(t), "/files")
	d := filesystem.NewDisk("local", newMem())

	for _, key := range []string{
		"a=b&c=d.pdf",
		"a b/c d.pdf",
		"relatório-q1.pdf",
		"a\nb.pdf",
		"k=&t=99999999",
	} {
		link, err := u.TemporaryURL(context.Background(), grant(tenant), d, key, time.Minute)
		if err != nil {
			t.Fatalf("%q: %v", key, err)
		}
		_, _, got, err := u.Redeem(strings.TrimPrefix(link, "/files/"))
		if err != nil {
			t.Fatalf("%q: %v", key, err)
		}
		if got != key {
			t.Fatalf("%q came back as %q", key, got)
		}
	}
}

// TestATamperedLinkIsRefused: the tenant and the key are inside the signature,
// so editing the URL to name another customer's file invalidates it.
func TestATamperedLinkIsRefused(t *testing.T) {
	u := filesystem.NewURLSigner(signer(t), "/files")
	d := filesystem.NewDisk("local", newMem())

	link, err := u.TemporaryURL(context.Background(), grant(tenant), d, "invoice.pdf", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimPrefix(link, "/files/")
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token = %q, want payload.expiry.signature", token)
	}

	for _, bad := range []string{
		parts[0] + "A." + parts[1] + "." + parts[2], // the payload
		parts[0] + "." + "9999999999." + parts[2],   // the expiry
		parts[0] + "." + parts[1] + "." + parts[0],  // the signature
		strings.ReplaceAll(token, ".", "-"),
		"",
		"nonsense",
	} {
		if _, _, _, err := u.Redeem(bad); !errors.Is(err, filesystem.ErrBadURL) {
			t.Errorf("%q was redeemed: %v", bad, err)
		}
	}
}

// TestALinkFromAnotherApplicationIsRefused: two applications, two keys, and a
// link is only good where it was issued.
func TestALinkFromAnotherApplicationIsRefused(t *testing.T) {
	mine := filesystem.NewURLSigner(signer(t), "/files")
	theirs := filesystem.NewURLSigner(signer(t), "/files")
	d := filesystem.NewDisk("local", newMem())

	link, err := theirs.TemporaryURL(context.Background(), grant(tenant), d, "invoice.pdf", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := mine.Redeem(strings.TrimPrefix(link, "/files/")); !errors.Is(err, filesystem.ErrBadURL) {
		t.Fatalf("err = %v, want ErrBadURL", err)
	}
}

// TestAnExpiredLinkIsRefusedAndSaysSo: "ask for another one" is the one thing a
// person can act on, so it stays distinguishable.
func TestAnExpiredLinkIsRefusedAndSaysSo(t *testing.T) {
	u := filesystem.NewURLSigner(signer(t), "/files")
	d := filesystem.NewDisk("local", newMem())

	link, err := u.TemporaryURL(context.Background(), grant(tenant), d, "invoice.pdf", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimPrefix(link, "/files/")

	// The signed expiry is a Unix second, so waiting a fixed millisecond count
	// is a flake: the wait is for the second itself to pass.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, _, err = u.Redeem(token); err != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !errors.Is(err, filesystem.ErrBadURL) {
		t.Fatalf("err = %v, want ErrBadURL", err)
	}
	if !errors.Is(err, encryption.ErrExpired) {
		t.Fatalf("err = %v, want it to still say expired", err)
	}
}

// TestALinkNeedsALifetime: a temporary URL with no expiry is a permanent one,
// and the caller who passed zero did not mean that.
func TestALinkNeedsALifetime(t *testing.T) {
	u := filesystem.NewURLSigner(signer(t), "/files")
	d := filesystem.NewDisk("local", newMem())
	if _, err := u.TemporaryURL(context.Background(), grant(tenant), d, "x.pdf", 0); err == nil {
		t.Fatal("a link with no lifetime was issued")
	}
}

// TestALinkCannotBeIssuedWithoutAGrant: the signature carries a decision, so
// there has to have been one.
func TestALinkCannotBeIssuedWithoutAGrant(t *testing.T) {
	u := filesystem.NewURLSigner(signer(t), "/files")
	d := filesystem.NewDisk("local", newMem())
	if _, err := u.TemporaryURL(context.Background(), auth.Grant{}, d, "x.pdf", time.Minute); !errors.Is(err, filesystem.ErrNoTenant) {
		t.Fatalf("err = %v, want ErrNoTenant", err)
	}
}

// TestAPresigningDiskAnswersFromTheStore: same call, and the bytes never pass
// through the application. A caller choosing between the two is a caller who
// gets it wrong on the disk that changed driver.
func TestAPresigningDiskAnswersFromTheStore(t *testing.T) {
	u := filesystem.NewURLSigner(signer(t), "/files")
	d := filesystem.NewDisk("s3", presignAdapter{newMem()})

	link, err := u.TemporaryURL(context.Background(), grant(tenant), d, "invoice.pdf", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(link, "https://store.example/") {
		t.Fatalf("link = %q, want the store's own URL", link)
	}
	// And the store was handed the resolved path, prefix and all.
	if !strings.Contains(link, tenant+"/invoice.pdf") {
		t.Fatalf("link = %q, want the tenant prefix in it", link)
	}
}

// TestPresigningStillNeedsAGrant: an object store URL is a bearer token for one
// object, and the object is the tenant's.
func TestPresigningStillNeedsAGrant(t *testing.T) {
	u := filesystem.NewURLSigner(signer(t), "/files")
	d := filesystem.NewDisk("s3", presignAdapter{newMem()})
	if _, err := u.TemporaryURL(context.Background(), auth.Grant{}, d, "x.pdf", time.Minute); !errors.Is(err, filesystem.ErrNoTenant) {
		t.Fatalf("err = %v, want ErrNoTenant", err)
	}
}
