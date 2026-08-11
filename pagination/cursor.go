package pagination

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// pointsToNextItemsKey is the name Illuminate's Cursor::toArray() merges the
// direction in under. It is kept byte for byte because the encoded cursor
// travels in URLs and in API payloads: a Go paginator has to read a token a PHP
// one wrote, and the other way round.
const pointsToNextItemsKey = "_pointsToNextItems"

// ErrCursor is what FromEncoded returns for a cursor it cannot read.
// Callers that page a public list should treat it as "start from the
// beginning", the way ResolveCurrentCursor does: a mangled cursor is a
// truncated link in an e-mail client, not an attack worth a 400.
//
// Illuminate's Cursor::fromEncoded returns null instead, having no error to
// return.
var ErrCursor = errors.New("pagination: malformed cursor")

// Cursor is Illuminate's Pagination\Cursor: a position in an ordered result
// set, being the values of the columns the query orders by for the row at one
// edge of a page, plus the direction the next query walks in.
//
// It is what keyset pagination replaces OFFSET with. OFFSET makes the database
// count and discard every row it skips, so page 500 costs five hundred pages of
// work, and it reads the page boundary at query time, so a row inserted while
// somebody reads page 3 pushes a row from page 3 onto page 4 -- a row that was
// never shown, and never will be. A cursor names the boundary by value, so the
// scan starts at the index entry and the pages hold still.
//
// The parameters are strings because they land in a URL and come back from one.
// A repository formats its ordering columns into strings on the way out and
// parses them on the way back in, and formatting is where it decides that a
// timestamp keeps its subsecond digits -- a cursor rounded to the second walks
// past every row that shares it.
//
// The fields are unexported because Illuminate reads them through parameter(),
// parameters(), pointsToNextItems() and pointsToPreviousItems(), and a struct
// with both would offer two ways to ask the same question.
type Cursor struct {
	parameters        map[string]string
	pointsToNextItems bool
}

// NewCursor answers Cursor::__construct().
//
// parameters maps the name of each ordering column to the value it has in the
// boundary row. The set must match the ORDER BY exactly: a cursor over
// (created_at, id) read by a query ordered by (email, id) computes the boundary
// on one ordering and returns the rows in another, which skips rows and repeats
// rows in silence.
//
// pointsToNextItems reports which side of the boundary the next query reads.
// True is forward, the ordinary "next page" -- it is the default in PHP, which
// has default arguments and Go does not. False is backward, and a backward
// query returns its rows in reverse order; CursorPaginate turns them around
// again.
//
// The map is copied, so a cursor cannot be changed through the map its caller
// kept.
func NewCursor(parameters map[string]string, pointsToNextItems bool) Cursor {
	copied := make(map[string]string, len(parameters))
	for name, value := range parameters {
		copied[name] = value
	}
	return Cursor{parameters: copied, pointsToNextItems: pointsToNextItems}
}

// Parameter answers Cursor::parameter(). It returns the value the boundary row
// has in the named ordering column.
//
// A name the cursor does not carry is an error, where PHP throws
// UnexpectedValueException. It is not an empty string, because an empty string
// is a legitimate value for a nullable column and telling the two apart is the
// difference between reading the next page and reading the first one again.
func (c Cursor) Parameter(parameterName string) (string, error) {
	value, ok := c.parameters[parameterName]
	if !ok {
		return "", fmt.Errorf("pagination: unable to find parameter [%s] in pagination item", parameterName)
	}
	return value, nil
}

