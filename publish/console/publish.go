package console

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/arandu-io/hesape/console"
	"github.com/arandu-io/hesape/foundation"
	"github.com/arandu-io/hesape/publish"
)

// LockFile is where the record of what was published lives, relative to the
// project root.
//
// It is committed, and that is why it sits at the root beside go.sum. The record
// is the only thing separating a file this command wrote from one somebody else
// did: without it every published file comes back as a conflict, so a clone
// that did not carry it is a clone where publishing stops. It also has to be
// reviewed, because a line appearing in it is a file the project did not write
// appearing in the tree.
const LockFile = "vendor-publish.lock"

// PublishCommand is `vendor:publish`.
type PublishCommand struct {
	root         string
	publications func() ([]foundation.Publication, error)
}

// NewPublishCommand returns the command, writing under root and publishing
// what publications answers when it runs.
//
// root is the project root -- the working directory of a binary the CLI ran
// from the project, so "." -- and publications is usually the composed
// application's collection over its registered modules.
func NewPublishCommand(root string, publications func() ([]foundation.Publication, error)) *PublishCommand {
	if root == "" {
		root = "."
	}
	return &PublishCommand{root: root, publications: publications}
}

// Command is the registry entry for vendor:publish.
func (c *PublishCommand) Command() console.Command {
	return console.Command{
		Name:        "vendor:publish",
		Description: "write what the registered modules offer into the project (preview without --apply)",
		Run:         c.Handle,
	}
}

// Handle reports what the registered modules would write into the project, and
// writes it when --apply is given.
//
// Nothing is written without --apply. Publishing twice writes nothing the
// second time. A file changed outside its custom markers is reported and left
// alone, and --force publishes over that one -- carrying what is between the
// markers forward even then. A run that left a file alone ends in an error
// naming it, because a pipeline reads the status and never the listing.
func (c *PublishCommand) Handle(_ context.Context, o *console.IO) error {
	flags := o.Flags()
	tag := flags.String("tag", "", "publish only one kind of file")
	apply := flags.Bool("apply", false, "write the files; without it nothing is written")
	force := flags.Bool("force", false, "publish over a file changed outside its custom markers")
	if err := flags.Parse(o.Args()); err != nil {
		return err
	}

	var publications []foundation.Publication
	if c.publications != nil {
		var err error
		if publications, err = c.publications(); err != nil {
			return err
		}
	}

	sources, err := Sources(publications, foundation.PublishTag(*tag))
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		nothingToPublish(o, *tag)
		return nil
	}

	lockPath := filepath.Join(c.root, LockFile)
	lock, err := publish.ReadLock(lockPath)
	if err != nil {
		return err
	}

	changes, err := publish.Plan(c.root, lock, publish.Options{Force: *force}, sources...)
	if err != nil {
		return err
	}
	report(o, changes)

	if !*apply {
		o.Line("nothing was written. Run it again with --apply.")
		return nil
	}

	// The lock is written whatever Apply answered, and before the answer is
	// returned. A file written and not recorded is a file the next run calls a
	// conflict, so a publication that stopped halfway has to leave behind what
	// it got through.
	_, applyErr := publish.Apply(c.root, lock, changes)
	if len(lock.Files) > 0 {
		if err := lock.Write(lockPath); err != nil {
			return err
		}
	}
	if applyErr != nil {
		return applyErr
	}

	o.Line("applied.")
	return leftAlone(changes)
}

// Sources turns what the modules declared into what the engine writes, keeping
// only the kind of file that was asked for. An empty tag keeps every kind; a tag
// outside the closed set is refused, naming the set.
//
// The origin recorded against every file is the tag, because a tag is all a
// publication still carries by the time it arrives here: collecting them over
// the modules flattens the list, and the name of the module that offered one
// does not survive it.
func Sources(publications []foundation.Publication, tag foundation.PublishTag) ([]publish.Source, error) {
	if tag != "" && !tag.Valid() {
		return nil, fmt.Errorf("%q is not a kind of file a module publishes (expected one of %s)",
			tag, strings.Join(tagNames(), ", "))
	}

	var sources []publish.Source
	for _, publication := range publications {
		if tag != "" && publication.Tag != tag {
			continue
		}
		sources = append(sources, publish.Source{
			Files:  publication.Files,
			From:   publication.From,
			To:     publication.To,
			Origin: string(publication.Tag),
		})
	}
	return sources, nil
}

// tagNames is the closed set, spelled the way a person types it. It is derived
// rather than written out, so the kinds a refusal names are the kinds a module
// may declare.
func tagNames() []string {
	tags := foundation.PublishTags()
	names := make([]string, 0, len(tags))
	for _, tag := range tags {
		names = append(names, string(tag))
	}
	return names
}

// nothingToPublish answers the two ways the plan can be empty, and they are
// different questions: a project whose modules publish nothing at all, and a
// tag that no module offers.
func nothingToPublish(o *console.IO, tag string) {
	if tag != "" {
		o.Line("no registered module publishes a %s.", tag)
		return
	}
	o.Line("no registered module publishes anything.")
	o.Line("A module offers files with Publishes() []foundation.Publication.")
}

// report prints one line per file, then the tally.
//
// The words are the actions themselves -- create, update, unchanged, conflict
// -- so the summary needs no plural of its own and reads the same for one file
// as for forty.
func report(o *console.IO, changes []publish.Change) {
	if len(changes) == 0 {
		// A module that offers a tree with nothing in it. The tally below would
		// be a blank line, which reads as output that went missing.
		o.Line("the modules that publish offer no files.")
		return
	}

	var listing strings.Builder
	w := tabwriter.NewWriter(&listing, 0, 0, 2, ' ', 0)
	for _, change := range changes {
		fmt.Fprintf(w, "%s\t%s\n", change.Action, change.Path)
	}
	_ = w.Flush()
	o.Line("%s", strings.TrimSuffix(listing.String(), "\n"))

	counted := map[publish.Action]int{}
	for _, change := range changes {
		counted[change.Action]++
	}
	var parts []string
	for _, action := range []publish.Action{publish.Create, publish.Update, publish.Unchanged, publish.Conflict} {
		if counted[action] > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", action, counted[action]))
		}
	}
	o.NewLine()
	o.Line("%s", strings.Join(parts, ", "))
}

// leftAlone is the error a publication ends with when it skipped a file.
//
// An error and not one more line, because a run that skipped files and exited
// zero is a run that says it published and did not.
func leftAlone(changes []publish.Change) error {
	var skipped []string
	for _, change := range changes {
		if change.Action == publish.Conflict {
			skipped = append(skipped, change.Path)
		}
	}
	if len(skipped) == 0 {
		return nil
	}

	return fmt.Errorf("left alone, having been changed outside the arandu:begin custom markers "+
		"or never published here:\n  %s\n"+
		"--force publishes over them, and carries what is between the markers forward even then",
		strings.Join(skipped, "\n  "))
}
