package filesystem_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/filesystem"
)

// TestConfigureBuildsEveryDiskTheConfigurationDescribes: the disks are built
// from configuration alone, a scoped disk after the one it narrows whatever
// the names sort to -- "assets" sorts before "local" and is built after it.
func TestConfigureBuildsEveryDiskTheConfigurationDescribes(t *testing.T) {
	disks := filesystem.NewDisks("local")
	err := disks.Configure(map[string]filesystem.Config{
		"local":   {Driver: "local", Root: t.TempDir()},
		"assets":  {Driver: "scoped", Disk: "local", Prefix: "assets"},
		"avatars": {Driver: "scoped", Disk: "assets", Prefix: "avatars"},
	})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}

	if got := strings.Join(disks.Names(), ","); got != "assets,avatars,local" {
		t.Fatalf("registered disks = %s, want every disk the configuration describes", got)
	}
	avatars, err := disks.Disk("avatars")
	if err != nil {
		t.Fatalf("Disk(avatars): %v", err)
	}
	put(t, avatars, grant(tenant), "me.png", "png")
}

// TestConfigureRefusesScopedDisksThatNarrowEachOther: a circle has no disk to
// start from, and saying so beats a boot that loops or a set missing both.
func TestConfigureRefusesScopedDisksThatNarrowEachOther(t *testing.T) {
	disks := filesystem.NewDisks("")
	err := disks.Configure(map[string]filesystem.Config{
		"x": {Driver: "scoped", Disk: "y", Prefix: "x"},
		"y": {Driver: "scoped", Disk: "x", Prefix: "y"},
	})
	if err == nil || !strings.Contains(err.Error(), "circle") {
		t.Fatalf("Configure answered %v, want the circle named", err)
	}
}

// TestConfigureNamesTheDiskItCouldNotBuild: a configuration naming a driver
// nobody registered stops the call, and the error says which disk.
func TestConfigureNamesTheDiskItCouldNotBuild(t *testing.T) {
	disks := filesystem.NewDisks("files")
	err := disks.Configure(map[string]filesystem.Config{"files": {Driver: "s3", Bucket: "uploads"}})
	if err == nil || !strings.Contains(err.Error(), `"files"`) {
		t.Fatalf("Configure answered %v, want the disk named", err)
	}
}

// TestAnExtendedDriverIsHandedTheObjectStorageFields: the S3 driver lives in
// its own module and reads its bucket, endpoint, region, path style and
// credentials off the disk's Config, so they have to arrive as written.
func TestAnExtendedDriverIsHandedTheObjectStorageFields(t *testing.T) {
	var got filesystem.Config
	disks := filesystem.NewDisks("files")
	disks.Extend("s3", func(_ string, cfg filesystem.Config) (filesystem.Adapter, error) {
		got = cfg
		return newMem(), nil
	})

	want := filesystem.Config{
		Driver: "s3", Bucket: "kuaa-prod", Endpoint: "https://acct.r2.cloudflarestorage.com",
		Region: "auto", PathStyle: true, AccessKey: "key-id", SecretKey: "s3cr3t",
	}
	if err := disks.Configure(map[string]filesystem.Config{"files": want}); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if got.Bucket != want.Bucket || got.Endpoint != want.Endpoint || got.Region != want.Region ||
		!got.PathStyle || got.AccessKey != want.AccessKey || got.SecretKey != want.SecretKey {
		t.Fatalf("the driver was handed %#v, want the fields the configuration wrote", got)
	}
	if _, err := disks.Disk("files"); err != nil {
		t.Fatalf("Disk(files): %v", err)
	}
}

// TestAConfigNeverPrintsItsCredentials: a disk's configuration ends up in an
// error message or a log line sooner or later, and a secret printed there is a
// secret in the log store.
func TestAConfigNeverPrintsItsCredentials(t *testing.T) {
	cfg := filesystem.Config{Driver: "s3", Bucket: "uploads", AccessKey: "AKIDEXAMPLE", SecretKey: "wJalrXUtnFEMI"}

	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		printed := fmt.Sprintf(verb, cfg)
		if strings.Contains(printed, "AKIDEXAMPLE") || strings.Contains(printed, "wJalrXUtnFEMI") {
			t.Fatalf("%s printed the credentials: %s", verb, printed)
		}
		if !strings.Contains(printed, "uploads") || !strings.Contains(printed, "[redacted]") {
			t.Fatalf("%s printed %s, want the bucket and the credentials marked redacted", verb, printed)
		}
	}
	if printed := fmt.Sprint(filesystem.Config{Driver: "local"}); strings.Contains(printed, "[redacted]") {
		t.Fatalf("a disk with no credentials printed %s, as if it had some", printed)
	}
}
