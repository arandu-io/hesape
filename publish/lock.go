package publish

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Lock is what an earlier publication wrote down about the files it wrote.
//
// It exists for one question, and it is the question the whole mechanism is
// judged by: was this file changed since we wrote it? Without a record, a
// publication can only tell that a file exists, and "it exists" cannot separate
// the file we wrote last week from the one somebody rewrote yesterday -- so it
// either overwrites the second or refuses the first.
//
// What it stores is the digest of the file with its custom blocks emptied, so
// that editing inside the markers is not a change and editing outside them is.
type Lock struct {
	// Files maps a slash-separated path, relative to the project root, to what
	// was written there.
	Files map[string]Entry `json:"files"`
}

// Entry is one published file.
type Entry struct {
	// Origin is an opaque label the publisher chose, carried so a person
	// reading the lock can tell where a file came from. Nothing here reads it.
	Origin string `json:"origin,omitempty"`
	// Digest is the hex sha256 of the file as published, with the body of every
	// custom block emptied.
	Digest string `json:"digest"`
}

// NewLock returns an empty lock: nothing has been published yet.
func NewLock() *Lock { return &Lock{Files: map[string]Entry{}} }

// ReadLock reads the lock at path. A file that is not there is an empty lock
// and not an error: the first publication has nothing to have written.
func ReadLock(path string) (*Lock, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return NewLock(), nil
	}
	if err != nil {
		return nil, err
	}

	lock := NewLock()
	if err := json.Unmarshal(content, lock); err != nil {
		return nil, err
	}
	if lock.Files == nil {
		lock.Files = map[string]Entry{}
	}
	return lock, nil
}

// Write writes the lock to path, creating the directory above it.
//
// The encoding sorts its keys, so the file a second publication writes differs
// from the first only where something was published -- a lock that reordered
// itself would show up as a change in every review.
func (l *Lock) Write(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(content, '\n'), 0o644)
}

// Paths lists the published files, in order.
func (l *Lock) Paths() []string {
	out := make([]string, 0, len(l.Files))
	for path := range l.Files {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

// digest is the hex sha256 of a file's canonical form: the content with the
// body of every custom block emptied.
func digest(path string, content []byte) string {
	sum := sha256.Sum256(canonical(path, content))
	return hex.EncodeToString(sum[:])
}
