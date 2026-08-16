package workbench

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/arandu-io/hesape/console"
	"github.com/arandu-io/hesape/process"
	"github.com/arandu-io/hesape/str"
)

// ErrMissingEmail is returned when the workbench configuration carries no
// author email. It is the one field the generator refuses to run without.
var ErrMissingEmail = errors.New("workbench: set the author's email in the workbench configuration")

// ErrMissingPackage is what [WorkbenchMakeCommand.Fire] returns when no
// vendor/name was given. A Go flag set has no required operand, so the command
// checks for it itself.
var ErrMissingPackage = errors.New("workbench: a vendor/name argument is required")

// Config is the workbench configuration a generated package is built from.
//
// The field names are the configuration keys. Name is the author's name, not the
// package's.
type Config struct {
	// Path is where packages are created: the workbench directory of the
	// application.
	Path string

	// Name is the author's name, written into LICENSE.
	Name string

	// Email is the author's address, and the one field the generator refuses to
	// run without.
	Email string

	// Host is the module host for the generated go.mod. Empty is
	// [DefaultHost]; see [Package.Host] for why a host is part of the identity.
	Host string
}

// WorkbenchMakeCommand builds `aru make:package`.
//
// It lives in this package rather than a console subpackage: nothing scans a
// directory for commands here, and the one file is easier to find beside the
// creator it drives.
type WorkbenchMakeCommand struct {
	creator *PackageCreator
	config  Config
	process *process.Factory
}

// NewWorkbenchMakeCommand builds the command over a creator, a configuration and
// a process factory.
//
// The process factory is a parameter so a test can fake `go mod tidy` instead of
// running it.
func NewWorkbenchMakeCommand(creator *PackageCreator, config Config, processes *process.Factory) *WorkbenchMakeCommand {
	if creator == nil {
		creator = NewPackageCreator()
	}
	if processes == nil {
		processes = process.NewFactory()
	}
	return &WorkbenchMakeCommand{creator: creator, config: config, process: processes}
}

// Command is the console.Command value a binary registers.
//
// The name is "make:package" rather than the directory the packages land in: it
// is the verb a person reaches for, and it groups with the other make: commands
// they already type.
func (c *WorkbenchMakeCommand) Command() console.Command {
	return console.Command{
		Name:        "make:package",
		Description: "create a new package in the workbench",
		Run:         c.Fire,
	}
}

// Fire builds the package from the argument, runs the creator, and then runs
// `go mod tidy` over the result.
//
// The tidy is what writes the require line for hesape, which is why the
// generated go.mod does not carry one.
//
// A failure of the tidy is reported without undoing the files: the package is on
// disk and correct, and re-running one command is a better answer than deleting
// somebody's new package because the network was down.
func (c *WorkbenchMakeCommand) Fire(ctx context.Context, o *console.IO) error {
	resources := o.Flags().Bool("resources", false, "create the migrations, views and assets directories")
	if err := o.Flags().Parse(o.Args()); err != nil {
		return err
	}

	pkg, err := c.buildPackage(o.Args())
	if err != nil {
		return err
	}

	directory, err := c.runCreator(pkg, !*resources)
	if err != nil {
		return err
	}

	o.Info("Package workbench created: %s", directory)
	o.TwoColumnDetail("module", pkg.ModulePath())

	return c.callGoModTidy(ctx, o, directory)
}

// runCreator writes the package and reports where it landed.
func (c *WorkbenchMakeCommand) runCreator(pkg *Package, plain bool) (string, error) {
	return c.creator.Create(pkg, c.config.Path, plain)
}

// callGoModTidy runs `go mod tidy` inside the generated package.
//
// The working directory is set on the process rather than on this one: a chdir
// in a long-lived process changes it for every goroutine, and the CLI has more
// than one.
func (c *WorkbenchMakeCommand) callGoModTidy(ctx context.Context, o *console.IO, directory string) error {
	result, err := c.process.NewPendingProcess().
		Path(directory).
		Run(ctx, []string{"go", "mod", "tidy"}, nil)
	if err != nil {
		return fmt.Errorf("workbench: go mod tidy: %w", err)
	}
	if result.Failed() {
		o.Warn("go mod tidy failed in %s; the package is written, run it again by hand", directory)
		o.Line("%s", result.ErrorOutput())
	}
	return nil
}

// buildPackage turns the command's argument and configuration into a [Package].
func (c *WorkbenchMakeCommand) buildPackage(args []string) (*Package, error) {
	if strings.TrimSpace(c.config.Email) == "" {
		return nil, ErrMissingEmail
	}

	vendor, name, err := c.getPackageSegments(args)
	if err != nil {
		return nil, err
	}

	return NewPackage(vendor, name, c.config.Name, c.config.Email, c.config.Host)
}

// getPackageSegments splits the argument on "/" into at most two parts and
// studly-cases both.
func (c *WorkbenchMakeCommand) getPackageSegments(args []string) (vendor, name string, err error) {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return "", "", ErrMissingPackage
	}

	segments := strings.SplitN(args[0], "/", 2)
	if len(segments) != 2 {
		return "", "", fmt.Errorf("%w: got %q, want vendor/name", ErrMissingPackage, args[0])
	}
	return str.Studly(segments[0]), str.Studly(segments[1]), nil
}
