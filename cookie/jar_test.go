package cookie_test

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/hesape/cookie"
	"github.com/arandu-io/hesape/support"
)

// ptr is the *bool a Make call passes when it means to say something about
// Secure rather than leave it to the jar.
func ptr(b bool) *bool { return &b }

// make9 is CookieJar::make with the arguments PHP defaults, so a test that only
// cares about one of them says only that one.
func make9(j *cookie.CookieJar, name, value string, minutes int) *http.Cookie {
	return j.Make(name, value, minutes, "", "", nil, true, false, http.SameSiteDefaultMode)
}

func TestMakeFillsInTheJarDefaults(t *testing.T) {
	j := cookie.NewCookieJar()

	c := make9(j, "name", "value", 0)

	if c.Name != "name" || c.Value != "value" {
		t.Fatalf("got %q=%q, want name=value", c.Name, c.Value)
	}
	if c.Path != "/" {
		t.Errorf("Path = %q, want the \"/\" the class initializes $path to", c.Path)
	}
	if c.Domain != "" {
		t.Errorf("Domain = %q, want none", c.Domain)
	}
	if c.Secure {
		t.Error("Secure is set, and the class leaves $secure null")
	}
	if !c.HttpOnly {
		t.Error("HttpOnly is not set, and $httpOnly defaults to true")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want lax, which is what $sameSite initializes to", c.SameSite)
	}
}

func TestMakeWithZeroMinutesIsASessionCookie(t *testing.T) {
	c := make9(cookie.NewCookieJar(), "name", "value", 0)

	if !c.Expires.IsZero() {
		t.Errorf("Expires = %v, want none: PHP passes 0 for the expiry and Symfony reads that as the session", c.Expires)
	}
	if c.MaxAge != 0 {
		t.Errorf("MaxAge = %d, want 0", c.MaxAge)
	}
	if rendered := c.String(); strings.Contains(rendered, "Max-Age") || strings.Contains(rendered, "Expires") {
		t.Errorf("Set-Cookie = %q, want no expiry attribute at all", rendered)
	}
}

func TestMakeTurnsMinutesIntoBothExpiresAndMaxAge(t *testing.T) {
	frozen := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	support.SetTestNow(&frozen)
	defer support.TravelBack()

	c := make9(cookie.NewCookieJar(), "name", "value", 60)

	if want := frozen.Add(time.Hour); !c.Expires.Equal(want) {
		t.Errorf("Expires = %v, want %v: availableAt() is now plus the minutes", c.Expires, want)
	}
	if c.MaxAge != 3600 {
		t.Errorf("MaxAge = %d, want 3600", c.MaxAge)
	}
}

func TestMakeWithNegativeMinutesRendersMaxAgeZero(t *testing.T) {
	// Symfony clamps a past expiry to Max-Age=0, and net/http renders any
	// negative MaxAge the same way. Both tell the browser to drop the cookie.
	c := make9(cookie.NewCookieJar(), "name", "", -2628000)

	rendered := c.String()
	if !strings.Contains(rendered, "Max-Age=0") {
		t.Fatalf("Set-Cookie = %q, want Max-Age=0", rendered)
	}
	if !c.Expires.Before(support.Now()) {
		t.Errorf("Expires = %v, want a date in the past", c.Expires)
	}
}

func TestMakeLetsTheCallOverrideEveryDefault(t *testing.T) {
	j := cookie.NewCookieJar().SetDefaultPathAndDomain("/app", "example.test", true, http.SameSiteLaxMode)

	c := j.Make("name", "value", 0, "/other", "other.test", ptr(false), false, false, http.SameSiteStrictMode)

	if c.Path != "/other" || c.Domain != "other.test" {
		t.Errorf("got path %q domain %q, want the ones the call passed", c.Path, c.Domain)
	}
	if c.Secure {
		t.Error("Secure is set: a non-nil false has to beat the jar's true, which is the is_bool($secure) check")
	}
	if c.HttpOnly {
		t.Error("HttpOnly is set, and the call passed false")
	}
	if c.SameSite != http.SameSiteStrictMode {
		t.Errorf("SameSite = %v, want strict", c.SameSite)
	}
}

