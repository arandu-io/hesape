package support

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
)

// EnvRepository answers to Dotenv\Repository\RepositoryInterface, narrowed to
// the one call Env makes on it.
type EnvRepository interface {
	// Get answers to RepositoryInterface::get: the raw value and whether the
	// variable is set at all, which is the null the PHP returns when it is not.
	Get(key string) (string, bool)
}

// EnvRepositoryFunc lets a plain function be an [EnvRepository].
type EnvRepositoryFunc func(key string) (string, bool)

// Get answers to RepositoryInterface::get.
func (f EnvRepositoryFunc) Get(key string) (string, bool) { return f(key) }

// processEnvRepository is the PutenvAdapter of the PHP: the process
// environment, which in Go is the only one there is.
type processEnvRepository struct{}

func (processEnvRepository) Get(key string) (string, bool) { return os.LookupEnv(key) }

// envAdapters is the ordered list of repositories a read walks.
type envAdapters struct {
	repositories []EnvRepository
}

// Get answers to RepositoryInterface::get, asking each adapter in turn.
func (a envAdapters) Get(key string) (string, bool) {
	for _, repository := range a.repositories {
		if v, ok := repository.Get(key); ok {
			return v, true
		}
	}
	return "", false
}

type envFacade struct{}

// Env answers to Illuminate\Support\Env. It is a value rather than a type
// because every method of the PHP class is static: support.Env.Get(key, nil)
// reads as Env::get($key).
//
// It is also the env() helper of helpers.php, which is Env::get and nothing
// else; PHP keeps class and function names apart and Go does not, so the one
// name is this.
var Env envFacade

var (
	envMu         sync.RWMutex
	envPutenv     = true
	envRepository EnvRepository
	envCustom     []func() EnvRepository
	envCustomName = map[string]int{}
)

// EnablePutenv answers to Env::enablePutenv: read the process environment
// again, and drop the repository that was built without it.
func (envFacade) EnablePutenv() {
	envMu.Lock()
	defer envMu.Unlock()
	envPutenv = true
	envRepository = nil
}

// DisablePutenv answers to Env::disablePutenv: stop reading the process
// environment, so that only the adapters given to [envFacade.Extend] answer.
//
// The PHP drops one adapter of several and keeps $_ENV and $_SERVER; Go has no
// $_ENV, so the process environment is the adapter this drops.
func (envFacade) DisablePutenv() {
	envMu.Lock()
	defer envMu.Unlock()
	envPutenv = false
	envRepository = nil
}

// Extend answers to Env::extend: register another adapter. The variadic
// argument stands for the PHP $name, which is null; a name given twice replaces
// the adapter registered under it.
func (envFacade) Extend(callback func() EnvRepository, name ...string) {
	envMu.Lock()
	defer envMu.Unlock()
	envRepository = nil
	if len(name) > 0 && name[0] != "" {
		if index, ok := envCustomName[name[0]]; ok {
			envCustom[index] = callback
			return
		}
		envCustomName[name[0]] = len(envCustom)
	}
	envCustom = append(envCustom, callback)
}

// GetRepository answers to Env::getRepository: the repository every read goes
// through, built once and kept.
func (envFacade) GetRepository() EnvRepository {
	envMu.Lock()
	defer envMu.Unlock()
	if envRepository != nil {
		return envRepository
	}
	adapters := envAdapters{}
	if envPutenv {
		adapters.repositories = append(adapters.repositories, processEnvRepository{})
	}
	for _, custom := range envCustom {
		if built := custom(); built != nil {
			adapters.repositories = append(adapters.repositories, built)
		}
	}
	envRepository = adapters
	return envRepository
}

// quotedEnvValue is the /\A(['"])(.*)\1\z/ of Env::getOption.
var quotedEnvValue = regexp.MustCompile(`^(['"])([\s\S]*)(['"])$`)

// Get answers to Env::get. The variadic argument stands for the PHP $default,
// which is null; a default that is a func() any is invoked, as PHP's value()
// does.
//
// The strings "true", "(true)", "false", "(false)", "empty", "(empty)", "null"
// and "(null)" become the value they name, in any case, and a quoted value
// loses its quotes -- which is what the PHP getOption does. A variable set to
// "null" reads as nil and does not fall back to the default, because the PHP
// Option is Some(null) there.
func (envFacade) Get(key string, def ...any) any {
	raw, ok := Env.GetRepository().Get(key)
	if !ok {
		return value(firstOr(def, nil))
	}
	return parseEnvValue(raw)
}

// GetOrFail answers to Env::getOrFail. The PHP throws RuntimeException, so this
// returns (any, error).
func (envFacade) GetOrFail(key string) (any, error) {
	raw, ok := Env.GetRepository().Get(key)
	if !ok {
		return nil, fmt.Errorf("Environment variable [%s] has no value.", key)
	}
	return parseEnvValue(raw), nil
}

func parseEnvValue(raw string) any {
	switch strings.ToLower(raw) {
	case "true", "(true)":
		return true
	case "false", "(false)":
		return false
	case "empty", "(empty)":
		return ""
	case "null", "(null)":
		return nil
	}
	if matches := quotedEnvValue.FindStringSubmatch(raw); matches != nil && matches[1] == matches[3] {
		return matches[2]
	}
	return raw
}
