// Package publish writes a module's files into a project and can be run again
// without eating what the project did to them.
//
// It is deliberately ignorant of what it is publishing. It takes a tree, a root
// to write it under and a lock recording what an earlier run wrote, and it
// knows nothing about views, configuration or migrations -- the caller does,
// and the caller is the one place where that vocabulary belongs.
//
// # The three guarantees
//
// Publishing twice does not change a file. What Plan reports as unchanged is
// unchanged: the same tree over the same project produces no write at all, so
// running the command because you are not sure whether you ran it is free.
//
// What would happen is reported before it happens. Plan touches nothing; Apply
// writes what Plan returned. A publication nobody could look at first is one
// people run in a branch and read afterwards.
//
// A customization is never overwritten in silence. Whatever is written between
// the arandu:begin custom markers is carried into the new file, and a file
// changed outside them is reported as a conflict rather than replaced -- the
// lock is what makes the second half possible, because "the file is there" does
// not distinguish the file we wrote from the one somebody rewrote.
package publish

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Source is a tree to publish and where it lands.
type Source struct {
	// Files is the tree, as the module embedded it.
	Files fs.FS
	// From is the directory inside Files to publish. Empty means all of it.
	From string
	// To is the directory the files land in, relative to the project root.
	// Empty means the root itself.
	To string
	// Origin is an opaque label recorded in the lock, so a person reading it
	// can tell which module and which kind of file a path came from. Nothing
	// here reads it.
	Origin string
}

// Action is what publishing one file would do.
type Action uint8

const (
	// Create writes a file that is not there.
	Create Action = iota + 1
	// Update rewrites a file that is there and is as it was published, custom
	// blocks carried forward.
	Update
	// Unchanged is a file that is already what publishing would write.
	Unchanged
	// Conflict is a file that was changed outside its custom blocks, or that
	// was never published by this mechanism at all. It is reported and left
	// alone.
	Conflict
)

// String is the word a preview prints.
func (a Action) String() string {
	switch a {
	case Create:
		return "create"
	case Update:
		return "update"
	case Unchanged:
		return "unchanged"
	case Conflict:
		return "conflict"
	}
	return "unknown"
}

// Change is what publishing one file would do, and what it would write.
type Change struct {
	// Path is where the file goes, slash-separated and relative to the root.
	Path string
	// Action is what would happen to it.
	Action Action
	// Origin is the label of the source it came from.
	Origin string

	content  []byte
	existing []byte
}

// Content is what would be written: the published file with the custom blocks
// of the file on disk carried into it.
func (c Change) Content() []byte { return bytes.Clone(c.content) }

// Existing is what is on disk now, and nil when nothing is. It is here so a
// preview can show the difference without reading the file a second time --
// and so that the difference it shows is the one Apply would make.
func (c Change) Existing() []byte { return bytes.Clone(c.existing) }

// Options are the ways a publication may be told to behave.
type Options struct {
	// Force publishes over a file that was changed outside its custom blocks.
	//
	// It is not a way to discard those blocks: the merge runs either way, so
	// what is inside the markers survives a forced publication too. What Force
	// gives up is the edit made outside them, which is the edit this mechanism
	// cannot carry forward.
	Force bool
}

// Plan reports what publishing the sources under root would do, and touches
// nothing.
//
// The changes come back ordered by path, so two runs over the same tree read
// the same way and a preview can be compared against the last one.
func Plan(root string, lock *Lock, opts Options, sources ...Source) ([]Change, error) {
	if lock == nil {
		lock = NewLock()
	}

	changes := map[string]Change{}
	for _, source := range sources {
		if source.Files == nil {
			return nil, fmt.Errorf("publish: source %q has no files", source.Origin)
		}
		if err := planSource(root, lock, opts, source, changes); err != nil {
			return nil, err
		}
	}

	out := make([]Change, 0, len(changes))
	for _, change := range changes {
		out = append(out, change)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func planSource(root string, lock *Lock, opts Options, source Source, changes map[string]Change) error {
	from := source.From
	if from == "" {
		from = "."
	}
	to, err := destination(source.To)
	if err != nil {
		return err
	}

	return fs.WalkDir(source.Files, from, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}

		relative, err := filepath.Rel(filepath.FromSlash(from), filepath.FromSlash(name))
		if err != nil {
			return err
		}
		target := path.Join(to, filepath.ToSlash(relative))

		if taken, clash := changes[target]; clash {
			return fmt.Errorf("publish: %s is published by both %q and %q", target, taken.Origin, source.Origin)
		}

		content, err := fs.ReadFile(source.Files, name)
		if err != nil {
			return err
		}

		change, err := plan(root, lock, opts, source, target, content)
		if err != nil {
			return err
		}
		changes[target] = change
		return nil
	})
}

// plan decides what one file's publication would do.
func plan(root string, lock *Lock, opts Options, source Source, target string, content []byte) (Change, error) {
	change := Change{Path: target, Origin: source.Origin, content: content}

	existing, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(target)))
	if errors.Is(err, fs.ErrNotExist) {
		change.Action = Create
		return change, nil
	}
	if err != nil {
		return Change{}, err
	}
	change.existing = existing

	// The merge runs before the decision, and on every path that writes,
	// including a forced one. A forced publication that skipped it would be the
	// command that eats the block it promised to keep.
	change.content = Merge(target, existing, content)

	entry, published := lock.Files[target]
	switch {
	case !published:
		// Nothing published this. Whatever it is, it is somebody's file, and
		// this mechanism has no record of ever having written it.
		change.Action = Conflict
	case entry.Digest != digest(target, existing):
		// Published, and changed since -- outside the custom blocks, because
		// the digest is taken with their bodies emptied.
		change.Action = Conflict
	case bytes.Equal(change.content, existing):
		change.Action = Unchanged
	default:
		change.Action = Update
	}

	if change.Action == Conflict && opts.Force {
		change.Action = Update
	}
	return change, nil
}

// Apply writes the changes that write, records them in the lock and returns
// them.
//
// A conflict is skipped, which is the guarantee it exists for. Writing the lock
// back out is the caller's, because where it is kept is the caller's too.
func Apply(root string, lock *Lock, changes []Change) ([]Change, error) {
	if lock == nil {
		return nil, errors.New("publish: applying without a lock would forget what was written")
	}

	var applied []Change
	for _, change := range changes {
		switch change.Action {
		case Conflict:
			continue
		case Unchanged:
			// Nothing is written, and the lock is refreshed anyway: the entry
			// says what is there, and it already says it.
			lock.Files[change.Path] = Entry{Origin: change.Origin, Digest: digest(change.Path, change.content)}
			continue
		}

		full := filepath.Join(root, filepath.FromSlash(change.Path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return applied, err
		}
		if err := os.WriteFile(full, change.content, 0o644); err != nil {
			return applied, err
		}
		lock.Files[change.Path] = Entry{Origin: change.Origin, Digest: digest(change.Path, change.content)}
		applied = append(applied, change)
	}
	return applied, nil
}

// destination cleans the directory a source lands in, and refuses one that
// climbs out of the project.
//
// A module says where its files go, and a module is code somebody else wrote:
// "../.." in that string is the difference between publishing into a project
// and writing anywhere the process can reach.
func destination(to string) (string, error) {
	if to == "" || to == "." {
		return ".", nil
	}
	cleaned := path.Clean(filepath.ToSlash(to))
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || path.IsAbs(cleaned) {
		return "", fmt.Errorf("publish: %q leaves the project", to)
	}
	return cleaned, nil
}
