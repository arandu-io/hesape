package collections_test

import (
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/collections"
)

type user struct {
	ID    int
	Name  string
	Team  string
	Score float64
}

var team = []user{
	{ID: 1, Name: "ana", Team: "core", Score: 3.5},
	{ID: 2, Name: "bruno", Team: "view", Score: 1.25},
	{ID: 3, Name: "carla", Team: "core", Score: 5},
	{ID: 4, Name: "diego", Team: "view", Score: 0},
}

func TestMap(t *testing.T) {
	got := collections.Map(team, func(u user) string { return u.Name })
	want := []string{"ana", "bruno", "carla", "diego"}
	if !slices.Equal(got, want) {
		t.Errorf("Map = %v, want %v", got, want)
	}
}

func TestMapChangesElementType(t *testing.T) {
	got := collections.Map([]int{1, 2, 3}, strconv.Itoa)
	want := []string{"1", "2", "3"}
	if !slices.Equal(got, want) {
		t.Errorf("Map = %v, want %v", got, want)
	}
}

func TestMapOfEmptyIsNotNil(t *testing.T) {
	got := collections.Map(nil, func(u user) int { return u.ID })
	if got == nil {
		t.Fatal("Map of a nil slice returned nil, want an empty slice")
	}
	if len(got) != 0 {
		t.Errorf("Map of a nil slice has %d elements, want 0", len(got))
	}
}

func TestFilter(t *testing.T) {
	got := collections.Filter(team, func(u user) bool { return u.Team == "core" })
	want := []int{1, 3}
	if ids := collections.Map(got, func(u user) int { return u.ID }); !slices.Equal(ids, want) {
		t.Errorf("Filter kept %v, want %v", ids, want)
	}
}

func TestFilterKeepingNothingIsNotNil(t *testing.T) {
	got := collections.Filter(team, func(user) bool { return false })
	if got == nil {
		t.Fatal("Filter that kept nothing returned nil, want an empty slice")
	}
	if len(got) != 0 {
		t.Errorf("Filter that kept nothing has %d elements, want 0", len(got))
	}
}

func TestFilterDoesNotAliasItsInput(t *testing.T) {
	in := []int{1, 2, 3, 4}
	got := collections.Filter(in, func(n int) bool { return n%2 == 0 })
	got[0] = 99
	if !slices.Equal(in, []int{1, 2, 3, 4}) {
		t.Errorf("writing to the result changed the input: %v", in)
	}
}

func TestReduce(t *testing.T) {
	got := collections.Reduce(team, "", func(acc string, u user) string {
		if acc == "" {
			return u.Name
		}
		return acc + "," + u.Name
	})
	const want = "ana,bruno,carla,diego"
	if got != want {
		t.Errorf("Reduce = %q, want %q", got, want)
	}
}

func TestReduceFoldsLeftToRight(t *testing.T) {
	got := collections.Reduce([]string{"a", "b", "c"}, "", func(acc, s string) string {
		return acc + s
	})
	if got != "abc" {
		t.Errorf("Reduce = %q, want %q", got, "abc")
	}
}

func TestReduceOfEmptyReturnsInitial(t *testing.T) {
	got := collections.Reduce(nil, 42, func(acc int, u user) int { return acc + u.ID })
	if got != 42 {
		t.Errorf("Reduce of an empty slice = %d, want the initial value 42", got)
	}
}

func TestFirst(t *testing.T) {
	got, ok := collections.First(team, func(u user) bool { return u.Team == "view" })
	if !ok {
		t.Fatal("First reported no match, want bruno")
	}
	if got.Name != "bruno" {
		t.Errorf("First = %q, want %q", got.Name, "bruno")
	}
}

func TestFirstWithoutMatch(t *testing.T) {
	got, ok := collections.First(team, func(u user) bool { return u.Team == "queue" })
	if ok {
		t.Fatalf("First reported a match %q, want none", got.Name)
	}
	if got != (user{}) {
		t.Errorf("First without a match = %+v, want the zero value", got)
	}
}

func TestLast(t *testing.T) {
	got, ok := collections.Last(team, func(u user) bool { return u.Team == "view" })
	if !ok {
		t.Fatal("Last reported no match, want diego")
	}
	if got.Name != "diego" {
		t.Errorf("Last = %q, want %q", got.Name, "diego")
	}
}

func TestLastWithoutMatch(t *testing.T) {
	got, ok := collections.Last(nil, func(user) bool { return true })
	if ok {
		t.Fatalf("Last of an empty slice reported a match %+v, want none", got)
	}
}

