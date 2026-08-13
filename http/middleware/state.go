package middleware

import (
	"net/http"
	"sync"
)

// The class statics of Illuminate\Http\Middleware. In PHP each of TrustHosts,
// TrustProxies and HandleCors carries its own; Go has one namespace per
// package, so they live here together and [FlushState] clears all of them --
// which is what a test that calls three flushState() in a row wanted anyway.
//
// The mutex is here because Go runs tests with -race and PHP has no
// concurrency to speak of.
var globalState = struct {
	sync.RWMutex

	// alwaysTrustHosts is TrustHosts::$alwaysTrust.
	alwaysTrustHosts []string
	// alwaysTrustHostsSet distinguishes "no hosts configured" from "an empty
	// list was configured", which is TrustHosts::$alwaysTrust being null
	// versus being [].
	alwaysTrustHostsSet bool
	// trustSubdomains is TrustHosts::$subdomains.
	trustSubdomains bool

	// skipCallbacks is HandleCors::$skipCallbacks.
	skipCallbacks []func(*http.Request) bool
}{}

// At is TrustHosts::at: name the hosts that should always be trusted, whatever
// a particular [TrustHosts] instance was built with.
//
// The PHP takes array|callable; the callable form exists so the application
// can defer reading configuration until the container has booted, which ADR
// 0001 rejected, so this takes the list.
func At(hosts []string, subdomains bool) {
	globalState.Lock()
	globalState.alwaysTrustHosts = hosts
	globalState.alwaysTrustHostsSet = true
	globalState.trustSubdomains = subdomains
	globalState.Unlock()
}

// Hosts is TrustHosts::hosts: the host patterns that should be trusted.
//
// It is empty until [At] is called. The PHP falls back to every subdomain of
// the configured application URL; there is no configuration repository behind
// this package, so the caller passes its hosts to [TrustHosts] instead.
func Hosts() []string {
	globalState.RLock()
	defer globalState.RUnlock()
	if !globalState.alwaysTrustHostsSet {
		return nil
	}
	out := make([]string, len(globalState.alwaysTrustHosts))
	copy(out, globalState.alwaysTrustHosts)
	return out
}

// TrustsSubdomains reports whether [At] asked for subdomains of the trusted
// hosts to be trusted too. It answers to TrustHosts::$subdomains, which the
// PHP reads inside hosts().
func TrustsSubdomains() bool {
	globalState.RLock()
	defer globalState.RUnlock()
	return globalState.trustSubdomains
}

// SkipWhen is HandleCors::skipWhen: skip the CORS handling for the requests
// the callback picks out.
//
// The PHP appends to a static array and never removes; [FlushState] is how a
// test undoes it, there and here.
func SkipWhen(callback func(*http.Request) bool) {
	globalState.Lock()
	globalState.skipCallbacks = append(globalState.skipCallbacks, callback)
	globalState.Unlock()
}

// shouldSkipCors is HandleCors::shouldSkip.
func shouldSkipCors(r *http.Request) bool {
	globalState.RLock()
	callbacks := globalState.skipCallbacks
	globalState.RUnlock()
	for _, callback := range callbacks {
		if callback(r) {
			return true
		}
	}
	return false
}

// FlushState is TrustHosts::flushState, TrustProxies::flushState and
// HandleCors::flushState: forget everything [At] and [SkipWhen] were told.
//
// It is one function for the three because the three statics live in one Go
// package. A test that flushes one and not the others was flushing all three
// in the PHP too -- Laravel's own test case calls all three from tearDown.
func FlushState() {
	globalState.Lock()
	globalState.alwaysTrustHosts = nil
	globalState.alwaysTrustHostsSet = false
	globalState.trustSubdomains = false
	globalState.skipCallbacks = nil
	globalState.Unlock()
}
