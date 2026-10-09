package console_test

import (
	"os"
	"testing"

	"github.com/arandu-io/hesape/console"
)

// TestBinaryIsTheRunningExecutable: a command line built to run this program
// again names the program, and a compiled program is its own interpreter.
func TestBinaryIsTheRunningExecutable(t *testing.T) {
	want, err := os.Executable()
	if err != nil {
		t.Skipf("os.Executable: %v", err)
	}
	if got := console.Binary(); got != want {
		t.Fatalf("Binary() = %q, want %q", got, want)
	}
}

// TestFormatCommandStringIsTheBinaryAndTheCommand: the line a scheduled
// command runs is the binary and the name, with nothing between them and no
// trailing space when there is no name.
func TestFormatCommandStringIsTheBinaryAndTheCommand(t *testing.T) {
	binary := console.Binary()

	if got, want := console.FormatCommandString("schedule:finish"), binary+" schedule:finish"; got != want {
		t.Fatalf("FormatCommandString(%q) = %q, want %q", "schedule:finish", got, want)
	}
	if got := console.FormatCommandString(""); got != binary {
		t.Fatalf("FormatCommandString(\"\") = %q, want the binary alone, %q", got, binary)
	}
	if got := console.FormatCommandString("  "); got != binary {
		t.Fatalf("FormatCommandString(\"  \") = %q, want the binary alone, %q", got, binary)
	}
}
