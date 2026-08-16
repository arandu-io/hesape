package console

import (
	"io"
	"strings"
	"sync"
)

// BufferedConsoleOutput keeps everything written to it and passes it on.
//
// The point is both halves at once: a command run from inside another one has
// to reach the terminal -- otherwise the person watching sees a silent pause
// -- and the caller has to be able to read what it said, which is what
// [Application.Output] returns.
//
// It is safe for concurrent use, because a command that runs work in parallel
// writes from more than one goroutine and a torn buffer is worse than a slow
// one.
type BufferedConsoleOutput struct {
	mu     sync.Mutex
	buffer strings.Builder
	out    io.Writer
}

// NewBufferedConsoleOutput returns the output.
//
// out is where the bytes go on their way past, and nil means nowhere: that is
// the buffer-only form a test uses.
func NewBufferedConsoleOutput(out io.Writer) *BufferedConsoleOutput {
	return &BufferedConsoleOutput{out: out}
}

// Write buffers the bytes and passes them on.
//
// It is an io.Writer so an IO can be pointed straight at it.
func (b *BufferedConsoleOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	b.buffer.Write(p)
	b.mu.Unlock()

	if b.out == nil {
		return len(p), nil
	}
	return b.out.Write(p)
}

// Fetch empties the buffer and returns what was in it.
//
// A second Fetch with nothing written between them returns empty, which is
// what lets one buffer serve a sequence of commands without the second
// reading the first's output.
func (b *BufferedConsoleOutput) Fetch() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	out := b.buffer.String()
	b.buffer.Reset()
	return out
}
