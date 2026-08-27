package faker

// The word lists.
//
// They are small and English on purpose. What a generated row has to look like
// is a row -- so that a screenshot of a seeded application reads, and a
// developer scanning a table can tell a name from an identifier -- and that
// takes a few dozen words, not a corpus. A project that needs another language
// writes its own Faker; the interface exists for that.
//
// They are package variables rather than files loaded at init, because a
// package that reads a file to answer Name() has a failure mode Name() cannot
// report.

var firstNames = []string{
	"Ada", "Alan", "Grace", "Edsger", "Barbara", "Donald", "Frances", "John",
	"Katherine", "Linus", "Margaret", "Ken", "Radia", "Tim", "Anita", "Bjarne",
	"Carol", "Dennis", "Evelyn", "Guido", "Hedy", "Jean", "Leslie", "Niklaus",
	"Shafi", "Vint",
}

var lastNames = []string{
	"Lovelace", "Turing", "Hopper", "Dijkstra", "Liskov", "Knuth", "Allen",
	"McCarthy", "Johnson", "Torvalds", "Hamilton", "Thompson", "Perlman",
	"Berners-Lee", "Borg", "Stroustrup", "Shaw", "Ritchie", "Boyd", "Van Rossum",
	"Lamarr", "Bartik", "Lamport", "Wirth", "Goldwasser", "Cerf",
}

var words = []string{
	"account", "amber", "anchor", "autumn", "bridge", "candle", "canvas",
	"cedar", "cinder", "cobalt", "compass", "copper", "coral", "crimson",
	"delta", "ember", "falcon", "fathom", "flint", "garnet", "granite",
	"harbour", "hollow", "indigo", "ivory", "juniper", "lantern", "linen",
	"marble", "meadow", "mercury", "orchid", "pebble", "quartz", "quiet",
	"ridge", "russet", "saffron", "sable", "silver", "solace", "summit",
	"thistle", "timber", "umber", "vellum", "verdant", "willow", "zenith",
}

func lower(s string) string {
	out := []byte(s)
	for i, c := range out {
		if c >= 'A' && c <= 'Z' {
			out[i] = c + ('a' - 'A')
		}
		if c == ' ' || c == '-' {
			out[i] = '.'
		}
	}
	return string(out)
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	out := []byte(s)
	if out[0] >= 'a' && out[0] <= 'z' {
		out[0] -= 'a' - 'A'
	}
	return string(out)
}
