package migrations

import "regexp"

// CreatePatterns are the migration names Guess reads as creating a table, with
// the table in the first capture group: create_invoices_table and
// create_invoices both name invoices.
//
// They are tried before ChangePatterns, and the first match wins, so a name
// that could be read either way is read as a create. Exported because they are
// the whole definition of what "this migration creates a table" means, and a
// caller that wants to test a name against them without going through Guess
// can.
var CreatePatterns = []*regexp.Regexp{
	regexp.MustCompile(`^create_(\w+)_table$`),
	regexp.MustCompile(`^create_(\w+)$`),
}

// ChangePatterns are the migration names Guess reads as changing a table,
// with the table in the first capture group.
var ChangePatterns = []*regexp.Regexp{
	regexp.MustCompile(`.+_(?:to|from|in)_(\w+)_table$`),
	regexp.MustCompile(`.+_(?:to|from|in)_(\w+)$`),
}

// Guess returns the table a migration name is about, and whether the
// migration creates it.
//
// It is what turns `aru make:migration create_invoices_table` into a stub
// that already says CREATE TABLE invoices, and `add_paid_at_to_invoices`
// into one that says ALTER TABLE invoices.
//
// guessed is false when neither set of patterns matches, which says "no
// guess" without a zero value that looks like an answer.
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
