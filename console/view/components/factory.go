// Package components is the set of shapes a command prints in.
//
// It mirrors Illuminate\Console\View\Components, and the files it answers to, in
// the clone at laravel_illuminate/console/View/Components:
//
//	Alert.php
//	Ask.php
//	AskWithCompletion.php
//	BulletList.php
//	Choice.php
//	Component.php
//	Confirm.php
//	Error.php
//	Factory.php
//	Info.php
//	Line.php
//	Mutators/EnsureDynamicContentIsHighlighted.php
//	Mutators/EnsureNoPunctuation.php
//	Mutators/EnsurePunctuation.php
//	Mutators/EnsureRelativePaths.php
//	Secret.php
//	Success.php
//	Task.php
//	TwoColumnDetail.php
//	Warn.php
//
// What is not here is the view files: PHP compiles four .php templates through
// Termwind, and there is no Termwind and no template engine between a command
// and its terminal. The markup those four files carry is written into the
// component that used to include them.
package components

// Factory is where a command reaches for a component.
//
// It answers View\Components\Factory. PHP dispatches by __call, turning the
// method name into a class name; here the methods are declared, which is what
// makes a typo a compile error rather than the InvalidArgumentException the PHP
// throws at run time.
type Factory struct {
	output Output
	base   string
}

// NewFactory returns the factory for one output.
//
// base is the application root that EnsureRelativePaths strips from a path, and
// empty means paths are printed as they came.
func NewFactory(output Output, base string) *Factory {
	return &Factory{output: output, base: base}
}

// Output returns the stream the components render into.
func (f *Factory) Output() Output { return f.output }

// Alert renders a message in a box, in capitals.
func (f *Factory) Alert(message string, verbosity ...Verbosity) {
	NewAlert(f.output, f.base).Render(message, verbosity...)
}

// BulletList renders one line per element.
func (f *Factory) BulletList(elements []string, verbosity ...Verbosity) {
	NewBulletList(f.output, f.base).Render(elements, verbosity...)
}

// Line renders a labelled sentence in one of the four styles: info, success,
// warn, error.
func (f *Factory) Line(style, message string, verbosity ...Verbosity) {
	NewLineComponent(f.output, f.base).Render(style, message, verbosity...)
}

// Info renders an informational line.
func (f *Factory) Info(message string, verbosity ...Verbosity) {
	NewInfo(f.output, f.base).Render(message, verbosity...)
}

// Success renders a line that reports something worked.
func (f *Factory) Success(message string, verbosity ...Verbosity) {
	NewSuccess(f.output, f.base).Render(message, verbosity...)
}

// Warn renders a line about something that is off but did not stop the command.
func (f *Factory) Warn(message string, verbosity ...Verbosity) {
	NewWarn(f.output, f.base).Render(message, verbosity...)
}

// Error renders a line about what went wrong.
func (f *Factory) Error(message string, verbosity ...Verbosity) {
	NewError(f.output, f.base).Render(message, verbosity...)
}

// TwoColumnDetail renders a label and its value, joined by dots.
func (f *Factory) TwoColumnDetail(first, second string, verbosity ...Verbosity) {
	NewTwoColumnDetail(f.output, f.base).Render(first, second, verbosity...)
}

// Task runs the work and reports how it went, on one line.
func (f *Factory) Task(description string, task TaskFunc, verbosity ...Verbosity) error {
	return NewTask(f.output, f.base).Render(description, task, verbosity...)
}

// Ask puts a question and returns the answer.
func (f *Factory) Ask(question, def string) (string, error) {
	return NewAsk(f.output, f.base).Render(question, def)
}

// AskWithCompletion is Ask with a list the terminal completes from.
func (f *Factory) AskWithCompletion(question string, choices []string, def string) (string, error) {
	return NewAskWithCompletion(f.output, f.base).Render(question, choices, def)
}

// Secret asks for a value the terminal must not show.
func (f *Factory) Secret(question string) (string, error) {
	return NewSecret(f.output, f.base).Render(question)
}

// Confirm asks a yes or no question.
func (f *Factory) Confirm(question string, def bool) (bool, error) {
	return NewConfirm(f.output, f.base).Render(question, def)
}

// Choice offers a numbered list and returns the option that was picked.
func (f *Factory) Choice(question string, choices []string, def string) (string, error) {
	return NewChoice(f.output, f.base).Render(question, choices, def)
}
