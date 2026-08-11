package workbench

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// StarterDepth is how far below the workbench path [Start] looks for a module.
//
// It is PHP's depth('<= 3') on the Finder, and it is enough for the
// vendor/name/go.mod that [PackageCreator.Create] writes.
const StarterDepth = 3

// Start answers to Illuminate\Workbench\Starter::start.
//
// PHP finds every autoload.php below the workbench path and requires it, which
// makes the packages in there loadable by the running application. Go has no
// runtime require and no autoloader: what makes a local module reachable from
// the application that imports it is a go.work entry, decided before the build
// rather than during it.
//
// So this finds the modules and returns their directories, sorted, and the
// caller does the one thing left:
//
//	directories, err := workbench.Start("./workbench")
//	if err != nil {
//		return err
//	}
//	// go work use ./workbench/acme/invoice-manager ...
//	_, err = factory.Run(ctx, append([]string{"go", "work", "use"}, directories...), nil)
//
// It does not run that itself. Editing go.work is a change to how the whole
// repository builds, and a function called from boot that rewrites the build
// configuration is the kind of thing nobody suspects until a build differs
// between two machines.
//
// A path that does not exist is not an error: it is a project with no workbench,
// which is nearly all of them. It answers nil.
func Start(path string) ([]string, error) {
	root, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !root.IsDir() {
		return nil, nil
	}

	var directories []string
	err = filepath.WalkDir(path, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			// PHP bounds the search with depth('<= 3'); the same bound here
			// keeps a stray vendor/ or a nested checkout from being walked in
			// full on every call.
			if depth(path, current) > StarterDepth {
				return fs.SkipDir
			}
			return nil
		}
		if entry.Name() == "go.mod" {
			directories = append(directories, filepath.Dir(current))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(directories)
	return directories, nil
}

// depth counts the path elements between root and current.
func depth(root, current string) int {
	relative, err := filepath.Rel(root, current)
	if err != nil || relative == "." {
		return 0
	}
	return strings.Count(relative, string(filepath.Separator)) + 1
}
