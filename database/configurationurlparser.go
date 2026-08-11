package database

import "github.com/arandu-io/hesape/support"

// ConfigurationUrlParser answers
// Illuminate\Database\ConfigurationUrlParser.
//
// The PHP file is an empty subclass of Illuminate\Support\ConfigurationUrlParser
// -- eight lines, a `use`, and a comment that says nothing. It exists so a
// caller inside the database namespace can name it without importing across
// namespaces, which is a PHP convenience with no Go equivalent.
//
// So this is an alias: one type, the two names the two namespaces give it.
// support.ConfigurationUrlParser is the implementation, and there is exactly
// one, which is what the PHP's inheritance also means.
//
// The spelling is the PHP's -- Url, not URL. ADR 0044 allows an initialism to
// go up, and this one does not, because the type it aliases already chose and
// two spellings of one name is worse than one that is slightly wrong.
type ConfigurationUrlParser = support.ConfigurationUrlParser

// NewConfigurationUrlParser is the `new ConfigurationUrlParser` the PHP writes.
func NewConfigurationUrlParser() *ConfigurationUrlParser {
	return support.NewConfigurationUrlParser()
}
