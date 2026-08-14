package session_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/encryption"
	"github.com/arandu-io/hesape/filesystem"
	"github.com/arandu-io/hesape/session"
)

func newStoreOver(h session.SessionHandler) *session.Store {
	return session.NewStore("arandu_session", h, "")
}

func startedStore(t *testing.T, h session.SessionHandler) *session.Store {
	t.Helper()
	s := newStoreOver(h)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	return s
}

func TestPutAndGetSpeakDotNotation(t *testing.T) {
	s := startedStore(t, session.NewArraySessionHandler(time.Hour))

	s.Put("user.name", "Paulo")
	s.Put("user.roles", []string{"owner"})

	if got := s.Get("user.name"); got != "Paulo" {
		t.Fatalf("got %v", got)
	}
	nested, ok := s.Get("user").(map[string]any)
	if !ok || nested["name"] != "Paulo" {
		t.Fatalf("the dotted key did not nest: %v", s.Get("user"))
	}
	if got := s.Get("user.missing", "fallback"); got != "fallback" {
		t.Fatalf("got %v", got)
	}
	if got := s.Get("nothing"); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestExistsAndHasAskDifferentQuestions(t *testing.T) {
	s := startedStore(t, session.NewArraySessionHandler(time.Hour))
	s.Put("present", nil)
	s.Put("filled", "x")

	if !s.Exists("present") {
		t.Fatal("a key holding nil does not exist")
	}
	if s.Has("present") {
		t.Fatal("a key holding nil reads as having something")
	}
	if !s.Has("filled") || !s.Exists("filled") {
		t.Fatal("a filled key is missing")
	}
	if !s.Missing("absent") {
		t.Fatal("a key nobody set is not missing")
	}
	if !s.HasAny("absent", "filled") {
		t.Fatal("HasAny wants all of them")
	}
	if s.HasAny("absent", "present") {
		t.Fatal("HasAny counted a nil")
	}
	// The empty case: false, and Illuminate's is true. See the doc comment.
	if s.Has() || s.Exists() {
		t.Fatal("asking about no keys answered yes")
	}
}

func TestOnlyExceptAndAll(t *testing.T) {
	s := startedStore(t, session.NewArraySessionHandler(time.Hour))
	s.Put("a", 1)
	s.Put("b", 2)

	only := s.Only([]string{"a"})
	if len(only) != 1 || only["a"] != 1 {
		t.Fatalf("only: %v", only)
	}
	except := s.Except([]string{"a"})
	if _, there := except["a"]; there {
		t.Fatalf("except: %v", except)
	}
	if except["b"] != 2 {
		t.Fatalf("except dropped what it should keep: %v", except)
	}

	// All is a copy: mutating it must not reach into the session, or a handler
	// writing the map it was handed rewrites the live one.
	all := s.All()
	all["a"] = 99
	if s.Get("a") != 1 {
		t.Fatal("All handed out the live map")
	}
}

func TestPullAndRemoveTakeTheValueWithThem(t *testing.T) {
	s := startedStore(t, session.NewArraySessionHandler(time.Hour))
	s.Put("once", "value")
	s.Put("twice", "value")

	if got := s.Pull("once"); got != "value" {
		t.Fatalf("pull: %v", got)
	}
	if s.Exists("once") {
		t.Fatal("pull left the key behind")
	}
	if got := s.Remove("twice"); got != "value" {
		t.Fatalf("remove: %v", got)
	}
	if s.Exists("twice") {
		t.Fatal("remove left the key behind")
	}
	if got := s.Pull("never", "default"); got != "default" {
		t.Fatalf("pull of an absent key: %v", got)
	}
}

func TestPushIncrementAndDecrement(t *testing.T) {
	s := startedStore(t, session.NewArraySessionHandler(time.Hour))

	s.Push("cart", "apple")
	s.Push("cart", "pear")
	cart, _ := s.Get("cart").([]any)
	if len(cart) != 2 || cart[1] != "pear" {
		t.Fatalf("push: %v", s.Get("cart"))
	}

	if got := s.Increment("views", 1); got != 1 {
		t.Fatalf("a counter that was never set started at %d", got)
	}
	if got := s.Increment("views", 4); got != 5 {
		t.Fatalf("increment: %d", got)
	}
	if got := s.Decrement("views", 2); got != 3 {
		t.Fatalf("decrement: %d", got)
	}
}

func TestRememberStoresWhatTheCallbackProducedOnce(t *testing.T) {
	s := startedStore(t, session.NewArraySessionHandler(time.Hour))
	calls := 0

	first := s.Remember("expensive", func() any { calls++; return "value" })
	second := s.Remember("expensive", func() any { calls++; return "other" })

	if first != "value" || second != "value" || calls != 1 {
		t.Fatalf("got %v, %v after %d calls", first, second, calls)
	}
}

func TestReplaceReplacesTheNamedKeysAndFlushEmptiesTheSession(t *testing.T) {
	s := startedStore(t, session.NewArraySessionHandler(time.Hour))
	s.Put("keep", "kept")

	s.Replace(map[string]any{"a": 1})
	if s.Get("keep") != "kept" {
		t.Fatal("Replace emptied the session; that is Flush")
	}
	if s.Get("a") != 1 {
		t.Fatal("Replace did not write")
	}

	s.Flush()
	if len(s.All()) != 0 {
		t.Fatalf("flush left %v", s.All())
	}
}

// TestFlashCrossesExactlyOneRequest is the assertion the whole flash exists for:
// a message put in by the request that was rejected has to be readable by the
// page the redirect lands on, and must NOT be readable by the one after that.
// Getting the second half wrong puts a stale message on a page nobody
// submitted, and the person reading it has no way to tell.
func TestFlashCrossesExactlyOneRequest(t *testing.T) {
	handler := session.NewArraySessionHandler(time.Hour)
	ctx := context.Background()

	// Request one: the form is rejected, and it flashes.
	first := startedStore(t, handler)
	id := first.GetID()
	first.Flash("status", "Your changes were not saved.")
	first.FlashInput(map[string]any{"email": "paulo@example.com"})
	if err := first.Save(ctx); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Request two: the page the redirect landed on. It reads the message and the
	// old input.
	second := session.NewStore("arandu_session", handler, id)
	if err := second.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if got := second.Get("status"); got != "Your changes were not saved." {
		t.Fatalf("the message did not survive the redirect: %v", got)
	}
	if got := second.GetOldInput("email"); got != "paulo@example.com" {
		t.Fatalf("the old input did not survive the redirect: %v", got)
	}
	if !second.HasOldInput("email") || !second.HasOldInput("") {
		t.Fatal("HasOldInput disagrees with GetOldInput")
	}
	if err := second.Save(ctx); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Request three: a reload of the same page. The message is gone.
	third := session.NewStore("arandu_session", handler, id)
	if err := third.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if got := third.Get("status"); got != nil {
		t.Fatalf("the message outlived its redirect and is on a page nobody submitted: %v", got)
	}
	if third.HasOldInput("") {
		t.Fatalf("the old input outlived its redirect: %v", third.GetOldInput(""))
	}
}

// TestReflashCarriesTheMessageOneRequestFurther covers the request that could
// not draw the page -- a redirect in the middle of a flow.
func TestReflashCarriesTheMessageOneRequestFurther(t *testing.T) {
	handler := session.NewArraySessionHandler(time.Hour)
	ctx := context.Background()

	first := startedStore(t, handler)
	id := first.GetID()
	first.Flash("status", "saved")
	if err := first.Save(ctx); err != nil {
		t.Fatalf("save: %v", err)
	}

	second := session.NewStore("arandu_session", handler, id)
	if err := second.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	second.Reflash()
	if err := second.Save(ctx); err != nil {
		t.Fatalf("save: %v", err)
	}

	third := session.NewStore("arandu_session", handler, id)
	if err := third.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if got := third.Get("status"); got != "saved" {
		t.Fatalf("reflash did not carry the message: %v", got)
	}
	if err := third.Save(ctx); err != nil {
		t.Fatalf("save: %v", err)
	}

	fourth := session.NewStore("arandu_session", handler, id)
	if err := fourth.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if got := fourth.Get("status"); got != nil {
		t.Fatalf("reflash extended the message forever: %v", got)
	}
}

// TestKeepReflashesOnlyWhatItNames.
func TestKeepReflashesOnlyWhatItNames(t *testing.T) {
	handler := session.NewArraySessionHandler(time.Hour)
	ctx := context.Background()

	first := startedStore(t, handler)
	id := first.GetID()
	first.Flash("kept", "a")
	first.Flash("dropped", "b")
	if err := first.Save(ctx); err != nil {
		t.Fatalf("save: %v", err)
	}

	second := session.NewStore("arandu_session", handler, id)
	if err := second.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	second.Keep("kept")
	if err := second.Save(ctx); err != nil {
		t.Fatalf("save: %v", err)
	}

	third := session.NewStore("arandu_session", handler, id)
	if err := third.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if got := third.Get("kept"); got != "a" {
		t.Fatalf("Keep did not keep: %v", got)
	}
	if got := third.Get("dropped"); got != nil {
		t.Fatalf("Keep kept what it was not told to: %v", got)
	}
}

// TestNowIsGoneByTheNextRequest: a message for the page this request is about to
// draw, with no redirect in between.
func TestNowIsGoneByTheNextRequest(t *testing.T) {
	handler := session.NewArraySessionHandler(time.Hour)
	ctx := context.Background()

	first := startedStore(t, handler)
	id := first.GetID()
	first.Now("status", "drawn on this very page")
	if got := first.Get("status"); got != "drawn on this very page" {
		t.Fatalf("Now is not readable by the request that set it: %v", got)
	}
	if err := first.Save(ctx); err != nil {
		t.Fatalf("save: %v", err)
	}

	second := session.NewStore("arandu_session", handler, id)
	if err := second.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if got := second.Get("status"); got != nil {
		t.Fatalf("Now survived into the next request: %v", got)
	}
}

// TestFlashingTheSameKeyTwiceExtendsItRatherThanExpiringEarly is the bug the
// removeFromOldFlashData call in Flash exists to stop: a key flashed by a
// request that had also just read it would otherwise be aged out at the end of
// that same request.
func TestFlashingTheSameKeyTwiceExtendsItRatherThanExpiringEarly(t *testing.T) {
	handler := session.NewArraySessionHandler(time.Hour)
	ctx := context.Background()

	first := startedStore(t, handler)
	id := first.GetID()
	first.Flash("status", "one")
	if err := first.Save(ctx); err != nil {
		t.Fatalf("save: %v", err)
	}

	second := session.NewStore("arandu_session", handler, id)
	if err := second.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	second.Flash("status", "two")
	if err := second.Save(ctx); err != nil {
		t.Fatalf("save: %v", err)
	}

	third := session.NewStore("arandu_session", handler, id)
	if err := third.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if got := third.Get("status"); got != "two" {
		t.Fatalf("re-flashing expired the key instead of extending it: %v", got)
	}
}

func TestStartMintsATokenAndRegenerateReplacesIt(t *testing.T) {
	s := startedStore(t, session.NewArraySessionHandler(time.Hour))

	first := s.Token()
	if len(first) != 40 {
		t.Fatalf("the token is %q", first)
	}
	s.RegenerateToken()
	if s.Token() == first {
		t.Fatal("the token did not change")
	}
	if !s.IsStarted() {
		t.Fatal("the session says it has not started")
	}
}

func TestRegenerateChangesTheIDAndTheTokenAndDestroysTheOldRecord(t *testing.T) {
	handler := session.NewArraySessionHandler(time.Hour)
	ctx := context.Background()

	s := startedStore(t, handler)
	s.Put("subject", "1")
	if err := s.Save(ctx); err != nil {
		t.Fatalf("save: %v", err)
	}
	old := s.GetID()
	oldToken := s.Token()

	if err := s.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := s.Regenerate(ctx, true); err != nil {
		t.Fatalf("regenerate: %v", err)
	}

	if s.GetID() == old {
		t.Fatal("the id did not change, which is session fixation")
	}
	if s.Token() == oldToken {
		t.Fatal("the token did not change")
	}
	// The record under the old id is gone, so a cookie somebody planted before
	// the sign-in is worthless.
	stored, err := handler.Read(ctx, old)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if stored != "" {
		t.Fatal("the old record survived a destroying regenerate")
	}
	// The session kept what was in it: regenerating is not signing out.
	if s.Get("subject") != "1" {
		t.Fatal("regenerate emptied the session; that is Invalidate")
	}
}

func TestInvalidateEmptiesTheSessionAndGivesItANewID(t *testing.T) {
	handler := session.NewArraySessionHandler(time.Hour)
	ctx := context.Background()

	s := startedStore(t, handler)
	s.Put("subject", "1")
	if err := s.Save(ctx); err != nil {
		t.Fatalf("save: %v", err)
	}
	old := s.GetID()

	if err := s.Invalidate(ctx); err != nil {
		t.Fatalf("invalidate: %v", err)
	}
	if s.GetID() == old {
		t.Fatal("the id did not change")
	}
	if len(s.All()) != 0 {
		t.Fatalf("the session still holds %v", s.All())
	}
	stored, _ := handler.Read(ctx, old)
	if stored != "" {
		t.Fatal("the old record survived a sign-out")
	}
}

func TestIsValidIDRefusesAnythingThatIsNotOne(t *testing.T) {
	s := newStoreOver(session.NewNullSessionHandler())

	if !s.IsValidID(strings.Repeat("a", 40)) {
		t.Fatal("forty letters is not a session id")
	}
	for _, bad := range []string{
		"",
		strings.Repeat("a", 39),
		strings.Repeat("a", 41),
		"../../etc/passwd" + strings.Repeat("a", 24),
		strings.Repeat("a", 39) + "/",
		strings.Repeat("a", 39) + "-",
	} {
		if s.IsValidID(bad) {
			t.Fatalf("%q was accepted as a session id", bad)
		}
	}
}

// TestSetIDMintsOneRatherThanRefusing: a cookie is a value anybody can set, so
// refusing would turn it into an error page an attacker can force on somebody.
func TestSetIDMintsOneRatherThanRefusing(t *testing.T) {
	s := newStoreOver(session.NewNullSessionHandler())

	s.SetID("../../etc/passwd")
	if !s.IsValidID(s.GetID()) {
		t.Fatalf("a forged cookie became the session id: %q", s.GetID())
	}
	fresh := strings.Repeat("b", 40)
	s.SetID(fresh)
	if s.ID() != fresh || s.GetID() != fresh {
		t.Fatalf("a valid id was not kept: %q", s.GetID())
	}
}

func TestNameRoundTrips(t *testing.T) {
	s := newStoreOver(session.NewNullSessionHandler())

	if s.GetName() != "arandu_session" {
		t.Fatalf("got %q", s.GetName())
	}
	s.SetName("other")
	if s.GetName() != "other" {
		t.Fatalf("got %q", s.GetName())
	}
}

func TestPreviousURL(t *testing.T) {
	s := startedStore(t, session.NewArraySessionHandler(time.Hour))

	if s.HasPreviousURI() {
		t.Fatal("a fresh session remembers an address")
	}
	if _, err := s.PreviousURI(); !errors.Is(err, session.ErrNoPreviousURL) {
		t.Fatalf("got %v, want ErrNoPreviousURL", err)
	}

	s.SetPreviousURL("/invoices?page=2")
	if !s.HasPreviousURI() || s.PreviousURL() != "/invoices?page=2" {
		t.Fatalf("got %q", s.PreviousURL())
	}
	parsed, err := s.PreviousURI()
	if err != nil {
		t.Fatalf("previous uri: %v", err)
	}
	if parsed.Path != "/invoices" || parsed.Query().Get("page") != "2" {
		t.Fatalf("got %v", parsed)
	}
}

func TestPasswordConfirmedStampsTheKeyIlluminateUses(t *testing.T) {
	s := startedStore(t, session.NewArraySessionHandler(time.Hour))

	s.PasswordConfirmed()
	stamp := s.Get(session.PasswordConfirmedKey)
	if stamp == nil {
		t.Fatal("nothing was stamped")
	}
	if seconds, ok := stamp.(int64); !ok || time.Since(time.Unix(seconds, 0)) > time.Minute {
		t.Fatalf("got %v", stamp)
	}
}

func TestGetHandlerSetHandlerAndTheRequestQuestion(t *testing.T) {
	array := session.NewArraySessionHandler(time.Hour)
	s := newStoreOver(array)

	if s.GetHandler() != session.SessionHandler(array) {
		t.Fatal("GetHandler answered with something else")
	}
	if s.HandlerNeedsRequest() {
		t.Fatal("the array handler was asked for a request")
	}

	cookie := session.NewCookieSessionHandler(&fakeJar{}, time.Hour, false)
	s.SetHandler(cookie)
	if !s.HandlerNeedsRequest() {
		t.Fatal("the cookie handler was not asked for a request")
	}
	// SetRequestOnHandler must be a no-op for a handler that does not want one,
	// or wiring it into the middleware would need a branch at the call site.
	s.SetRequestOnHandler(httptest.NewRequest(http.MethodGet, "/", nil))
	s.SetHandler(array)
	s.SetRequestOnHandler(httptest.NewRequest(http.MethodGet, "/", nil))
}

// fakeJar records what a handler queued.
type fakeJar struct {
	queued    map[string]string
	forgotten []string
}

func (j *fakeJar) Queue(name, value string, lifetime time.Duration) {
	if j.queued == nil {
		j.queued = map[string]string{}
	}
	j.queued[name] = value
}

func (j *fakeJar) Forget(name string) { j.forgotten = append(j.forgotten, name) }

func TestTheSessionSurvivesTheHandlerRoundTrip(t *testing.T) {
	handler := session.NewArraySessionHandler(time.Hour)
	ctx := context.Background()

	first := startedStore(t, handler)
	id := first.GetID()
	first.Put("user.name", "Paulo")
	first.Put("count", 3)
	if err := first.Save(ctx); err != nil {
		t.Fatalf("save: %v", err)
	}

	second := session.NewStore("arandu_session", handler, id)
	if err := second.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if got := second.Get("user.name"); got != "Paulo" {
		t.Fatalf("got %v", got)
	}
	// A number that has been through JSON comes back a float64. Increment is
	// the method that knows, which is why a caller should not assert the type.
	if got := second.Increment("count", 1); got != 4 {
		t.Fatalf("got %d", got)
	}
}

func TestAnUnreadablePayloadStartsAnEmptySessionRatherThanFailing(t *testing.T) {
	handler := session.NewArraySessionHandler(time.Hour)
	ctx := context.Background()
	id := strings.Repeat("c", 40)

	if err := handler.Write(ctx, id, "this is not JSON"); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := session.NewStore("arandu_session", handler, id)
	if err := s.Start(ctx); err != nil {
		t.Fatalf("a payload written by an older release became an error: %v", err)
	}
	if s.Token() == "" {
		t.Fatal("the empty session did not get a token")
	}
}

func TestAnExpiredSessionReadsAsEmpty(t *testing.T) {
	handler := session.NewArraySessionHandler(time.Millisecond)
	ctx := context.Background()

	first := startedStore(t, handler)
	id := first.GetID()
	first.Put("subject", "1")
	if err := first.Save(ctx); err != nil {
		t.Fatalf("save: %v", err)
	}
	time.Sleep(5 * time.Millisecond)

	second := session.NewStore("arandu_session", handler, id)
	if err := second.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if second.Get("subject") != nil {
		t.Fatal("an expired session came back")
	}
}

func TestGarbageCollectionSweepsWhatIsIdle(t *testing.T) {
	handler := session.NewArraySessionHandler(time.Hour)
	ctx := context.Background()

	for range 3 {
		s := startedStore(t, handler)
		if err := s.Save(ctx); err != nil {
			t.Fatalf("save: %v", err)
		}
	}
	deleted, err := handler.GC(ctx, time.Nanosecond)
	if err != nil {
		t.Fatalf("gc: %v", err)
	}
	if deleted != 3 {
		t.Fatalf("swept %d, want 3", deleted)
	}
}

func TestTheNullHandlerRemembersNothing(t *testing.T) {
	handler := session.NewNullSessionHandler()
	ctx := context.Background()

	s := startedStore(t, handler)
	id := s.GetID()
	s.Put("subject", "1")
	if err := s.Save(ctx); err != nil {
		t.Fatalf("save: %v", err)
	}

	second := session.NewStore("arandu_session", handler, id)
	if err := second.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if second.Get("subject") != nil {
		t.Fatal("the null handler remembered something")
	}
	if n, err := handler.GC(ctx, time.Hour); err != nil || n != 0 {
		t.Fatalf("gc: %d, %v", n, err)
	}
}

func TestTheFileHandlerRoundTripsAndCollects(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	handler, err := session.NewFileSessionHandler(nil, dir, time.Hour)
	if err != nil {
		t.Fatalf("file handler: %v", err)
	}
	ctx := context.Background()

	first := startedStore(t, handler)
	id := first.GetID()
	first.Put("subject", "1")
	if err := first.Save(ctx); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, id)); err != nil {
		t.Fatalf("the session file is not where it should be: %v", err)
	}

	second := session.NewStore("arandu_session", handler, id)
	if err := second.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if second.Get("subject") != "1" {
		t.Fatal("the session did not come back off disk")
	}

	deleted, err := handler.GC(ctx, time.Nanosecond)
	if err != nil || deleted != 1 {
		t.Fatalf("gc: %d, %v", deleted, err)
	}
	if err := handler.Destroy(ctx, id); err != nil {
		t.Fatalf("destroy: %v", err)
	}
}

