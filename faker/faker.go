// Package faker generates plausible values for factories and tests.
//
// # Why this is written here rather than taken from a library
//
// One property decides it: a failing test has to be reproducible from the seed
// it printed. Every Faker this package hands out is driven by an explicitly
// seeded generator, so faker.New(42) yields the same sequence on every run, on
// every machine, forever. The package-level functions of math/rand and
// math/rand/v2 do not have that property -- they are seeded from the runtime --
// and a library that reaches for them cannot be made to have it from outside.
//
// The second reason is smaller and still real: the core of this collection
// carries one third-party dependency, and a name generator is not the one worth
// making it two.
//
// # What it is not
//
// It is not a locale library and it will not become one. The word lists are
// small and English, chosen so that a generated row reads like a row rather
// than like a hash. A project that needs Portuguese street names writes its own
// Faker and passes it in -- which is what the interface is for.
package faker

import (
	"fmt"
	"math/rand/v2"
	"time"
)

// Faker is what a factory definition asks of a source of plausible values.
//
// It is an interface so that a project can substitute its own, and it is small
// so that substituting one is an afternoon rather than a project. Anything not
// here is written in the definition, where the reader can see it.
type Faker interface {
	// FirstName returns a given name.
	FirstName() string
	// LastName returns a family name.
	LastName() string
	// Name returns a full name.
	Name() string
	// UserName returns a handle: lowercase, no spaces.
	UserName() string
	// Email returns an address at a domain reserved for documentation, so a
	// seeded database cannot mail a stranger.
	Email() string
	// Word returns one word.
	Word() string
	// Sentence returns n words, capitalised, ending in a full stop.
	Sentence(words int) string
	// Paragraph returns n sentences.
	Paragraph(sentences int) string
	// Int returns a number in [min, max].
	Int(min, max int) int
	// Float returns a number in [min, max] rounded to decimals places.
	Float(min, max float64, decimals int) float64
	// Bool returns true half the time.
	Bool() bool
	// UUID returns a version 4 identifier.
	//
	// It is drawn from this Faker's generator, not from crypto/rand, because
	// reproducibility is the point here. It is fake data and must never be used
	// where an unguessable identifier is needed.
	UUID() string
	// Time returns an instant in [from, to], truncated to the second.
	Time(from, to time.Time) time.Time
	// Pick returns one of the options.
	Pick(options ...string) string
	// Unique returns a Faker that does not repeat a value it has already
	// answered for the same method.
	Unique() Faker
}

// New returns a Faker seeded with seed.
//
// The same seed yields the same sequence. That is the whole contract, and it is
// what makes a factory failure reproducible: the seed goes in the test output,
// and the run that reproduces it takes the seed back.
func New(seed int64) Faker {
	return &faker{rand: rand.New(rand.NewPCG(uint64(seed), uint64(seed)>>32))}
}

type faker struct {
	rand *rand.Rand
}

func (f *faker) FirstName() string { return f.Pick(firstNames...) }
func (f *faker) LastName() string  { return f.Pick(lastNames...) }
func (f *faker) Name() string      { return f.FirstName() + " " + f.LastName() }

func (f *faker) UserName() string {
	return fmt.Sprintf("%s.%s%d", lower(f.FirstName()), lower(f.LastName()), f.Int(1, 999))
}

// Email answers at example.test, which RFC 6761 reserves and no resolver
// answers for. A seeded database that mails a real address is a seeded database
// that has mailed a stranger.
func (f *faker) Email() string {
	return fmt.Sprintf("%s@%s", f.UserName(), f.Pick("example.test", "example.invalid"))
}

func (f *faker) Word() string { return f.Pick(words...) }

func (f *faker) Sentence(count int) string {
	if count < 1 {
		count = 1
	}
	out := make([]byte, 0, count*7)
	for i := range count {
		word := f.Word()
		if i == 0 {
			out = append(out, upperFirst(word)...)
			continue
		}
		out = append(out, ' ')
		out = append(out, word...)
	}
	return string(append(out, '.'))
}

func (f *faker) Paragraph(count int) string {
	if count < 1 {
		count = 1
	}
	out := ""
	for i := range count {
		if i > 0 {
			out += " "
		}
		out += f.Sentence(f.Int(6, 14))
	}
	return out
}

func (f *faker) Int(min, max int) int {
	if max <= min {
		return min
	}
	return min + f.rand.IntN(max-min+1)
}

func (f *faker) Float(min, max float64, decimals int) float64 {
	if max <= min {
		return min
	}
	scale := 1.0
	for range decimals {
		scale *= 10
	}
	v := min + f.rand.Float64()*(max-min)
	return float64(int64(v*scale+0.5)) / scale
}

func (f *faker) Bool() bool { return f.rand.IntN(2) == 1 }

func (f *faker) UUID() string {
	var b [16]byte
	for i := range b {
		b[i] = byte(f.rand.IntN(256))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func (f *faker) Time(from, to time.Time) time.Time {
	if !to.After(from) {
		return from.Truncate(time.Second)
	}
	span := int(to.Sub(from) / time.Second)
	return from.Add(time.Duration(f.Int(0, span)) * time.Second).Truncate(time.Second)
}

func (f *faker) Pick(options ...string) string {
	if len(options) == 0 {
		return ""
	}
	return options[f.rand.IntN(len(options))]
}

// Unique returns a Faker whose every method refuses to repeat itself.
//
// The uniqueness is per method and per returned Faker, which is what a caller
// asking for unique emails means. It gives up after a bounded number of tries
// and returns the value anyway rather than looping: a word list of forty words
// asked for a hundred unique words has no answer, and hanging is worse than
// repeating.
func (f *faker) Unique() Faker {
	return &uniqueFaker{inner: f, seen: map[string]map[string]struct{}{}}
}
