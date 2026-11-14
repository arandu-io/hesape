package support_test

import (
	"testing"

	"github.com/arandu-io/hesape/support"
)

func TestABrandIsTheSlugOfTheApplicationName(t *testing.T) {
	for name, want := range map[string]string{
		"Peráta":      "perata",
		"peráta":      "perata",
		"My App":      "my_app",
		"my-app":      "my_app",
		"  KUAA.io  ": "kuaa_io",
		"Arandu":      "arandu",
	} {
		if got := support.NewBrand(name).Slug(); got != want {
			t.Errorf("NewBrand(%q).Slug() = %q, want %q", name, got, want)
		}
	}
}

func TestANameWithNoLetterIsTheDefaultBrand(t *testing.T) {
	for _, name := range []string{"", "   ", "---", "日本語"} {
		b := support.NewBrand(name)
		if !b.IsDefault() || b.Slug() != support.DefaultBrand {
			t.Errorf("NewBrand(%q) = %q, want the default brand", name, b.Slug())
		}
	}
	if !(support.Brand{}).IsDefault() || (support.Brand{}).Slug() != "arandu" {
		t.Fatal("the zero Brand is not the default one")
	}
}

func TestABrandNamesWhatTheBrowserSees(t *testing.T) {
	b := support.NewBrand("Peráta")
	if got := b.Cookie("session"); got != "perata_session" {
		t.Errorf("Cookie = %q", got)
	}
	if got := b.StorageKey("theme"); got != "perata.theme" {
		t.Errorf("StorageKey = %q", got)
	}
	if got := b.Path("assets/"); got != "/_perata/assets/" {
		t.Errorf("Path = %q", got)
	}
	d := support.Brand{}
	if d.Cookie("session") != "arandu_session" || d.StorageKey("theme") != "arandu.theme" || d.Path("assets/") != "/_arandu/assets/" {
		t.Error("the default brand does not name things as the collection always did")
	}
}