// TestTheFileHandlerCannotBeWalkedOutOfItsDirectory. The id has already been
// through IsValidID by the time a request reaches here, and this is the second
// lock on the same door.
func TestTheFileHandlerCannotBeWalkedOutOfItsDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sessions")
	handler, err := session.NewFileSessionHandler(nil, dir, time.Hour)
	if err != nil {
		t.Fatalf("file handler: %v", err)
	}
	ctx := context.Background()

	if err := handler.Write(ctx, "../escaped", "payload"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "escaped")); err == nil {
		t.Fatal("the handler wrote outside its directory")
	}
}

func TestTheCookieHandlerCarriesTheSessionInTheRequest(t *testing.T) {
	jar := &fakeJar{}
	handler := session.NewCookieSessionHandler(jar, time.Hour, false)
	ctx := context.Background()
	id := strings.Repeat("d", 40)

	if err := handler.Write(ctx, id, `{"subject":"1"}`); err != nil {
		t.Fatalf("write: %v", err)
	}
	value, ok := jar.queued[id]
	if !ok {
		t.Fatalf("nothing was queued: %v", jar.queued)
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: id, Value: value})
	handler.SetRequest(r)

	got, err := handler.Read(ctx, id)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != `{"subject":"1"}` {
		t.Fatalf("got %q", got)
	}

	// A handler with no request on it answers empty rather than panicking: that
	// is a route wired without StartSession, and a 500 is the wrong answer.
	fresh := session.NewCookieSessionHandler(jar, time.Hour, false)
	if got, err := fresh.Read(ctx, id); got != "" || err != nil {
		t.Fatalf("got %q, %v", got, err)
	}

	if err := handler.Destroy(ctx, id); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if len(jar.forgotten) != 1 || jar.forgotten[0] != id {
		t.Fatalf("destroy queued %v", jar.forgotten)
	}
}