// Parameters answers Cursor::parameters(). It returns the values of the named
// ordering columns, in the order asked for, and fails on the first name the
// cursor does not carry.
func (c Cursor) Parameters(parameterNames []string) ([]string, error) {
	values := make([]string, 0, len(parameterNames))
	for _, name := range parameterNames {
		value, err := c.Parameter(name)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

// PointsToNextItems answers Cursor::pointsToNextItems(). It reports whether the
// query reading from this cursor walks forward.
func (c Cursor) PointsToNextItems() bool { return c.pointsToNextItems }

// PointsToPreviousItems answers Cursor::pointsToPreviousItems(). It reports
// whether the query reading from this cursor walks backward, and so returns its
// rows in reverse order.
func (c Cursor) PointsToPreviousItems() bool { return !c.pointsToNextItems }

// ToArray answers Cursor::toArray(). It is the parameters with the direction
// merged in under _pointsToNextItems, which is the shape Encode writes.
//
// An ordering column actually named _pointsToNextItems would collide with the
// direction, in Go exactly as in PHP. It is not defended against here: doing so
// would change the encoding, and a cursor that no PHP paginator can read is a
// worse fault than a column name nobody uses.
func (c Cursor) ToArray() map[string]any {
	out := make(map[string]any, len(c.parameters)+1)
	for name, value := range c.parameters {
		out[name] = value
	}
	out[pointsToNextItemsKey] = c.pointsToNextItems
	return out
}

// Encode answers Cursor::encode(). It renders the cursor as one URL-safe token:
// base64 of the JSON of ToArray, with "+" and "/" replaced by "-" and "_" and
// the padding removed, which is exactly base64url without padding.
//
// It is encoding rather than encryption: anybody can read it, and anybody can
// write one. That is why a repository validates the parameter names it takes
// from a cursor against the same allowlist it validates a sort field against --
// a column name interpolated from a cursor is injection through the same door
// as a column name interpolated from a query string.
func (c Cursor) Encode() string {
	// A map of strings and a bool always marshal, so the error is unreachable
	// rather than ignored.
	encoded, err := json.Marshal(c.ToArray())
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(encoded)
}

// FromEncoded answers Cursor::fromEncoded(). It reads a cursor back out of the
// token Encode wrote.
//
// PHP writes Cursor::fromEncoded($token) and Go has no static methods, so the
// type is gone from the identifier: pagination.FromEncoded(token). The name is
// the PHP one, because that is the one a reader coming from Laravel searches
// for.
//
// Padding is tolerated, so a cursor that travelled through something that pads
// base64 still parses. Anything else -- empty, not base64, not a JSON object --
// is ErrCursor, where PHP returns null.
//
// Numbers keep the digits they were written with rather than going through a
// float, so a cursor over a 64-bit key survives the round trip. A missing
// _pointsToNextItems reads as backward, which is what PHP's null does.
func FromEncoded(encodedString string) (Cursor, error) {
	if encodedString == "" {
		return Cursor{}, fmt.Errorf("%w: empty", ErrCursor)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(encodedString, "="))
	if err != nil {
		return Cursor{}, fmt.Errorf("%w: %v", ErrCursor, err)
	}

	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.UseNumber()
	var wire map[string]any
	if err := decoder.Decode(&wire); err != nil {
		return Cursor{}, fmt.Errorf("%w: %v", ErrCursor, err)
	}
	if wire == nil {
		return Cursor{}, fmt.Errorf("%w: null", ErrCursor)
	}

	cursor := Cursor{parameters: make(map[string]string, len(wire))}
	for name, value := range wire {
		if name == pointsToNextItemsKey {
			cursor.pointsToNextItems, _ = value.(bool)
			continue
		}
		text, ok := cursorParameterString(value)
		if !ok {
			return Cursor{}, fmt.Errorf("%w: parameter [%s] is not a scalar", ErrCursor, name)
		}
		cursor.parameters[name] = text
	}
	return cursor, nil
}

// cursorParameterString renders one decoded JSON scalar as the string the
// parameters map holds. A JSON null becomes the empty string, matching what a
// nullable column formats to on the way out.
func cursorParameterString(value any) (string, bool) {
	switch v := value.(type) {
	case nil:
		return "", true
	case string:
		return v, true
	case json.Number:
		return v.String(), true
	case bool:
		return strconv.FormatBool(v), true
	default:
		return "", false
	}
}

// ResolveCurrentCursor answers AbstractCursorPaginator::resolveCurrentCursor().
//
// Illuminate reads the cursor through a static closure a service provider
// installs; there is no container here (ADR 0001) and no facade (ADR 0002), so
// the URL of the request being served is passed in. A cursor that is absent or
// does not parse is nil, which every constructor reads as "the first page".
//
// An empty cursorName means DefaultCursorName.
func ResolveCurrentCursor(u *url.URL, cursorName string) *Cursor {
	if cursorName == "" {
		cursorName = DefaultCursorName
	}
	if u == nil {
		return nil
	}
	raw := u.Query().Get(cursorName)
	if raw == "" {
		return nil
	}
	cursor, err := FromEncoded(raw)
	if err != nil {
		return nil
	}
	return &cursor
}