func TestGroupBy(t *testing.T) {
	got := collections.GroupBy(team, func(u user) string { return u.Team })

	keys := slices.Sorted(maps.Keys(got))
	if want := []string{"core", "view"}; !slices.Equal(keys, want) {
		t.Fatalf("GroupBy keys = %v, want %v", keys, want)
	}

	core := collections.Map(got["core"], func(u user) int { return u.ID })
	if want := []int{1, 3}; !slices.Equal(core, want) {
		t.Errorf("group core = %v, want %v in input order", core, want)
	}
	view := collections.Map(got["view"], func(u user) int { return u.ID })
	if want := []int{2, 4}; !slices.Equal(view, want) {
		t.Errorf("group view = %v, want %v in input order", view, want)
	}
}

func TestGroupByOfEmptyIsNotNil(t *testing.T) {
	got := collections.GroupBy(nil, func(u user) string { return u.Team })
	if got == nil {
		t.Fatal("GroupBy of a nil slice returned nil, want an empty map")
	}
	if len(got) != 0 {
		t.Errorf("GroupBy of a nil slice has %d keys, want 0", len(got))
	}
}

func TestKeyBy(t *testing.T) {
	got := collections.KeyBy(team, func(u user) int { return u.ID })
	if len(got) != 4 {
		t.Fatalf("KeyBy produced %d entries, want 4", len(got))
	}
	if got[3].Name != "carla" {
		t.Errorf("KeyBy[3] = %q, want %q", got[3].Name, "carla")
	}
}

func TestKeyByKeepsTheLastOfADuplicate(t *testing.T) {
	got := collections.KeyBy(team, func(u user) string { return u.Team })
	if got["core"].Name != "carla" {
		t.Errorf("KeyBy[core] = %q, want the last element %q", got["core"].Name, "carla")
	}
	if got["view"].Name != "diego" {
		t.Errorf("KeyBy[view] = %q, want the last element %q", got["view"].Name, "diego")
	}
}

func TestPartition(t *testing.T) {
	matched, rest := collections.Partition(team, func(u user) bool { return u.Score >= 3 })

	got := collections.Map(matched, func(u user) string { return u.Name })
	if want := []string{"ana", "carla"}; !slices.Equal(got, want) {
		t.Errorf("matched = %v, want %v", got, want)
	}
	got = collections.Map(rest, func(u user) string { return u.Name })
	if want := []string{"bruno", "diego"}; !slices.Equal(got, want) {
		t.Errorf("rest = %v, want %v", got, want)
	}
}

func TestPartitionHalvesAreNotNil(t *testing.T) {
	matched, rest := collections.Partition([]int{1, 3, 5}, func(n int) bool { return n%2 == 1 })
	if matched == nil || rest == nil {
		t.Fatalf("Partition returned nil halves: matched=%v rest=%v", matched, rest)
	}
	if len(rest) != 0 {
		t.Errorf("rest = %v, want it empty", rest)
	}
}

func TestUniqueBy(t *testing.T) {
	got := collections.UniqueBy(team, func(u user) string { return u.Team })
	names := collections.Map(got, func(u user) string { return u.Name })
	if want := []string{"ana", "bruno"}; !slices.Equal(names, want) {
		t.Errorf("UniqueBy kept %v, want the first of each key %v", names, want)
	}
}

func TestUniqueByPreservesInputOrder(t *testing.T) {
	in := []string{"Delta", "alpha", "DELTA", "Beta", "beta"}
	got := collections.UniqueBy(in, strings.ToLower)
	want := []string{"Delta", "alpha", "Beta"}
	if !slices.Equal(got, want) {
		t.Errorf("UniqueBy = %v, want %v", got, want)
	}
}

func TestUniqueByOfEmptyIsNotNil(t *testing.T) {
	got := collections.UniqueBy(nil, func(u user) int { return u.ID })
	if got == nil {
		t.Fatal("UniqueBy of a nil slice returned nil, want an empty slice")
	}
}

func TestSum(t *testing.T) {
	if got := collections.Sum(team, func(u user) int { return u.ID }); got != 10 {
		t.Errorf("Sum of ids = %d, want 10", got)
	}
	if got := collections.Sum(team, func(u user) float64 { return u.Score }); got != 9.75 {
		t.Errorf("Sum of scores = %v, want 9.75", got)
	}
}

func TestSumOfEmptyIsZero(t *testing.T) {
	if got := collections.Sum(nil, func(u user) int { return u.ID }); got != 0 {
		t.Errorf("Sum of an empty slice = %d, want 0", got)
	}
}

func TestSumOverANamedNumericType(t *testing.T) {
	type cents int64
	in := []cents{199, 250, 1}
	if got := collections.Sum(in, func(c cents) cents { return c }); got != 450 {
		t.Errorf("Sum = %d, want 450", got)
	}
}
