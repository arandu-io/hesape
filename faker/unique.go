package faker

import "time"

// uniqueFaker is what Unique returns: the same generator, with a memory of what
// each method has already answered.
type uniqueFaker struct {
	inner *faker
	seen  map[string]map[string]struct{}
}

// attempts is how many times a unique method tries before giving up.
//
// It gives up rather than looping because exhaustion is a real state: a list of
// forty words asked for a hundred unique words has no answer, and a factory that
// hangs is worse than one that repeats a word.
const attempts = 100

func (u *uniqueFaker) once(method string, next func() string) string {
	seen := u.seen[method]
	if seen == nil {
		seen = map[string]struct{}{}
		u.seen[method] = seen
	}
	var value string
	for range attempts {
		value = next()
		if _, taken := seen[value]; !taken {
			seen[value] = struct{}{}
			return value
		}
	}
	return value
}

func (u *uniqueFaker) FirstName() string { return u.once("FirstName", u.inner.FirstName) }
func (u *uniqueFaker) LastName() string  { return u.once("LastName", u.inner.LastName) }
func (u *uniqueFaker) Name() string      { return u.once("Name", u.inner.Name) }
func (u *uniqueFaker) UserName() string  { return u.once("UserName", u.inner.UserName) }
func (u *uniqueFaker) Email() string     { return u.once("Email", u.inner.Email) }
func (u *uniqueFaker) Word() string      { return u.once("Word", u.inner.Word) }
func (u *uniqueFaker) UUID() string      { return u.once("UUID", u.inner.UUID) }

func (u *uniqueFaker) Sentence(words int) string {
	return u.once("Sentence", func() string { return u.inner.Sentence(words) })
}

func (u *uniqueFaker) Paragraph(sentences int) string {
	return u.once("Paragraph", func() string { return u.inner.Paragraph(sentences) })
}

func (u *uniqueFaker) Pick(options ...string) string {
	return u.once("Pick", func() string { return u.inner.Pick(options...) })
}

// Int, Float, Bool and Time are not made unique.
//
// A bounded numeric range runs out immediately -- Bool has two answers -- and a
// method that silently stops being unique is worse than one that never claimed
// to be. A caller that needs distinct numbers has a sequence, which is a
// factory concern and not a faker one.
func (u *uniqueFaker) Int(min, max int) int { return u.inner.Int(min, max) }

func (u *uniqueFaker) Float(min, max float64, decimals int) float64 {
	return u.inner.Float(min, max, decimals)
}

func (u *uniqueFaker) Bool() bool { return u.inner.Bool() }

func (u *uniqueFaker) Time(from, to time.Time) time.Time { return u.inner.Time(from, to) }

// Unique on a unique Faker answers itself: asking twice is not asking for two
// independent memories, it is asking for the one that is already there.
func (u *uniqueFaker) Unique() Faker { return u }
