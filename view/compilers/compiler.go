package compilers

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ErrCachePathMissing is returned by NewCompiler when no cache path is given.
var ErrCachePathMissing = errors.New("view/compilers: provide a valid cache path")

// Compiler is a plain struct that a concrete compiler embeds. Its filesystem
// is the os package: there is one, it is the disk, and a second one would be
// a second way to read a file.
//
// The cache path is storage/framework/views, the same place the view build
// writes to. The compiled extension is go, because what comes out the other
// side is Go source that the toolchain then builds.
type Compiler struct {
	// cachePath is where compiled views are written.
	cachePath string

	// basePath is stripped from a path before it is hashed.
	basePath string

	// shouldCache reports whether compiled views may be reused.
	shouldCache bool

	// compiledExtension is the extension of the compiled file.
	compiledExtension string

	// shouldCheckTimestamps reports whether modification times are compared.
	shouldCheckTimestamps bool

	// path is the file currently being compiled.
	path string
}

// NewCompiler returns a Compiler, or ErrCachePathMissing if cachePath is
// empty. An empty compiledExtension defaults to "go".
func NewCompiler(cachePath, basePath string, shouldCache bool, compiledExtension string, shouldCheckTimestamps bool) (*Compiler, error) {
	if cachePath == "" {
		return nil, ErrCachePathMissing
	}
	if compiledExtension == "" {
		compiledExtension = "go"
	}
	return &Compiler{
		cachePath:             cachePath,
		basePath:              basePath,
		shouldCache:           shouldCache,
		compiledExtension:     compiledExtension,
		shouldCheckTimestamps: shouldCheckTimestamps,
	}, nil
}

// GetCompiledPath returns the cache path path compiles to.
//
// The digest is the first 32 hex digits of SHA-256 over the input, chosen
// over a third-party hash so that a cache file name costs no dependency.
func (c *Compiler) GetCompiledPath(path string) string {
	sum := sha256.Sum256([]byte("v2" + after(path, c.basePath)))
	return filepath.Join(c.cachePath, hex.EncodeToString(sum[:])[:32]+"."+c.compiledExtension)
}

// IsExpired reports whether the compiled output for path is stale, and
// returns an error rather than silently treating an unreadable source as
// current.
func (c *Compiler) IsExpired(path string) (bool, error) {
	if !c.shouldCache {
		return true, nil
	}

	compiled := c.GetCompiledPath(path)

	// A compiled file that is not there means the view is expired, so that it
	// gets compiled rather than read as an empty page.
	compiledInfo, err := os.Stat(compiled)
	if err != nil {
		return true, nil
	}

	if !c.shouldCheckTimestamps {
		return false, nil
	}

	sourceInfo, err := os.Stat(path)
	if err != nil {
		return true, err
	}

	return !sourceInfo.ModTime().Before(compiledInfo.ModTime()), nil
}

// GetPath returns the path of the file currently being compiled.
func (c *Compiler) GetPath() string { return c.path }

// SetPath sets the path of the file currently being compiled.
func (c *Compiler) SetPath(path string) { c.path = path }

// GetCachePath reports where compiled views are written.
//
// Go has no protected field, so this accessor is how a compiler in another
// package reads it.
func (c *Compiler) GetCachePath() string { return c.cachePath }

// ensureCompiledDirectoryExists creates the directory path is written into,
// if it does not already exist.
func (c *Compiler) ensureCompiledDirectoryExists(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o755)
}

// shortHash returns 32 hex digits: a stable directory name for a prefix.
func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:32]
}

// after returns the part of subject after the first occurrence of search,
// or subject itself when search is empty or absent. Kept local so that this
// package depends on nothing but the standard library.
func after(subject, search string) string {
	if search == "" {
		return subject
	}
	if i := strings.Index(subject, search); i >= 0 {
		return subject[i+len(search):]
	}
	return subject
}