func TestMakeFallsBackToTheJarWhenAnArgumentSaysNothing(t *testing.T) {
	j := cookie.NewCookieJar().SetDefaultPathAndDomain("/app", "example.test", true, http.SameSiteNoneMode)

	c := make9(j, "name", "value", 0)

	if c.Path != "/app" || c.Domain != "example.test" {
		t.Errorf("got path %q domain %q, want the jar defaults", c.Path, c.Domain)
	}
	if !c.Secure {
		t.Error("Secure is not set: a nil argument has to fall back to the jar's true")
	}
	if c.SameSite != http.SameSiteNoneMode {
		t.Errorf("SameSite = %v, want none", c.SameSite)
	}
}

func TestForeverIsFourHundredDays(t *testing.T) {
	frozen := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	support.SetTestNow(&frozen)
	defer support.TravelBack()

	c := cookie.NewCookieJar().Forever("name", "value", "", "", nil, true, false, http.SameSiteDefaultMode)

	if want := frozen.AddDate(0, 0, 400); !c.Expires.Equal(want) {
		t.Errorf("Expires = %v, want %v: forever() passes 576000 minutes", c.Expires, want)
	}
	if c.MaxAge != 576000*60 {
		t.Errorf("MaxAge = %d, want %d", c.MaxAge, 576000*60)
	}
}

func TestForgetEmptiesTheValueAndDatesItInThePast(t *testing.T) {
	j := cookie.NewCookieJar()

	c := j.Forget("name", "/app", "example.test")

	if c.Value != "" {
		t.Errorf("Value = %q, want empty: forget() passes null", c.Value)
	}
	if c.Path != "/app" || c.Domain != "example.test" {
		t.Errorf("got path %q domain %q, want the ones passed", c.Path, c.Domain)
	}
	if !c.HttpOnly {
		t.Error("HttpOnly is not set, and forget() leaves $httpOnly at its true default")
	}
	if !strings.Contains(c.String(), "Max-Age=0") {
		t.Errorf("Set-Cookie = %q, want Max-Age=0", c.String())
	}
}

func TestForgetDoesNotQueue(t *testing.T) {
	j := cookie.NewCookieJar()

	j.Forget("name", "", "")

	if j.HasQueued("name", "") {
		t.Fatal("forget() queued: it builds the cookie, and expire() is the one that queues it")
	}
}

func TestQueueAndQueued(t *testing.T) {
	j := cookie.NewCookieJar()
	c := make9(j, "name", "value", 0)

	j.Queue(c)

	if !j.HasQueued("name", "") {
		t.Fatal("HasQueued says no after Queue")
	}
	if got := j.Queued("name", nil, ""); got != c {
		t.Fatalf("Queued returned %v, want the cookie that was queued", got)
	}
	if got := j.Queued("name", nil, "/"); got != c {
		t.Fatalf("Queued by path returned %v, want the cookie", got)
	}
}

func TestQueuedReturnsTheDefaultWhenNothingIsThere(t *testing.T) {
	j := cookie.NewCookieJar()
	fallback := &http.Cookie{Name: "fallback"}

	if got := j.Queued("missing", fallback, ""); got != fallback {
		t.Errorf("Queued of a name never queued returned %v, want the default", got)
	}
	j.Queue(make9(j, "name", "value", 0))
	if got := j.Queued("name", fallback, "/other"); got != fallback {
		t.Errorf("Queued of a path never queued returned %v, want the default", got)
	}
	if j.HasQueued("missing", "") {
		t.Error("HasQueued said yes for a name never queued")
	}
	if j.HasQueued("name", "/other") {
		t.Error("HasQueued said yes for a path never queued")
	}
}

