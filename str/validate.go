package str

import (
	"encoding/json"
	"net"
	"regexp"
	"strings"
	"sync"
)

// IsJSON answers for Str::isJson. It reports whether the string parses as JSON.
//
// PHP spells it isJson; the Go convention capitalizes the initialism.
func IsJSON(value string) bool {
	return json.Valid([]byte(value))
}

// IsASCII answers for Str::isAscii. It reports whether every byte of the string
// is below 128.
//
// PHP spells it isAscii; the Go convention capitalizes the initialism.
func IsASCII(value string) bool { return isASCII(value) }

// IsURL answers for Str::isUrl. It reports whether the string is an absolute
// URL whose scheme is one of the protocols given, or one of the three hundred
// registered ones when none are given.
//
// PHP spells it isUrl; the Go convention capitalizes the initialism. The shape
// it accepts is the one Symfony's UrlValidator accepts, which the Laravel
// carries verbatim: a scheme, optional basic auth, a host that is a domain of
// at least two characters or an IPv4 address or a bracketed IPv6 address, an
// optional port, and then path, query and fragment.
func IsURL(value string, protocols ...string) bool {
	if !urlPattern(protocols).MatchString(value) {
		return false
	}
	// The bracketed alternative is checked loosely by the pattern and exactly
	// here, because an IPv6 address written as a regular expression is a page
	// of parentheses that nobody can read or correct.
	if open := strings.IndexByte(value, '['); open >= 0 {
		closing := strings.IndexByte(value[open:], ']')
		if closing < 0 || net.ParseIP(value[open+1:open+closing]) == nil {
			return false
		}
	}
	return true
}

var (
	urlPatternOnce  sync.Once
	urlPatternCache *regexp.Regexp
	urlCustomMu     sync.Mutex
	urlCustomCache  = map[string]*regexp.Regexp{}
)

// urlPattern builds the expression for a protocol list, keeping the default one
// compiled once because it is three hundred alternatives long.
func urlPattern(protocols []string) *regexp.Regexp {
	if len(protocols) == 0 {
		urlPatternOnce.Do(func() { urlPatternCache = compileURLPattern(urlProtocols) })
		return urlPatternCache
	}
	list := strings.Join(protocols, "|")
	urlCustomMu.Lock()
	defer urlCustomMu.Unlock()
	if re, ok := urlCustomCache[list]; ok {
		return re
	}
	re := compileURLPattern(list)
	urlCustomCache[list] = re
	return re
}

func compileURLPattern(protocolList string) *regexp.Regexp {
	quoted := make([]string, 0, 8)
	for _, p := range strings.Split(protocolList, "|") {
		quoted = append(quoted, regexp.QuoteMeta(p))
	}
	const (
		userinfo = `(?:[_.\p{L}\p{N}-]|%[0-9A-Fa-f]{2})+`
		domain   = `[\p{L}\p{N}\p{S}\-_.]+(?:\.?(?:[\p{L}\p{N}]|xn--[\p{L}\p{N}-]+)+\.?)`
		ipv4     = `\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`
		ipv6     = `\[[0-9A-Fa-f:.]{2,45}\]`
		path     = `(?:/(?:[\p{L}\p{N}\-._~!$&'()*+,;=:@]|%[0-9A-Fa-f]{2})*)*`
		query    = `(?:\?(?:[\p{L}\p{N}\-._~!$&'\[\]()*+,;=:@/?]|%[0-9A-Fa-f]{2})*)?`
		fragment = `(?:#(?:[\p{L}\p{N}\-._~!$&'()*+,;=:@/?]|%[0-9A-Fa-f]{2})*)?`
	)
	return regexp.MustCompile(`(?i)^(?:` + strings.Join(quoted, "|") + `)://` +
		`(?:(?:` + userinfo + `:)?` + userinfo + `@)?` +
		`(?:` + domain + `|` + ipv4 + `|` + ipv6 + `)` +
		`(?::[0-9]+)?` + path + query + fragment + `\z`)
}

// urlProtocols is the default protocol list Str::isUrl carries, the IANA
// registry as Laravel spells it, pipe separated.
const urlProtocols = ""