func TestTheCookieHandlerRefusesAPayloadThatHasRunOut(t *testing.T) {
	handler := session.NewCookieSessionHandler(&fakeJar{}, -time.Hour, false)
	ctx := context.Background()
	id := strings.Repeat("e", 40)

	jar := &fakeJar{}
	writer := session.NewCookieSessionHandler(jar, -time.Hour, false)
	if err := writer.Write(ctx, id, `{"subject":"1"}`); err != nil {
		t.Fatalf("write: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: id, Value: jar.queued[id]})
	handler.SetRequest(r)

	// The expiry inside the value is what is checked, because the cookie's own
	// expiry is enforced by the browser and the browser is what is distrusted.
	if got, _ := handler.Read(ctx, id); got != "" {
		t.Fatalf("an expired cookie session came back: %q", got)
	}
}

func TestTheCacheHandlerGoesThroughTheCache(t *testing.T) {
	cache := &fakeCache{entries: map[string]string{}}
	handler := session.NewCacheBasedSessionHandler(cache, time.Hour)
	ctx := context.Background()
	id := strings.Repeat("f", 40)

	if handler.GetCache() != session.Cache(cache) {
		t.Fatal("GetCache answered with something else")
	}
	if err := handler.Write(ctx, id, "payload"); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := handler.Read(ctx, id)
	if err != nil || got != "payload" {
		t.Fatalf("got %q, %v", got, err)
	}
	if err := handler.Destroy(ctx, id); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if got, _ := handler.Read(ctx, id); got != "" {
		t.Fatalf("got %q after destroy", got)
	}
	if n, err := handler.GC(ctx, time.Hour); err != nil || n != 0 {
		t.Fatalf("gc: %d, %v", n, err)
	}
}

type fakeCache struct{ entries map[string]string }

func (c *fakeCache) Get(_ context.Context, key string) (string, error) { return c.entries[key], nil }

func (c *fakeCache) Put(_ context.Context, key, value string, _ time.Duration) error {
	c.entries[key] = value
	return nil
}

func (c *fakeCache) Forget(_ context.Context, key string) error {
	delete(c.entries, key)
	return nil
}

func TestTheDatabaseHandlerRefusesATableNameThatIsNotOne(t *testing.T) {
	for _, bad := range []string{"", "sessions; DROP TABLE users", `sessions"`, "sessions-1"} {
		if _, err := session.NewDatabaseSessionHandler(nil, bad, time.Hour); !errors.Is(err, session.ErrBadTableName) {
			t.Fatalf("%q was accepted as a table name: %v", bad, err)
		}
	}
	if _, err := session.NewDatabaseSessionHandler(nil, "sessions", time.Hour); err != nil {
		t.Fatalf("a plain identifier was refused: %v", err)
	}
}

func TestTheEncryptedStoreSealsWhatReachesTheHandler(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	encrypter, err := encryption.NewEncrypter(key, encryption.AES256GCM)
	if err != nil {
		t.Fatalf("encrypter: %v", err)
	}
	handler := session.NewArraySessionHandler(time.Hour)
	ctx := context.Background()

	first := session.NewEncryptedStore("arandu_session", handler, encrypter, "")
	if err := first.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	id := first.GetID()
	first.Put("subject", "paulo@example.com")
	if err := first.Save(ctx); err != nil {
		t.Fatalf("save: %v", err)
	}
	if first.GetEncrypter() != session.Encrypter(encrypter) {
		t.Fatal("GetEncrypter answered with something else")
	}

	stored, err := handler.Read(ctx, id)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(stored, "paulo@example.com") {
		t.Fatalf("the session reached the handler in the clear: %q", stored)
	}

	second := session.NewEncryptedStore("arandu_session", handler, encrypter, id)
	if err := second.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if got := second.Get("subject"); got != "paulo@example.com" {
		t.Fatalf("got %v", got)
	}
}

func TestAnEncryptedSessionUnderAnotherKeyStartsEmptyRatherThanFailing(t *testing.T) {
	handler := session.NewArraySessionHandler(time.Hour)
	ctx := context.Background()

	mine, _ := encryption.NewEncrypter([]byte("0123456789abcdef0123456789abcdef"), encryption.AES256GCM)
	theirs, _ := encryption.NewEncrypter([]byte("fedcba9876543210fedcba9876543210"), encryption.AES256GCM)

	first := session.NewEncryptedStore("arandu_session", handler, mine, "")
	if err := first.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	id := first.GetID()
	first.Put("subject", "1")
	if err := first.Save(ctx); err != nil {
		t.Fatalf("save: %v", err)
	}

	// A key rotation, or somebody else's cookie. Signing the person out is
	// survivable; a 500 on every page until they clear their cookies is not.
	second := session.NewEncryptedStore("arandu_session", handler, theirs, id)
	if err := second.Start(ctx); err != nil {
		t.Fatalf("a session under another key became an error: %v", err)
	}
	if second.Get("subject") != nil {
		t.Fatal("a session under another key was read")
	}
}

func TestTheManagerBuildsANewStorePerCallOverASharedHandler(t *testing.T) {
	manager := session.NewSessionManager(session.Config{Driver: "array", Cookie: "arandu_session"}, nil)
	ctx := context.Background()

	first, err := manager.Driver("")
	if err != nil {
		t.Fatalf("driver: %v", err)
	}
	second, err := manager.Driver("")
	if err != nil {
		t.Fatalf("driver: %v", err)
	}
	if first == second {
		t.Fatal("two requests were handed the same Store, and they would save each other's session")
	}
	if first.GetHandler() != second.GetHandler() {
		t.Fatal("two requests were handed different handlers, and neither would see the other's session")
	}
	if first.GetID() == second.GetID() {
		t.Fatal("two fresh sessions got the same id")
	}

	if err := first.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	first.Put("subject", "1")
	if err := first.Save(ctx); err != nil {
		t.Fatalf("save: %v", err)
	}

	third, _ := manager.Driver("")
	third.SetID(first.GetID())
	if err := third.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if third.Get("subject") != "1" {
		t.Fatal("the handler is not shared between requests")
	}
}

func TestTheManagerNamesTheDriverItCannotBuildAlone(t *testing.T) {
	manager := session.NewSessionManager(session.Config{Driver: "database"}, nil)

	_, err := manager.Driver("")
	if !errors.Is(err, session.ErrNoDriver) {
		t.Fatalf("got %v, want ErrNoDriver", err)
	}
	if !strings.Contains(err.Error(), "Extend") {
		t.Fatalf("the error does not say how to fix it: %v", err)
	}

	manager.Extend("database", func(cfg session.Config) (session.SessionHandler, error) {
		return session.NewArraySessionHandler(cfg.Lifetime), nil
	})
	if _, err := manager.Driver(""); err != nil {
		t.Fatalf("the registered driver was not used: %v", err)
	}
}

func TestTheManagerRefusesEncryptionWithNoEncrypter(t *testing.T) {
	manager := session.NewSessionManager(session.Config{Driver: "array", Encrypt: true}, nil)

	if _, err := manager.Driver(""); err == nil {
		t.Fatal("a session configured to be encrypted was built in the clear")
	}
}

func TestTheManagerAnswersTheBlockingQuestions(t *testing.T) {
	manager := session.NewSessionManager(session.Config{Driver: "array"}, nil)

	if manager.ShouldBlock() {
		t.Fatal("blocking is on by default")
	}
	if manager.DefaultRouteBlockLockSeconds() != session.DefaultBlockLockSeconds {
		t.Fatalf("got %v", manager.DefaultRouteBlockLockSeconds())
	}
	if manager.DefaultRouteBlockWaitSeconds() != session.DefaultBlockWaitSeconds {
		t.Fatalf("got %v", manager.DefaultRouteBlockWaitSeconds())
	}
	if manager.GetSessionConfig().Lifetime != session.DefaultLifetime {
		t.Fatalf("got %v", manager.GetSessionConfig().Lifetime)
	}

	blocking := session.NewSessionManager(session.Config{
		Driver:           "array",
		Block:            true,
		BlockStore:       "redis",
		BlockLockSeconds: time.Second,
		BlockWaitSeconds: 2 * time.Second,
	}, nil)
	if !blocking.ShouldBlock() || blocking.BlockDriver() != "redis" {
		t.Fatal("the blocking configuration was not read")
	}
	if blocking.DefaultRouteBlockLockSeconds() != time.Second {
		t.Fatalf("got %v", blocking.DefaultRouteBlockLockSeconds())
	}

	blocking.SetDefaultDriver("null")
	if blocking.GetDefaultDriver() != "null" {
		t.Fatalf("got %q", blocking.GetDefaultDriver())
	}
}

func TestTheFileDriverIsBuiltFromConfiguration(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "framework", "sessions")
	manager := session.NewSessionManager(session.Config{Driver: "file", Files: dir}, nil)

	s, err := manager.Driver("")
	if err != nil {
		t.Fatalf("driver: %v", err)
	}
	if _, ok := s.GetHandler().(*session.FileSessionHandler); !ok {
		t.Fatalf("got %T", s.GetHandler())
	}
	if !filesystem.NewFilesystem().IsDirectory(dir) {
		t.Fatal("the session directory was not made")
	}
}

// TestOnlyTakesTopLevelKeys: Store::only is Arr::only, which is
// array_intersect_key over the top level and reaches into nothing. This read
// each key in dot notation, so Only(["user.name"]) returned a key the PHP has no
// way of returning and the two disagreed on what a whitelist is. Except stays in
// dot notation, because Arr::except is Arr::forget and that one does walk.
func TestOnlyTakesTopLevelKeys(t *testing.T) {
	s := startedStore(t, session.NewArraySessionHandler(time.Hour))
	s.Put("user", map[string]any{"name": "Ana", "email": "ana@example.test"})

	if got := s.Only([]string{"user.name"}); len(got) != 0 {
		t.Errorf("Only reached inside a nested map: %v", got)
	}
	got := s.Only([]string{"user"})
	if len(got) != 1 {
		t.Fatalf("Only([\"user\"]) = %v, want the whole map under one key", got)
	}

	// Except is the other half, and it does walk.
	if except := s.Except([]string{"user.name"}); except["user"].(map[string]any)["name"] != nil {
		t.Errorf("Except stopped walking: %v", except)
	}
}

// TestIncrementCountsANumberWrittenAsText: PHP's + coerces first, so a key
// holding the string "5" counts as five and increment returns 6. This read it as
// zero and returned 1, which silently restarts a counter that has been through
// anything that stringifies -- a form value, a header, an older release.
func TestIncrementCountsANumberWrittenAsText(t *testing.T) {
	s := startedStore(t, session.NewArraySessionHandler(time.Hour))

	s.Put("views", "5")
	if got := s.Increment("views", 1); got != 6 {
		t.Errorf(`Increment over "5" = %d, want 6`, got)
	}
	s.Put("ratio", "5.7")
	if got := s.Increment("ratio", 1); got != 6 {
		t.Errorf(`Increment over "5.7" = %d, want 6`, got)
	}
	// Text that is not a number is still zero: PHP warns and takes 0, and an
	// error on a path whose whole purpose is not to have one is worse.
	s.Put("name", "Ana")
	if got := s.Increment("name", 1); got != 1 {
		t.Errorf(`Increment over "Ana" = %d, want 1`, got)
	}
}

// TestPushKeepsWhatWasAlreadyThere: PHP raises an Error for `$array[] =` over a
// scalar, so nothing is lost there. This started a fresh list and dropped the
// value that was in the key -- silent data loss, on the one call whose name says
// it only adds.
func TestPushKeepsWhatWasAlreadyThere(t *testing.T) {
	s := startedStore(t, session.NewArraySessionHandler(time.Hour))

	s.Put("cart", "apple")
	s.Push("cart", "pear")

	cart, ok := s.Get("cart").([]any)
	if !ok || len(cart) != 2 {
		t.Fatalf("cart = %v, want both values", s.Get("cart"))
	}
	if cart[0] != "apple" || cart[1] != "pear" {
		t.Errorf("cart = %v, want the old value first", cart)
	}
}

// TestTheFileHandlerCollectsTheWayTheFinderDoes: PHP builds the sweep with
// Finder::create()->in($path)->files()->ignoreDotFiles(true), which walks
// subdirectories and skips dot files. This did the opposite of both: it read one
// level and deleted anything it found, so a storage directory holding a
// .gitignore -- which is how the directory survives a clone at all -- lost it on
// the first sweep.
func TestTheFileHandlerCollectsTheWayTheFinderDoes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	handler, err := session.NewFileSessionHandler(nil, dir, time.Hour)
	if err != nil {
		t.Fatalf("file handler: %v", err)
	}
	ctx := context.Background()

	if err := handler.Write(ctx, strings.Repeat("h", 40), "payload"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*\n"), 0o600); err != nil {
		t.Fatalf("gitignore: %v", err)
	}
	nested := filepath.Join(dir, "shard")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, strings.Repeat("i", 40)), []byte("payload"), 0o600); err != nil {
		t.Fatalf("nested session: %v", err)
	}

	deleted, err := handler.GC(ctx, time.Nanosecond)
	if err != nil {
		t.Fatalf("gc: %v", err)
	}
	if deleted != 2 {
		t.Errorf("collected %d sessions, want the two under the directory", deleted)
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); err != nil {
		t.Errorf("the sweep deleted the dot file the Finder skips: %v", err)
	}
}