func TestQueuedWithoutAPathReturnsTheOneQueuedLast(t *testing.T) {
	j := cookie.NewCookieJar()
	first := j.Make("name", "first", 0, "/a", "", nil, true, false, http.SameSiteDefaultMode)
	second := j.Make("name", "second", 0, "/b", "", nil, true, false, http.SameSiteDefaultMode)
	j.Queue(first)
	j.Queue(second)

	if got := j.Queued("name", nil, ""); got != second {
		t.Fatalf("Queued returned %v, want the one queued last -- Arr::last over the paths", got)
	}

	// Re-queueing an existing path replaces the cookie without moving the path
	// to the end, because assigning into a PHP array does not move a key.
	replaced := j.Make("name", "replaced", 0, "/a", "", nil, true, false, http.SameSiteDefaultMode)
	j.Queue(replaced)
	if got := j.Queued("name", nil, "/a"); got != replaced {
		t.Fatalf("Queued(/a) returned %v, want the replacement", got)
	}
	if got := j.Queued("name", nil, ""); got != second {
		t.Fatalf("Queued returned %v, want /b still: replacing /a must not make it the last", got)
	}
}

func TestGetQueuedCookiesFlattensNameThenPathInOrder(t *testing.T) {
	j := cookie.NewCookieJar()
	a1 := j.Make("a", "1", 0, "/one", "", nil, true, false, http.SameSiteDefaultMode)
	a2 := j.Make("a", "2", 0, "/two", "", nil, true, false, http.SameSiteDefaultMode)
	b1 := j.Make("b", "1", 0, "/one", "", nil, true, false, http.SameSiteDefaultMode)
	j.Queue(a1)
	j.Queue(b1)
	j.Queue(a2)

	got := j.GetQueuedCookies()

	want := []*http.Cookie{a1, a2, b1}
	if len(got) != len(want) {
		t.Fatalf("GetQueuedCookies returned %d cookies, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("cookie %d is %q=%q, want %q=%q", i, got[i].Name, got[i].Value, want[i].Name, want[i].Value)
		}
	}
}

func TestGetQueuedCookiesOnAnEmptyJar(t *testing.T) {
	if got := cookie.NewCookieJar().GetQueuedCookies(); len(got) != 0 {
		t.Fatalf("GetQueuedCookies on a fresh jar returned %d cookies, want none", len(got))
	}
}

func TestExpireQueuesTheForgetCookie(t *testing.T) {
	j := cookie.NewCookieJar()

	j.Expire("name", "", "")

	queued := j.Queued("name", nil, "/")
	if queued == nil {
		t.Fatal("expire() did not queue anything")
	}
	if queued.Value != "" {
		t.Errorf("queued value = %q, want empty", queued.Value)
	}
	if !strings.Contains(queued.String(), "Max-Age=0") {
		t.Errorf("Set-Cookie = %q, want Max-Age=0", queued.String())
	}
}

func TestUnqueueWithoutAPathTakesEveryPath(t *testing.T) {
	j := cookie.NewCookieJar()
	j.Queue(j.Make("name", "1", 0, "/a", "", nil, true, false, http.SameSiteDefaultMode))
	j.Queue(j.Make("name", "2", 0, "/b", "", nil, true, false, http.SameSiteDefaultMode))

	j.Unqueue("name", "")

	if j.HasQueued("name", "") {
		t.Fatal("the name is still queued")
	}
	if got := j.GetQueuedCookies(); len(got) != 0 {
		t.Fatalf("GetQueuedCookies returned %d cookies, want none", len(got))
	}
}

func TestUnqueueWithAPathTakesOnlyThatPath(t *testing.T) {
	j := cookie.NewCookieJar()
	j.Queue(j.Make("name", "1", 0, "/a", "", nil, true, false, http.SameSiteDefaultMode))
	j.Queue(j.Make("name", "2", 0, "/b", "", nil, true, false, http.SameSiteDefaultMode))

	j.Unqueue("name", "/a")

	if j.HasQueued("name", "/a") {
		t.Error("/a is still queued")
	}
	if !j.HasQueued("name", "/b") {
		t.Error("/b went with it")
	}
	if got := j.Queued("name", nil, ""); got == nil || got.Value != "2" {
		t.Errorf("Queued returned %v, want the cookie left on /b", got)
	}
}

