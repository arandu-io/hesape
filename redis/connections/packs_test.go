package connections_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/arandu-io/hesape/redis/connections"
)

// TestTheFormerNameIsTheSameType: code written against the old name has to
// keep compiling until the name is removed, both where it names the type and
// where it names the field a Connection embeds. An alias guarantees that; a
// second defined type would not, because the two would stop being assignable
// to each other, and this file would stop compiling.
func TestTheFormerNameIsTheSameType(t *testing.T) {
	var former connections.PacksPhpRedisValues
	var current connections.PacksValues = former

	conn := connections.Connection{PacksPhpRedisValues: current}
	if got := conn.Pack([]string{"a", "b"}); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("Pack = %v, want the values as given: nothing encodes them", got)
	}
	if conn.Serialized() || conn.Compressed() || conn.LzfCompressed() || conn.ZstdCompressed() || conn.Lz4Compressed() {
		t.Error("every encoding question must answer no: the driver has no serializer and no compression")
	}
}

// TestWithoutSerializationOrCompressionRunsTheCallbackOnce, and hands back
// what it returned: both are always off, so wrapping a read in it must change
// nothing about the read.
func TestWithoutSerializationOrCompressionRunsTheCallbackOnce(t *testing.T) {
	want := errors.New("the read failed")
	calls := 0
	err := connections.PacksValues{}.WithoutSerializationOrCompression(func() error {
		calls++
		return want
	})
	if calls != 1 {
		t.Errorf("the callback ran %d times, want 1", calls)
	}
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want the callback's own error", err)
	}
}
