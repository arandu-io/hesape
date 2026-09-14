// Command protogen regenerates the RPC fixture with the toolchain pinned by
// the rpc module.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	modulePath      = "github.com/arandu-io/hesape/rpc"
	protocVersion   = "libprotoc 35.1"
	protoSource     = "internal/proto/fixture/v1/communication.proto"
	generatedPrefix = "internal/gen/fixture/v1"
)

func main() {
	write := flag.Bool("write", false, "write generated files instead of checking them")
	flag.Parse()
	if err := run(*write); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(write bool) error {
	if err := requireProtocVersion(); err != nil {
		return err
	}
	goPlugin, err := toolPath("protoc-gen-go")
	if err != nil {
		return err
	}
	connectPlugin, err := toolPath("protoc-gen-connect-go")
	if err != nil {
		return err
	}

	temporary, err := os.MkdirTemp("", "arandu-rpc-protogen-")
	if err != nil {
		return fmt.Errorf("create generation directory: %w", err)
	}
	defer os.RemoveAll(temporary)

	arguments := []string{
		"--plugin=protoc-gen-go=" + goPlugin,
		"--plugin=protoc-gen-connect-go=" + connectPlugin,
		"--go_out=" + temporary,
		"--go_opt=module=" + modulePath,
		"--connect-go_out=" + temporary,
		"--connect-go_opt=module=" + modulePath,
		protoSource,
	}
	command := exec.Command("protoc", arguments...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("generate protobuf: %w", err)
	}

	generated := filepath.Join(temporary, generatedPrefix)
	freshFiles := make(map[string]struct{})
	err = filepath.WalkDir(generated, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(temporary, path)
		if err != nil {
			return err
		}
		target := filepath.Clean(relative)
		freshFiles[target] = struct{}{}
		fresh, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if write {
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			return os.WriteFile(target, fresh, 0o644)
		}
		current, err := os.ReadFile(target)
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("generated file is missing: %s", target)
		}
		if err != nil {
			return err
		}
		if !bytes.Equal(current, fresh) {
			return fmt.Errorf("generated file is stale: %s", target)
		}
		return nil
	})
	if err != nil || write {
		return err
	}
	return filepath.WalkDir(generatedPrefix, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if _, ok := freshFiles[filepath.Clean(path)]; !ok {
			return fmt.Errorf("generated file is obsolete: %s", path)
		}
		return nil
	})
}

func requireProtocVersion() error {
	output, err := exec.Command("protoc", "--version").Output()
	if err != nil {
		return fmt.Errorf("read protoc version: %w", err)
	}
	got := strings.TrimSpace(string(output))
	if got != protocVersion {
		return fmt.Errorf("protoc version is %q, want %q", got, protocVersion)
	}
	return nil
}

func toolPath(name string) (string, error) {
	output, err := exec.Command("go", "tool", "-n", name).Output()
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", name, err)
	}
	path := strings.TrimSpace(string(output))
	if path == "" {
		return "", fmt.Errorf("resolve %s: empty tool path", name)
	}
	return path, nil
}
