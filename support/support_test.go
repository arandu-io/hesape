package support

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestOddsRefusesChancesAboveOneWithNothingToBeOutOf(t *testing.T) {
	if _, err := Odds(2); !errors.Is(err, ErrFloatGreaterThanOne) {
		t.Fatalf("got %v, want ErrFloatGreaterThanOne", err)
	}
	if _, err := Odds(2, 10); err != nil {
		t.Fatalf("two out of ten is not the same question: %v", err)
	}
}

func TestAlwaysWinAndAlwaysLoseRunTheOneCallback(t *testing.T) {
	defer DetermineResultNormally()

	lottery, err := Odds(0.0001)
	if err != nil {
		t.Fatal(err)
	}
	lottery.Winner(func() any { return "won" }).Loser(func() any { return "lost" })

	AlwaysWin()
	if got := lottery.Choose(); got != "won" {
		t.Fatalf("got %v, want won", got)
	}

	AlwaysLose()
	if got := lottery.Choose(); got != "lost" {
		t.Fatalf("got %v, want lost", got)
	}
}

func TestALotteryWithNoCallbackAnswersTrueOrFalse(t *testing.T) {
	defer DetermineResultNormally()

	lottery, err := Odds(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	AlwaysWin()
	if got := lottery.Choose(); got != true {
		t.Fatalf("got %v, want true", got)
	}
	AlwaysLose()
	if got := lottery.Choose(); got != false {
		t.Fatalf("got %v, want false", got)
	}
}

func TestAlwaysWinPutsTheLotteryBackAfterTheCallback(t *testing.T) {
	defer DetermineResultNormally()

	lottery, _ := Odds(0, 10)
	inside := false
	AlwaysWin(func() { inside = lottery.Choose() == true })

	if !inside {
		t.Fatal("the lottery did not win inside the callback")
	}
	if got := lottery.Choose(); got != false {
		t.Fatalf("zero out of ten wins after the callback: %v", got)
	}
}

func TestFixWalksTheSequenceAndThenDrawsNormally(t *testing.T) {
	defer DetermineResultNormally()

	lottery, _ := Odds(0, 10)
	Fix([]bool{true, true, false})

	got := lottery.Choose(4)
	results, ok := got.([]any)
	if !ok || len(results) != 4 {
		t.Fatalf("got %v", got)
	}
	if results[0] != true || results[1] != true || results[2] != false {
		t.Fatalf("the sequence was not walked in order: %v", results)
	}
	if results[3] != false {
		t.Fatalf("zero out of ten cannot win once the sequence is out: %v", results[3])
	}
}

func TestChooseWithNoCountRunsTheLotteryOnce(t *testing.T) {
	defer DetermineResultNormally()

	lottery, _ := Odds(1, 1)
	AlwaysWin()

	if _, isList := lottery.Choose().([]any); isList {
		t.Fatal("choose with no count is one result, not a list")
	}
	if results, isList := lottery.Choose(0).([]any); !isList || len(results) != 0 {
		t.Fatal("choose of zero is an empty list")
	}
}

func TestOnceRunsTheCallbackOncePerCallSite(t *testing.T) {
	Flush()
	Enable()
	defer Flush()

	calls := 0
	counted := func() int {
		return Once(func() int {
			calls++
			return calls
		})
	}

	if got := counted(); got != 1 {
		t.Fatalf("got %d, want 1", got)
	}
	if got := counted(); got != 1 {
		t.Fatalf("the second call ran the callback again: %d", got)
	}
	if calls != 1 {
		t.Fatalf("the callback ran %d times", calls)
	}
}

func TestTwoCallSitesAreTwoValues(t *testing.T) {
	Flush()
	Enable()
	defer Flush()

	first := Once(func() string { return "first" })
	second := Once(func() string { return "second" })

	if first != "first" || second != "second" {
		t.Fatalf("got %q and %q", first, second)
	}
}

func TestDisableRunsTheCallbackEveryTime(t *testing.T) {
	Flush()
	Disable()
	defer func() {
		Enable()
		Flush()
	}()

	calls := 0
	counted := func() int {
		return Once(func() int {
			calls++
			return calls
		})
	}

	_, _ = counted(), counted()
	if calls != 2 {
		t.Fatalf("the callback ran %d times, want 2", calls)
	}
}

func TestValidatedInputReadsAndMergesWithoutTouchingTheOriginal(t *testing.T) {
	input := NewValidatedInput(map[string]any{
		"name":    "Ana",
		"address": map[string]any{"city": "Recife"},
	})

	if got := input.Input("address.city"); got != "Recife" {
		t.Fatalf("got %v, want Recife", got)
	}
	if got := input.Input("address.zip", "none"); got != "none" {
		t.Fatalf("a missing key falls back to the default, got %v", got)
	}
	if got := input.String("name"); got != "Ana" {
		t.Fatalf("got %q", got)
	}

	merged := input.Merge(map[string]any{"name": "Bia"})
	if got := merged.Input("name"); got != "Bia" {
		t.Fatalf("got %v, want Bia", got)
	}
	if got := input.Input("name"); got != "Ana" {
		t.Fatalf("merge wrote into the original: %v", got)
	}
}

func TestValidatedInputKeysAndOnlyAndExcept(t *testing.T) {
	input := NewValidatedInput(map[string]any{"a": 1, "b": 2, "c": 3})

	if got := input.Keys(); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("got %v", got)
	}
	if got := input.Only("a", "z"); !reflect.DeepEqual(got, map[string]any{"a": 1}) {
		t.Fatalf("a key that is not there is left out, got %v", got)
	}
	if got := input.Except("a"); !reflect.DeepEqual(got, map[string]any{"b": 2, "c": 3}) {
		t.Fatalf("got %v", got)
	}
	if !input.Has("a", "b") || input.Has("a", "z") {
		t.Fatal("Has wants every key")
	}
	if !input.Missing("z") {
		t.Fatal("Missing is the other side of Has")
	}
}

