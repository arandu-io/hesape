package console

import (
	"context"
	"fmt"
	"strings"
)

// QuestionHelper writes the prompt a question is asked with.
//
// It renders the question in the shape the components use, and that is the
// whole of it -- the reading of the answer is on IO, because there is no
// Question object here for a helper to be handed.
type QuestionHelper struct{}

// WritePrompt renders the question and its default.
//
// A default is shown in brackets, and a question with none is shown bare:
// the brackets say "press enter for this", and showing empty ones says it
// about nothing.
func (QuestionHelper) WritePrompt(question, def string) string {
	if def == "" {
		return question + " "
	}
	return fmt.Sprintf("%s [%s] ", question, def)
}

// ConfigurePrompts decides whether the command may ask anything.
//
// A command run with --no-interaction, or with nothing on its input, does
// not ask -- it takes the default and carries on. A prompt in a deploy
// pipeline is a pipeline that hangs.
func (o *IO) ConfigurePrompts() {
	if o.input == nil {
		return
	}
	if o.HasOption("no-interaction") && o.Option("no-interaction").Bool() {
		o.input.SetInteractive(false)
	}
}

// Interactive reports whether the command may prompt.
//
// A command with no signature may: it was started from a terminal and nobody
// said otherwise.
func (o *IO) Interactive() bool { return o.input == nil || o.input.Interactive() }

// PromptForMissingArguments asks for the required arguments that were left out.
//
// It is what turns `make:model` with no name into a question rather than an
// error, and it asks only for what the signature says is required -- an
// optional argument has a default, and asking for it would be asking a
// question with a known answer.
//
// It does nothing when the command may not prompt, so the pipeline gets the
// error and the person gets the question.
func (o *IO) PromptForMissingArguments() error {
	if o.input == nil || !o.Interactive() {
		return nil
	}

	arguments, _ := o.input.Definition()
	for _, argument := range arguments {
		if !argument.IsRequired() || o.input.Argument(argument.Name).Present() {
			continue
		}

		question := argument.Description
		if question == "" {
			question = "What is the " + strings.ReplaceAll(argument.Name, "_", " ") + "?"
		}

		answer, err := o.OutputComponents().Ask(question, "")
		if err != nil {
			return err
		}
		if strings.TrimSpace(answer) == "" {
			return Exit(1, "the argument %s is required", argument.Name)
		}
		o.input.setArgument(argument.Name, answer)
	}
	return nil
}

// TestOptions are the flags a generator adds when it can write a matching test.
//
// Go has one test runner, so there is one flag: the three-way choice a
// language with several runners would need does not exist here.
func TestOptions(typeName string) []Option {
	return []Option{{
		Name:        "test",
		Mode:        OptionValueNone,
		Description: "Generate an accompanying test for the " + typeName,
	}}
}

// HandleTestCreation writes the matching test, when it was asked for.
//
// The path is the generated file's, with _test appended, written by calling
// the same make:test a person would run by hand.
func (g GeneratorCommand) HandleTestCreation(ctx context.Context, app *Application, o *IO, path string) error {
	if !o.HasOption("test") || !o.Option("test").Bool() {
		return nil
	}
	name := strings.TrimSuffix(path, ".go") + "_test"
	return app.Call(ctx, "make:test", name)
}
