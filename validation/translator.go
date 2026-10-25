package validation

import (
	"strings"

	"github.com/arandu-io/hesape/translation"
)

// This file is the bridge between the application's translator and the narrow
// Translator a Validator reads its sentences from.
//
// *translation.Translator does not satisfy [Translator] by itself, for two
// reasons that are both about the request:
//
//   - the locale comes first on translation.Translator.Get and last on
//     [Translator].Get, and the replacements are a translation.Replace there and a
//     plain map here;
//   - Get answers a string there, and a line group -- validation.custom,
//     validation.attributes -- here, because the validator looks custom messages
//     and attribute names up by pattern and has to read the whole group to do it.
//
// FromTranslation is the adapter. The locale is not part of it: the Validator
// passes the locale of its request on every call, from [WithLocale].

// FromTranslation adapts the application's translator for a Validator or a
// Factory:
//
//	factory := validation.NewFactory(validation.FromTranslation(tr))
//
// A key with a line answers the line, with the placeholders of the replacements
// filled. A key that names a group answers the group, as nested maps of strings.
// A key with neither answers the key itself, which is what every lookup in this
// package compares against. A missing key is never reported to the translator's
// missing-key callback: the validator probes several keys for each message and
// most of them are not there.
//
// A nil translator is a catalogue with no lines, and a Validator over it says the
// sentences the rules carry.
func FromTranslation(t *translation.Translator) Translator {
	return translationAdapter{t: t}
}

type translationAdapter struct{ t *translation.Translator }

// Get reads a line, or a group of lines, in the locale. The empty locale is the
// translator's own.
func (a translationAdapter) Get(key string, replace map[string]any, locale string) any {
	if a.t == nil {
		return key
	}
	if a.t.Has(locale, key) {
		return a.t.Get(locale, key, translation.Replace(replace))
	}

	group, item, found := strings.Cut(key, ".")
	if !found {
		return key
	}
	if tree := nest(a.t.Lines(locale, group), item+"."); len(tree) > 0 {
		return tree
	}
	return key
}

// DefaultLocale is the locale the translator answers in when it is not told one,
// which is where a module's sentence is looked up for a run that names none.
func (a translationAdapter) DefaultLocale() string {
	if a.t == nil {
		return ""
	}
	return a.t.GetLocale()
}

// Choice reads a line for a count, in the locale.
func (a translationAdapter) Choice(key string, number int, replace map[string]any, locale string) string {
	if a.t == nil {
		return key
	}
	return a.t.Choice(locale, key, number, translation.Replace(replace))
}

// nest turns the dotted lines under a prefix into nested maps of strings:
// "custom.email.required" under "custom." is {"email": {"required": ...}}. A line
// that is both a sentence and the start of a group keeps the sentence.
func nest(lines translation.Lines, prefix string) map[string]any {
	tree := map[string]any{}
	for key, sentence := range lines {
		rest, under := strings.CutPrefix(key, prefix)
		if !under || rest == "" {
			continue
		}
		node := tree
		segments := strings.Split(rest, ".")
		for i, segment := range segments {
			if i == len(segments)-1 {
				if _, taken := node[segment].(map[string]any); !taken {
					node[segment] = sentence
				}
				continue
			}
			next, isGroup := node[segment].(map[string]any)
			if !isGroup {
				next = map[string]any{}
				node[segment] = next
			}
			node = next
		}
	}
	return tree
}