// TestTheCookieHandlerWritesTheJSONLaravelWrites: the wire shape was base64 of
// the JSON, so a cookie written by a Laravel application read back here as an
// empty session and a cookie written here read back empty there -- an
// interoperability break that only shows in production, on the day somebody puts
// the two behind one domain. json_encode is what the PHP queues, and
// rawurlencode is what Symfony's Cookie does to it on the way out; net/http
// sanitises instead of encoding, so the encoding is done here.
func TestTheCookieHandlerWritesTheJSONLaravelWrites(t *testing.T) {
	jar := &fakeJar{}
	handler := session.NewCookieSessionHandler(jar, time.Hour, false)
	ctx := context.Background()
	id := strings.Repeat("j", 40)

	if err := handler.Write(ctx, id, `{"subject":"1"}`); err != nil {
		t.Fatalf("write: %v", err)
	}
	decoded, err := url.QueryUnescape(jar.queued[id])
	if err != nil {
		t.Fatalf("the cookie is not percent-encoded: %v", err)
	}
	var payload struct {
		Data    string `json:"data"`
		Expires int64  `json:"expires"`
	}
	if err := json.Unmarshal([]byte(decoded), &payload); err != nil {
		t.Fatalf("the cookie does not hold the JSON Laravel writes: %v (%q)", err, decoded)
	}
	if payload.Data != `{"subject":"1"}` || payload.Expires == 0 {
		t.Fatalf("payload = %+v", payload)
	}
}

// TestTheCookieHandlerReadsWhatLaravelWrote is the other direction, and it is
// the one that matters: the cookie below was produced by PHP, and it used to
// read back as an empty session.
func TestTheCookieHandlerReadsWhatLaravelWrote(t *testing.T) {
	handler := session.NewCookieSessionHandler(&fakeJar{}, time.Hour, false)
	id := strings.Repeat("k", 40)

	laravel := `{"data":"a:1:{s:7:\"subject\";s:1:\"1\";}","expires":` +
		strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10) + `}`

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: id, Value: url.QueryEscape(laravel)})
	handler.SetRequest(r)

	got, err := handler.Read(context.Background(), id)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != `a:1:{s:7:"subject";s:1:"1";}` {
		t.Fatalf("got %q, want the payload the PHP wrote", got)
	}
}
