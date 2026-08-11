package migrations

import "regexp"

// CreatePatterns answers
// Illuminate\Database\Console\Migrations\TableGuesser::CREATE_PATTERNS.
var CreatePatterns = []*regexp.Regexp{
	regexp.MustCompile(`^create_(\w+)_table$`),
	regexp.MustCompile(`^create_(\w+)$`),
}

// ChangePatterns answers TableGuesser::CHANGE_PATTERNS.
var ChangePatterns = []*regexp.Regexp{
	regexp.MustCompile(`.+_(?:to|from|in)_(\w+)_table$`),
	regexp.MustCompile(`.+_(?:to|from|in)_(\w+)$`),
}

// Guess answers TableGuesser::guess: the table a migration name is about, and
// whether the migration creates it.
//
// It is what turns `aru make:migration create_invoices_table` into a stub that
// already says CREATE TABLE invoices, and `add_paid_at_to_invoices` into one
// that says ALTER TABLE invoices.
//
// The PHP returns null -- silently, off the end of the function -- when neither
// set of patterns matches, and every caller then does `[$table, $create] =
// TableGuesser::guess(...)` on it. The third value here says "no guess", which
// is the same information without the fatal.
func Guess(migration string) (table string, create bool, guessed bool) {
	for _, pattern := range CreatePatterns {
		if matches := pattern.FindStringSubmatch(migration); matches != nil {
			return matches[1], true, true
		}
	}

	for _, pattern := range ChangePatterns {
		if matches := pattern.FindStringSubmatch(migration); matches != nil {
			return matches[1], false, true
		}
	}

	return "", false, false
}
