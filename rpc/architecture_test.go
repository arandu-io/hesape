package rpc_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

func TestRuntimeDependencyBoundary(t *testing.T) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		t.Fatal("Go build information is unavailable")
	}
	allowed := map[string]bool{
		"connectrpc.com/connect":      true,
		"github.com/arandu-io/hesape": true,
		"google.golang.org/protobuf":  true,
	}
	for _, dependency := range info.Deps {
		if !allowed[dependency.Path] {
			t.Errorf("runtime dependency %s is outside the RPC boundary", dependency.Path)
		}
	}
}

func TestRPCPackageCannotCreateASecondServer(t *testing.T) {
	forbidden := []string{
		"grpc.NewServer",
		"http.Server{",
		"http.ListenAndServe",
		"http.Serve(",
		"net.Listen(",
	}
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, marker := range forbidden {
			if strings.Contains(string(source), marker) {
				t.Errorf("%s contains forbidden second-server constructor %q", path, marker)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