func TestValidatedInputAllWithKeysReadsByDottedKey(t *testing.T) {
	input := NewValidatedInput(map[string]any{
		"user": map[string]any{"name": "Ana", "age": 30},
	})

	got := input.All("user.name")
	want := map[string]any{"user": map[string]any{"name": "Ana"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBenchmarkMeasuresEveryCallbackAndValueGivesBackWhatItReturned(t *testing.T) {
	measured := Benchmark.Measure([]func(){func() {}, func() {}}, 2)
	if len(measured) != 2 {
		t.Fatalf("got %d results, want 2", len(measured))
	}
	for _, average := range measured {
		if average < 0 {
			t.Fatalf("a run cannot take less than nothing: %v", average)
		}
	}

	got, milliseconds := Value(func() string { return "done" })
	if got != "done" || milliseconds < 0 {
		t.Fatalf("got %q in %v", got, milliseconds)
	}
}

func TestEscapeArgumentQuotesTheWholeThingAndTheQuotesInside(t *testing.T) {
	if Windows_os() {
		t.Skip("the Windows branch is the other half of the method")
	}
	if got := ProcessUtils.EscapeArgument("ls"); got != "'ls'" {
		t.Fatalf("got %s", got)
	}
	if got := ProcessUtils.EscapeArgument(""); got != "''" {
		t.Fatalf("an empty argument is still an argument, got %s", got)
	}
	if got := ProcessUtils.EscapeArgument("it's"); got != `'it'\''s'` {
		t.Fatalf("got %s", got)
	}
	if got := ProcessUtils.EscapeArgument("a b; rm -rf /"); got != "'a b; rm -rf /'" {
		t.Fatalf("got %s", got)
	}
}

func TestParseKeyReadsTheThreeParts(t *testing.T) {
	resolver := NewNamespacedItemResolver()

	namespace, group, item := resolver.ParseKey("app.timezone")
	if namespace != "" || group != "app" || item != "timezone" {
		t.Fatalf("got %q %q %q", namespace, group, item)
	}

	namespace, group, item = resolver.ParseKey("app")
	if namespace != "" || group != "app" || item != "" {
		t.Fatalf("a whole group has no item: %q %q %q", namespace, group, item)
	}

	namespace, group, item = resolver.ParseKey("cart::config.options.size")
	if namespace != "cart" || group != "config" || item != "options.size" {
		t.Fatalf("got %q %q %q", namespace, group, item)
	}
}

func TestParseKeyAnswersFromTheCacheAndSetParsedKeyWritesIntoIt(t *testing.T) {
	resolver := NewNamespacedItemResolver()
	resolver.SetParsedKey("app.timezone", "ns", "group", "item")

	namespace, group, item := resolver.ParseKey("app.timezone")
	if namespace != "ns" || group != "group" || item != "item" {
		t.Fatalf("got %q %q %q", namespace, group, item)
	}

	resolver.FlushParsedKeys()
	namespace, group, item = resolver.ParseKey("app.timezone")
	if namespace != "" || group != "app" || item != "timezone" {
		t.Fatalf("the cache was not flushed: %q %q %q", namespace, group, item)
	}
}

func TestParseConfigurationSpreadsTheUrlOverTheOptions(t *testing.T) {
	parser := NewConfigurationUrlParser()

	got, err := parser.ParseConfiguration("postgres://ana:secret@db.test:5432/shop?sslmode=require")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"driver":   "pgsql",
		"database": "shop",
		"host":     "db.test",
		"port":     5432,
		"username": "ana",
		"password": "secret",
		"sslmode":  "require",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParseConfigurationLeavesAConfigurationWithNoUrlAlone(t *testing.T) {
	parser := NewConfigurationUrlParser()

	got, err := parser.ParseConfiguration(map[string]any{"driver": "sqlite"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, map[string]any{"driver": "sqlite"}) {
		t.Fatalf("got %v", got)
	}
}

func TestParseConfigurationReadsTheSqliteTripleSlashAndTheNativeTypes(t *testing.T) {
	parser := NewConfigurationUrlParser()

	got, err := parser.ParseConfiguration("sqlite:///database.sqlite?foreign_key_constraints=true")
	if err != nil {
		t.Fatal(err)
	}
	if got["driver"] != "sqlite" || got["database"] != "database.sqlite" {
		t.Fatalf("got %v", got)
	}
	if got["foreign_key_constraints"] != true {
		t.Fatalf("a string that is a bool comes back as one, got %#v", got["foreign_key_constraints"])
	}
}

func TestDriverAliasesCanBeReadAndAddedTo(t *testing.T) {
	AddDriverAlias("pg", "pgsql")
	aliases := GetDriverAliases()
	if aliases["pg"] != "pgsql" || aliases["postgres"] != "pgsql" {
		t.Fatalf("got %v", aliases)
	}

	aliases["pg"] = "written over"
	if GetDriverAliases()["pg"] != "pgsql" {
		t.Fatal("the caller was handed the map itself")
	}
}

func TestBlankAndFilled(t *testing.T) {
	blank := []any{nil, "", "   ", []any{}, map[string]any{}}
	for _, v := range blank {
		if !Blank(v) {
			t.Fatalf("%#v is blank", v)
		}
		if Filled(v) {
			t.Fatalf("%#v is not filled", v)
		}
	}
	filled := []any{"a", 0, false, true, []any{nil}}
	for _, v := range filled {
		if Blank(v) {
			t.Fatalf("%#v is not blank", v)
		}
	}
}

func TestEEscapesEverythingThatCanCloseAnAttribute(t *testing.T) {
	got := E(`<a href="x" onclick='y'>&</a>`)
	want := `&lt;a href=&quot;x&quot; onclick=&#039;y&#039;&gt;&amp;&lt;/a&gt;`
	if got != want {
		t.Fatalf("got %s", got)
	}
	if got := E(nil); got != "" {
		t.Fatalf("nothing escapes to nothing, got %q", got)
	}
	if got := E(NewHtmlString("<b>ok</b>")); got != "<b>ok</b>" {
		t.Fatalf("an Htmlable says its own HTML, got %s", got)
	}
	if got := E("&amp; &", false); got != "&amp; &amp;" {
		t.Fatalf("without double encoding an entity is left alone, got %s", got)
	}
}

func TestClassBasenameDropsThePackageAndReadsThroughAPointer(t *testing.T) {
	if got := Class_basename(`App\Models\User`); got != "User" {
		t.Fatalf("got %s", got)
	}
	if got := Class_basename(NewHtmlString("")); got != "HtmlString" {
		t.Fatalf("got %s", got)
	}
	if got := Class_basename(nil); got != "" {
		t.Fatalf("got %s", got)
	}
}

func TestObjectGetWalksTheDottedKey(t *testing.T) {
	object := map[string]any{"user": map[string]any{"name": "Ana"}}

	if got := Object_get(object, "user.name"); got != "Ana" {
		t.Fatalf("got %v", got)
	}
	if got := Object_get(object, "user.age", 30); got != 30 {
		t.Fatalf("a missing name is the default, got %v", got)
	}
	if got := Object_get(object, ""); !reflect.DeepEqual(got, object) {
		t.Fatalf("an empty key is the object itself, got %v", got)
	}
	if got := Object_get(object, "   "); !reflect.DeepEqual(got, object) {
		t.Fatalf("a key of spaces is the same as none, got %v", got)
	}
}

func TestPregReplaceArrayWalksTheReplacementsInOrder(t *testing.T) {
	got := Preg_replace_array(`\?`, []string{"8:30", "9:00"}, "The event will take place between ? and ?")
	if got != "The event will take place between 8:30 and 9:00" {
		t.Fatalf("got %s", got)
	}
	if got := Preg_replace_array(`\?`, []string{"only"}, "? and ?"); got != "only and " {
		t.Fatalf("a match past the end of the list is dropped, got %q", got)
	}
}

func TestRetryRunsAgainUntilItWorksOrRunsOut(t *testing.T) {
	Fake()
	defer Fake(false)

	attempts := 0
	got, err := Retry(3, func(attempt int) (any, error) {
		attempts++
		if attempt < 3 {
			return nil, errors.New("not yet")
		}
		return "done", nil
	}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got != "done" || attempts != 3 {
		t.Fatalf("got %v after %d attempts", got, attempts)
	}
}

func TestRetryGivesBackTheLastErrorWhenItNeverWorks(t *testing.T) {
	Fake()
	defer Fake(false)

	boom := errors.New("boom")
	attempts := 0
	_, err := Retry(2, func(int) (any, error) {
		attempts++
		return nil, boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want boom", err)
	}
	if attempts != 2 {
		t.Fatalf("it tried %d times, want 2", attempts)
	}
}

func TestRetryStopsWhenTheTestSaysTheErrorIsNotWorthAnotherTry(t *testing.T) {
	Fake()
	defer Fake(false)

	attempts := 0
	_, _ = Retry(5, func(int) (any, error) {
		attempts++
		return nil, errors.New("fatal")
	}, 0, func(err error) bool { return !strings.Contains(err.Error(), "fatal") })

	if attempts != 1 {
		t.Fatalf("it tried %d times, want 1", attempts)
	}
}

func TestRetryTakesAListOfBackoffsAsTheNumberOfTries(t *testing.T) {
	Fake()
	defer Fake(false)

	attempts := 0
	_, _ = Retry([]int{1, 2}, func(int) (any, error) {
		attempts++
		return nil, errors.New("no")
	})

	if attempts != 3 {
		t.Fatalf("two backoffs are three tries, got %d", attempts)
	}
	AssertSleptTimes(t, 2)
}

func TestTapHandsTheValueToTheCallbackAndThenBack(t *testing.T) {
	seen := ""
	got := Tap("value", func(v string) { seen = v })

	if got != "value" || seen != "value" {
		t.Fatalf("got %q, seen %q", got, seen)
	}
	if got := Tap("value", nil); got != "value" {
		t.Fatalf("got %q", got)
	}
}

func TestThrowIfAndThrowUnless(t *testing.T) {
	boom := errors.New("boom")

	if err := Throw_if(true, boom); !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
	if err := Throw_if(false, boom); err != nil {
		t.Fatalf("got %v", err)
	}
	if err := Throw_unless(false, boom); !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
	if err := Throw_unless(true, boom); err != nil {
		t.Fatalf("got %v", err)
	}
}

func TestTransformRunsOnlyOnAFilledValue(t *testing.T) {
	if got := Transform("5", func(s string) int { return len(s) }); got != 1 {
		t.Fatalf("got %d", got)
	}
	if got := Transform("", func(s string) int { return len(s) }, 42); got != 42 {
		t.Fatalf("a blank value is the default, got %d", got)
	}
	if got := Transform("", func(s string) int { return len(s) }); got != 0 {
		t.Fatalf("with no default it is the zero value, got %d", got)
	}
}

func TestWithPassesTheValueThroughTheCallback(t *testing.T) {
	if got := With(5, func(n int) string { return strings.Repeat("a", n) }); got != "aaaaa" {
		t.Fatalf("got %q", got)
	}
}

func TestAppendConfigPushesNumericKeysPastTheRest(t *testing.T) {
	got := Append_config(map[string]any{"0": "first", "1": "second", "name": "kept"})
	want := map[string]any{"10000": "first", "10001": "second", "name": "kept"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestWithLocaleRunsUnderTheLocaleAndPutsTheOldOneBack(t *testing.T) {
	app := &localeHolder{locale: "en"}

	got := WithLocale(app, "pt_BR", func() any { return app.GetLocale() })

	if got != "pt_BR" {
		t.Fatalf("got %v", got)
	}
	if app.GetLocale() != "en" {
		t.Fatalf("the old locale was not put back: %s", app.GetLocale())
	}
	if got := WithLocale(app, "", func() any { return app.GetLocale() }); got != "en" {
		t.Fatalf("no locale runs the callback as it is, got %v", got)
	}
}

type localeHolder struct{ locale string }

func (l *localeHolder) GetLocale() string       { return l.locale }
func (l *localeHolder) SetLocale(locale string) { l.locale = locale }

func TestForwardCallToCallsTheMethodAndSaysSoWhenThereIsNone(t *testing.T) {
	bag := NewMessageBag(map[string][]string{"name": {"required"}})

	returned, err := ForwardCallTo(bag, "First", "name")
	if err != nil {
		t.Fatal(err)
	}
	if len(returned) != 1 || returned[0] != "required" {
		t.Fatalf("got %v", returned)
	}

	_, err = ForwardCallTo(bag, "Whatever")
	var bad *ErrBadMethodCall
	if !errors.As(err, &bad) {
		t.Fatalf("got %v, want ErrBadMethodCall", err)
	}
	if bad.Method != "Whatever" || bad.Class != "MessageBag" {
		t.Fatalf("got %s::%s", bad.Class, bad.Method)
	}
}

func TestDeferPutsTheCallbackOnTheCollection(t *testing.T) {
	collection := DeferredCallbackCollection()
	for collection.Count() > 0 {
		collection.Invoke()
	}

	ran := false
	deferred := Defer(func() { ran = true }, "welcome")

	if deferred.GetName() != "welcome" {
		t.Fatalf("got %q", deferred.GetName())
	}
	if collection.Count() != 1 {
		t.Fatalf("got %d callbacks", collection.Count())
	}

	collection.Invoke()
	if !ran {
		t.Fatal("the deferred callback never ran")
	}
	if collection.Count() != 0 {
		t.Fatal("the collection was not emptied")
	}
}

func TestDumpHandsBackTheFirstValue(t *testing.T) {
	if got := Dump("first", "second"); got != "first" {
		t.Fatalf("got %v", got)
	}
	if got := Dump(); got != nil {
		t.Fatalf("got %v", got)
	}
}

func TestDdEndsTheProcess(t *testing.T) {
	original := exit
	status := 0
	exit = func(code int) { status = code }
	defer func() { exit = original }()

	Dd("gone")
	if status != 1 {
		t.Fatalf("got status %d, want 1", status)
	}

	NewValidatedInput(map[string]any{"a": 1}).Dd()
	if status != 1 {
		t.Fatalf("got status %d, want 1", status)
	}
}

func TestCreateFromIdReadsTheInstantOutOfAnOrderedIdentifier(t *testing.T) {
	// 2026-08-07 12:00:00 UTC, which is 1786104000000 milliseconds.
	want := int64(1786104000000)

	got, err := CreateFromId("01KZE1GBG0ABCDEFGHJKMNPQRS")
	if err != nil {
		t.Fatal(err)
	}
	if got.UnixMilli() != want {
		t.Fatalf("the ULID gave %d, want %d", got.UnixMilli(), want)
	}

	got, err = CreateFromId("019fdc18-2e00-7000-8000-000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if got.UnixMilli() != want {
		t.Fatalf("the ordered UUID gave %d, want %d", got.UnixMilli(), want)
	}

	if _, err := CreateFromId("not an id"); !errors.Is(err, ErrNotAnOrderedID) {
		t.Fatalf("got %v, want ErrNotAnOrderedID", err)
	}
	if _, err := CreateFromId(""); !errors.Is(err, ErrNotAnOrderedID) {
		t.Fatalf("got %v, want ErrNotAnOrderedID", err)
	}
}