func TestUnqueueDropsTheNameWhenItsLastPathGoes(t *testing.T) {
	j := cookie.NewCookieJar()
	j.Queue(j.Make("name", "1", 0, "/a", "", nil, true, false, http.SameSiteDefaultMode))

	j.Unqueue("name", "/a")

	if j.HasQueued("name", "") {
		t.Fatal("the name survived its last path, which is the empty() check in the PHP")
	}
	// Queueing again after the name was dropped has to work, and has to leave
	// one entry rather than two.
	j.Queue(j.Make("name", "2", 0, "/a", "", nil, true, false, http.SameSiteDefaultMode))
	if got := j.GetQueuedCookies(); len(got) != 1 {
		t.Fatalf("GetQueuedCookies returned %d cookies, want 1", len(got))
	}
}

func TestUnqueueOfSomethingNeverQueuedIsNotAnError(t *testing.T) {
	j := cookie.NewCookieJar()
	j.Queue(make9(j, "name", "value", 0))

	j.Unqueue("missing", "")
	j.Unqueue("missing", "/a")
	j.Unqueue("name", "/never")

	if got := j.GetQueuedCookies(); len(got) != 1 {
		t.Fatalf("GetQueuedCookies returned %d cookies, want the one that was queued", len(got))
	}
}

func TestFlushQueuedCookiesEmptiesTheQueueAndReturnsTheJar(t *testing.T) {
	j := cookie.NewCookieJar()
	j.Queue(make9(j, "name", "value", 0))

	if got := j.FlushQueuedCookies(); got != j {
		t.Error("FlushQueuedCookies returned another jar, and the PHP returns $this")
	}
	if got := j.GetQueuedCookies(); len(got) != 0 {
		t.Fatalf("GetQueuedCookies returned %d cookies after the flush, want none", len(got))
	}
	// The name index has to go with the cookies, or the next queue writes twice.
	j.Queue(make9(j, "name", "again", 0))
	if got := j.GetQueuedCookies(); len(got) != 1 {
		t.Fatalf("GetQueuedCookies returned %d cookies, want 1", len(got))
	}
}

func TestSetDefaultPathAndDomainAssignsWhatItIsGiven(t *testing.T) {
	j := cookie.NewCookieJar()

	if got := j.SetDefaultPathAndDomain("", "", false, http.SameSiteDefaultMode); got != j {
		t.Error("SetDefaultPathAndDomain returned another jar, and the PHP returns $this")
	}

	// Direct assignment, as in the PHP: the "/" and the lax are gone, not kept.
	c := make9(j, "name", "value", 0)
	if c.Path != "" {
		t.Errorf("Path = %q, want empty: the default was cleared", c.Path)
	}
	if c.SameSite != http.SameSiteDefaultMode {
		t.Errorf("SameSite = %v, want none set", c.SameSite)
	}
}

func TestCloneCarriesTheDefaultsAndDoesNotShareTheQueue(t *testing.T) {
	j := cookie.NewCookieJar().SetDefaultPathAndDomain("/app", "example.test", true, http.SameSiteStrictMode)
	j.Queue(make9(j, "boot", "value", 0))

	clone := j.Clone()

	c := make9(clone, "name", "value", 0)
	if c.Path != "/app" || c.Domain != "example.test" || !c.Secure || c.SameSite != http.SameSiteStrictMode {
		t.Errorf("the clone built %+v, want the original's defaults", c)
	}
	if !clone.HasQueued("boot", "") {
		t.Error("the clone lost what was queued before it: PHP's clone copies the property")
	}

	clone.Queue(make9(clone, "later", "value", 0))
	if j.HasQueued("later", "") {
		t.Fatal("a cookie queued on the clone reached the original, so one request would write into another's response")
	}
	j.Unqueue("boot", "")
	if !clone.HasQueued("boot", "") {
		t.Fatal("unqueueing on the original emptied the clone")
	}
}

func TestAJarIsUsableFromManyGoroutines(t *testing.T) {
	j := cookie.NewCookieJar()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for n := 0; n < 50; n++ {
				j.Queue(make9(j, "shared", "value", 0))
				j.Queued("shared", nil, "")
				j.HasQueued("shared", "/")
				j.GetQueuedCookies()
				j.Clone()
				j.Unqueue("shared", "/")
			}
		}(i)
	}
	wg.Wait()
}
