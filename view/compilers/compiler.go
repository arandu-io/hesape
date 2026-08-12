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
//
// It answers the InvalidArgumentException Compiler::__construct throws:
// "Please provide a valid cache path."
var ErrCachePathMissing = errors.New("view/compilers: provide a valid cache path")

// Compiler mirrors Illuminate\View\Compilers\Compiler.
//
// PHP declares it abstract and composes a Filesystem into it. Here it is a
// plain struct that a concrete compiler embeds, and the filesystem is the os
// package: there is one, it is the disk, and a second one would be a second
// way to read a file (RULE 9).
//
// The cache path is storage/framework/views, the same place `aru view:build`
// writes to, and the compiled extension is go rather than php because what
// comes out the other side is Go source that the toolchain then builds.
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

// NewCompiler is Compiler::__construct.
//
// It returns (*Compiler, error) where PHP throws InvalidArgumentException.
// An empty compiledExtension defaults to "go".
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

// GetCompiledPath is Compiler::getCompiledPath.
//
// PHP hashes with xxh128, which the Go standard library does not carry and
// which is not worth a dependency for a cache file name. The digest is the
// first 32 hex digits of SHA-256 over the same input, so the shape of the
// name -- 32 hex digits and the compiled extension -- is unchanged.
func (c *Compiler) GetCompiledPath(path string) string {
	sum := sha256.Sum256([]byte("v2" + after(path, c.basePath)))
	return filepath.Join(c.cachePath, hex.EncodeToString(sum[:])[:32]+"."+c.compiledExtension)
}

// IsExpired is Compiler::isExpired.
//
// It returns (bool, error) where PHP throws ErrorException: a view whose
// source cannot be stat'ed is not silently treated as current.
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

// GetPath is BladeCompiler::getPath.
func (c *Compiler) GetPath() string { return c.path }

// SetPath is BladeCompiler::setPath.
func (c *Compiler) SetPath(path string) { c.path = path }

// GetCachePath reports where compiled views are written.
//
// PHP reads $this->cachePath from inside the class hierarchy; Go has no
// protected, so the accessor is the way a compiler in another package asks.
func (c *Compiler) GetCachePath() string { return c.cachePath }

// ensureCompiledDirectoryExists is Compiler::ensureCompiledDirectoryExists.
func (c *Compiler) ensureCompiledDirectoryExists(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o755)
}

// shortHash is the 32 hex digits PHP gets from hash('xxh128', ...). The
// digest differs; the shape and the purpose -- a stable directory name for a
// prefix -- do not.
func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:32]
}

// after returns the part of subject after the first occurrence of search,
// or subject itself when search is empty or absent. It is Str::after, kept
// local so that this package depends on nothing but the standard library.
func after(subject, search string) string {
	if search == "" {
		return subject
	}
	if i := strings.Index(subject, search); i >= 0 {
		return subject[i+len(search):]
	}
	return subject
}
