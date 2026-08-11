package support

import (
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/arandu-io/hesape/support/arr"
)

// ErrMalformedConfigurationURL answers to the InvalidArgumentException
// ConfigurationUrlParser::parseUrl raises.
//
// The message is the PHP one, capital and full stop included.
var ErrMalformedConfigurationURL = errors.New("The database configuration URL is malformed.")

// ConfigurationUrlParser answers to
// Illuminate\Support\ConfigurationUrlParser: the reader that turns a single
// DATABASE_URL into the connection options a driver wants.
type ConfigurationUrlParser struct{}

// NewConfigurationUrlParser is the `new ConfigurationUrlParser` the PHP writes,
// which has no constructor of its own.
func NewConfigurationUrlParser() *ConfigurationUrlParser { return &ConfigurationUrlParser{} }

var (
	driverAliasesMu sync.RWMutex

	// driverAliases answers to the protected static $driverAliases property.
	driverAliases = map[string]string{
		"mssql":      "sqlsrv",
		"mysql2":     "mysql", // RDS
		"postgres":   "pgsql",
		"postgresql": "pgsql",
		"sqlite3":    "sqlite",
		"redis":      "tcp",
		"rediss":     "tls",
	}
)

// sqliteTripleSlash is the '#^(sqlite3?):///#' of ConfigurationUrlParser::parseUrl.
var sqliteTripleSlash = regexp.MustCompile(`^(sqlite3?):///`)

// ParseConfiguration answers to ConfigurationUrlParser::parseConfiguration: the
// configuration with the url key read out and spread over driver, database,
// host, port, username, password and whatever the query string carried.
//
// The PHP accepts an array or a bare string, and so does this. The PHP throws
// InvalidArgumentException on a URL it cannot read, so this returns
// (map[string]any, error).
func (p *ConfigurationUrlParser) ParseConfiguration(config any) (map[string]any, error) {
	settings := map[string]any{}
	switch c := config.(type) {
	case nil:
		return settings, nil
	case string:
		settings["url"] = c
	case map[string]any:
		for key, held := range c {
			settings[key] = held
		}
	default:
		return settings, nil
	}

	raw := toString(arr.Pull(settings, "url", nil))
	if raw == "" {
		return settings, nil
	}

	parsed, err := parseConfigurationURL(raw)
	if err != nil {
		return nil, err
	}

	for key, held := range p.getPrimaryOptions(parsed) {
		settings[key] = held
	}
	for key, held := range p.getQueryOptions(parsed) {
		settings[key] = held
	}
	return settings, nil
}

// urlComponents is the array parse_url returns, narrowed to the keys the parser
// reads.
type urlComponents struct {
	scheme string
	host   string
	port   string
	user   string
	pass   string
	path   string
	query  string
}

// parseConfigurationURL answers to the protected
// ConfigurationUrlParser::parseUrl, sqlite rewrite included: sqlite:///db is
// given the null host the PHP gives it, so the path survives parsing.
func parseConfigurationURL(raw string) (urlComponents, error) {
	raw = sqliteTripleSlash.ReplaceAllString(raw, "$1://null/")
	parsed, err := url.Parse(raw)
	if err != nil {
		return urlComponents{}, ErrMalformedConfigurationURL
	}
	components := urlComponents{
		scheme: parsed.Scheme,
		host:   parsed.Hostname(),
		port:   parsed.Port(),
		path:   parsed.Path,
		query:  parsed.RawQuery,
	}
	if parsed.User != nil {
		components.user = parsed.User.Username()
		components.pass, _ = parsed.User.Password()
	}
	return components, nil
}

// getPrimaryOptions answers to the protected
// ConfigurationUrlParser::getPrimaryOptions. A component the URL does not carry
// is left out, which is what the array_filter on null does.
func (p *ConfigurationUrlParser) getPrimaryOptions(parsed urlComponents) map[string]any {
	options := map[string]any{}
	if driver := p.getDriver(parsed); driver != "" {
		options["driver"] = driver
	}
	if database := p.getDatabase(parsed); database != "" {
		options["database"] = parseStringToNativeType(rawURLDecode(database))
	}
	if parsed.host != "" {
		options["host"] = parseStringToNativeType(rawURLDecode(parsed.host))
	}
	if parsed.port != "" {
		options["port"] = parseStringToNativeType(parsed.port)
	}
	if parsed.user != "" {
		options["username"] = parseStringToNativeType(rawURLDecode(parsed.user))
	}
	if parsed.pass != "" {
		options["password"] = parseStringToNativeType(rawURLDecode(parsed.pass))
	}
	return options
}

// getDriver answers to the protected ConfigurationUrlParser::getDriver.
func (p *ConfigurationUrlParser) getDriver(parsed urlComponents) string {
	if parsed.scheme == "" {
		return ""
	}
	driverAliasesMu.RLock()
	defer driverAliasesMu.RUnlock()
	if alias, ok := driverAliases[parsed.scheme]; ok {
		return alias
	}
	return parsed.scheme
}

// getDatabase answers to the protected ConfigurationUrlParser::getDatabase: the
// path with its leading slash off, and nothing at all when there is no path.
func (p *ConfigurationUrlParser) getDatabase(parsed urlComponents) string {
	if parsed.path == "" || parsed.path == "/" {
		return ""
	}
	return strings.TrimPrefix(parsed.path, "/")
}

// getQueryOptions answers to the protected
// ConfigurationUrlParser::getQueryOptions, which is PHP's parse_str.
func (p *ConfigurationUrlParser) getQueryOptions(parsed urlComponents) map[string]any {
	if parsed.query == "" {
		return map[string]any{}
	}
	return parseStringsToNativeTypes(parseQueryString(parsed.query)).(map[string]any)
}

// parseStringsToNativeTypes answers to the protected
// ConfigurationUrlParser::parseStringsToNativeTypes.
func parseStringsToNativeTypes(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, held := range typed {
			out[key] = parseStringsToNativeTypes(held)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, held := range typed {
			out = append(out, parseStringsToNativeTypes(held))
		}
		return out
	case string:
		return parseStringToNativeType(typed)
	default:
		return v
	}
}

// parseStringToNativeType is json_decode on one value: "true" is a bool, "5432"
// is a number, and anything the decoder refuses stays the string it was.
//
// PHP reads a whole number as an int; encoding/json reads every number as a
// float64, so an integral number is handed back as an int.
func parseStringToNativeType(v string) any {
	var decoded any
	if err := json.Unmarshal([]byte(v), &decoded); err != nil {
		return v
	}
	if number, ok := decoded.(float64); ok && number == float64(int(number)) {
		return int(number)
	}
	return decoded
}

// GetDriverAliases answers to ConfigurationUrlParser::getDriverAliases.
func GetDriverAliases() map[string]string {
	driverAliasesMu.RLock()
	defer driverAliasesMu.RUnlock()
	out := make(map[string]string, len(driverAliases))
	for alias, driver := range driverAliases {
		out[alias] = driver
	}
	return out
}

// AddDriverAlias answers to ConfigurationUrlParser::addDriverAlias.
func AddDriverAlias(alias, driver string) {
	driverAliasesMu.Lock()
	defer driverAliasesMu.Unlock()
	driverAliases[alias] = driver
}
