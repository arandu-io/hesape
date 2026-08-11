package support

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/arandu-io/hesape/support/arr"
)

// Js answers to Illuminate\Support\Js: data turned into a JavaScript
// expression that is safe to drop inside an HTML attribute.
//
// The escaping is what makes it safe, and it is the escaping the PHP asks for
// with JSON_HEX_TAG, JSON_HEX_APOS, JSON_HEX_AMP and JSON_HEX_QUOT: inside a
// string, <, >, ', & and " leave as <, >, ', & and ",
// so none of them can close the attribute or open a tag. A forward slash leaves
// as \/, which is what PHP's json_encode does unless told otherwise.
type Js struct {
	js string
}

// NewJs answers to Js::__construct. The PHP $flags and $depth have no
// counterpart in encoding/json and are not taken; the PHP throws JsonException,
// so this returns (*Js, error).
func NewJs(data any) (*Js, error) {
	expression, err := convertDataToJavaScriptExpression(data)
	if err != nil {
		return nil, err
	}
	return &Js{js: expression}, nil
}

// From answers to Js::from.
func From(data any) (*Js, error) { return NewJs(data) }

// Encode answers to Js::encode: the data as JSON, escaped the way the class
// needs it. A [Jsonable] is asked for its own JSON and an [arr.Arrayer] -- the
// Go form of Illuminate\Contracts\Support\Arrayable -- for its array first.
func Encode(data any) (string, error) {
	if jsonable, ok := data.(Jsonable); ok {
		encoded, err := jsonable.ToJson()
		if err != nil {
			return "", err
		}
		return jsHexEscape(encoded), nil
	}
	if arrayable, ok := data.(arr.Arrayer); ok {
		if _, marshaler := data.(json.Marshaler); !marshaler {
			data = arrayable.ToArray()
		}
	}

	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(data); err != nil {
		return "", err
	}
	return jsHexEscape(strings.TrimRight(buffer.String(), "\n")), nil
}

// convertDataToJavaScriptExpression is Js::convertDataToJavaScriptExpression.
func convertDataToJavaScriptExpression(data any) (string, error) {
	if js, ok := data.(*Js); ok {
		return js.ToHtml(), nil
	}

	encoded, err := Encode(data)
	if err != nil {
		return "", err
	}

	if _, ok := data.(string); ok {
		return "'" + encoded[1:len(encoded)-1] + "'", nil
	}
	return convertJsonToJavaScriptExpression(encoded)
}

// convertJsonToJavaScriptExpression is Js::convertJsonToJavaScriptExpression:
// anything that is not a scalar comes back as a JSON.parse call, so that the
// browser parses it instead of the JavaScript parser reading an object literal.
func convertJsonToJavaScriptExpression(encoded string) (string, error) {
	if encoded == "[]" || encoded == "{}" {
		return encoded, nil
	}
	if strings.HasPrefix(encoded, `"`) || strings.HasPrefix(encoded, "{") || strings.HasPrefix(encoded, "[") {
		wrapped, err := Encode(encoded)
		if err != nil {
			return "", err
		}
		return "JSON.parse('" + wrapped[1:len(wrapped)-1] + "')", nil
	}
	return encoded, nil
}

// jsHexEscape applies JSON_HEX_TAG, JSON_HEX_APOS, JSON_HEX_AMP, JSON_HEX_QUOT
// and PHP's default slash escaping to the contents of every string literal in
// the JSON, leaving the structural characters alone.
func jsHexEscape(encoded string) string {
	var b strings.Builder
	b.Grow(len(encoded))

	inString := false
	for i := 0; i < len(encoded); i++ {
		c := encoded[i]
		if !inString {
			b.WriteByte(c)
			if c == '"' {
				inString = true
			}
			continue
		}
		switch c {
		case '\\':
			if i+1 < len(encoded) && encoded[i+1] == '"' {
				b.WriteString(`"`)
				i++
				continue
			}
			b.WriteByte(c)
			if i+1 < len(encoded) {
				i++
				b.WriteByte(encoded[i])
			}
		case '"':
			b.WriteByte(c)
			inString = false
		case '<':
			b.WriteString(`<`)
		case '>':
			b.WriteString(`>`)
		case '&':
			b.WriteString(`&`)
		case '\'':
			b.WriteString(`'`)
		case '/':
			b.WriteString(`\/`)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// ToHtml answers to Js::toHtml.
func (j *Js) ToHtml() string {
	if j == nil {
		return ""
	}
	return j.js
}

// String answers to Js::__toString.
func (j *Js) String() string { return j.ToHtml() }
