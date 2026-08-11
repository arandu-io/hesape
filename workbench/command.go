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

// ErrMissingEmail answers to the UnexpectedValueException
// WorkbenchMakeCommand::buildPackage throws when the workbench config has no
// author email.
var ErrMissingEmail = errors.New("workbench: set the author's email in the workbench configuration")

// ErrMissingPackage is what [WorkbenchMakeCommand.Fire] answers when no
// vendor/name was given. PHP gets this from InputArgument::REQUIRED, which
// Symfony checks before the command runs; a Go flag set has no required
// operand, so the command checks it itself.
var ErrMissingPackage = errors.New("workbench: a vendor/name argument is required")

// Config answers to the workbench configuration array
// WorkbenchMakeCommand::buildPackage reads out of $this->laravel['config'].
//
// The field names are the config keys. Name is the author's name, not the
// package's, because that is what the key means in PHP.
type Config struct {
	// Path is where packages are created: the workbench directory of the
	// application. PHP takes it from $this->laravel['path.base'].'/workbench'.
	Path string

	// Name is the author's name, written into LICENSE. PHP's workbench.name.
	Name string

	// Email is the author's address. PHP's workbench.email, and the one field
	// PHP refuses to run without.
	Email string

	// Host is the module host for the generated go.mod. Empty is
	// [DefaultHost]. It has no PHP counterpart -- see [Package.Host].
	Host string
}

// WorkbenchMakeCommand answers to
// Illuminate\Workbench\Console\WorkbenchMakeCommand.
//
// It lives in this package rather than a console subpackage. PHP puts it in
// Console/ because Laravel scans that directory; nothing scans anything here,
// and the one file is easier to find beside the creator it drives.
type WorkbenchMakeCommand struct {
	creator *PackageCreator
	config  Config
	process *process.Factory
}

// NewWorkbenchMakeCommand answers to WorkbenchMakeCommand::__construct.
//
// PHP takes the creator and reaches for the config and the shell through the
// container. Both are parameters here (ADR 0001): the process factory is one so
// a test can fake `go mod tidy` instead of running it.
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
// PHP's $name is "workbench", after the directory the packages land in. The
// name here is "make:package", which is what docs/31-reorganizacao-hesape.md:123
// settled on: it is the verb a person reaches for, and it groups with the other
// make: commands they already type.
func (c *WorkbenchMakeCommand) Command() console.Command {
	return console.Command{
		Name:        "make:package",
		Description: "create a new package in the workbench",
		Run:         c.Fire,
	}
}

// Fire answers to WorkbenchMakeCommand::fire.
//
// It builds the package from the argument, runs the creator, and then runs the
// dependency resolver over the result -- `go mod tidy`, which is where PHP runs
// `composer install --dev`. The tidy is what writes the require line for
// hesape, which is why the generated go.mod does not carry one.
//
// PHP returns void and lets the exception escape. This returns error, and a
// failure of the tidy is reported without undoing the files: the package is on
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

// runCreator answers to WorkbenchMakeCommand::runCreator.
func (c *WorkbenchMakeCommand) runCreator(pkg *Package, plain bool) (string, error) {
	return c.creator.Create(pkg, c.config.Path, plain)
}

// callGoModTidy answers to WorkbenchMakeCommand::callComposerUpdate.
//
// PHP chdirs into the package and calls passthru('composer install --dev').
// This sets the working directory on the process instead: a chdir in a
// long-lived process changes it for every goroutine, and the CLI has more than
// one.
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

// buildPackage answers to WorkbenchMakeCommand::buildPackage.
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

// getPackageSegments answers to WorkbenchMakeCommand::getPackageSegments.
//
// PHP splits on "/" into at most two and studly-cases both. So does this, with
// str.Studly for studly_case.
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
