// Command buildcheck verifies executable compilation, without packaging or
// running foreign binaries. Outputs live in an automatically cleaned temp dir.
package main

import (
	"bytes"
	"debug/macho"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	targets := flag.String("targets", "windows/amd64,windows/arm64,linux/amd64,linux/arm64,darwin/arm64,darwin/amd64", "comma-separated compile targets")
	flag.Parse()
	if err := checkTargets(strings.Split(*targets, ",")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func checkTargets(targets []string) error {
	dir, err := os.MkdirTemp("", "flowlens-buildcheck-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	for _, target := range targets {
		goos, arch, ok := strings.Cut(target, "/")
		if !ok || (goos != "windows" && goos != "linux" && goos != "darwin") || (arch != "amd64" && arch != "arm64") {
			return fmt.Errorf("unsupported target %q", target)
		}
		env := targetEnvironment(goos, arch)
		for _, tags := range []string{"", "mygo_noinspector"} {
			fmt.Printf("Compile %s tags=%q CGO_ENABLED=0\n", target, tags)
			list := exec.Command("go", "list", "-deps", "-json", "-tags="+tags, ".")
			list.Env = env
			list.Stderr = os.Stderr
			output, err := list.Output()
			if err != nil {
				return fmt.Errorf("%s dependencies: %w", target, err)
			}
			decoder := json.NewDecoder(bytes.NewReader(output))
			for {
				var pkg struct {
					ImportPath string
					CgoFiles   []string
				}
				if err := decoder.Decode(&pkg); errors.Is(err, io.EOF) {
					break
				} else if err != nil {
					return err
				}
				if len(pkg.CgoFiles) != 0 {
					return fmt.Errorf("%s selects cgo in %s: %v", target, pkg.ImportPath, pkg.CgoFiles)
				}
			}
			binary := filepath.Join(dir, goos+"-"+arch+"-"+tags)
			args := []string{"build", "-tags=" + tags, "-o", binary}
			if goos == "darwin" {
				args = append(args, "-ldflags=-macos=13.0")
			}
			args = append(args, ".")
			build := exec.Command("go", args...)
			build.Env, build.Stdout, build.Stderr = env, os.Stdout, os.Stderr
			if err := build.Run(); err != nil {
				return fmt.Errorf("%s build: %w", target, err)
			}
			if goos == "darwin" {
				if err := checkMachOMinimum(binary); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func targetEnvironment(goos, arch string) []string {
	env := make([]string, 0, len(os.Environ())+3)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(key) {
		case "GOOS", "GOARCH", "CGO_ENABLED":
			continue
		}
		env = append(env, entry)
	}
	return append(env, "GOOS="+goos, "GOARCH="+arch, "CGO_ENABLED=0")
}

func checkMachOMinimum(path string) error {
	file, err := macho.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	for _, load := range file.Loads {
		raw := load.Raw()
		if len(raw) < 16 {
			continue
		}
		var version uint32
		switch file.ByteOrder.Uint32(raw) {
		case 0x32:
			version = file.ByteOrder.Uint32(raw[12:]) // LC_BUILD_VERSION
		case 0x24:
			version = file.ByteOrder.Uint32(raw[8:]) // LC_VERSION_MIN_MACOSX
		default:
			continue
		}
		if version != 13<<16 {
			return fmt.Errorf("%s has macOS deployment version %#x, want 13.0", path, version)
		}
		return nil
	}
	return fmt.Errorf("%s has no Mach-O deployment information", path)
}
