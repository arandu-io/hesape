package foundation

// CSRFExempt is optional: the module answers requests that cannot carry a
// CSRF token -- a webhook receiver, called by another system with no session
// -- and declares the path prefixes they arrive under, so the kernel exempts
// them from the CSRF check instead of every application writing the
// exemption by hand.
//
// What a module declares is a prefix as the request carries it, beginning
// with '/' and never "/" itself: the check refuses a prefix that would exempt
// every route, and a module that asks for it fails the boot rather than
// turning the check off.
type CSRFExempt interface {
	CSRFExemptPaths() []string
}

// CSRFExemptions collects the exempt path prefixes of every module that
// declares some, in registration order, each once -- the list the kernel
// hands to the CSRF check as its exemptions.
func CSRFExemptions(modules ...Module) []string {
	var paths []string
	seen := map[string]bool{}
	for _, m := range modules {
		exempt, ok := m.(CSRFExempt)
		if !ok {
			continue
		}
		for _, p := range exempt.CSRFExemptPaths() {
			if !seen[p] {
				seen[p] = true
				paths = append(paths, p)
			}
		}
	}
	return paths
}
