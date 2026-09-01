package pythonpluginservice

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

//go:embed runtime/bootstrap.py runtime/flowlens/__init__.py
var embeddedWorkerRuntime embed.FS

const (
	workerRuntimeCommitAttempts     = 8
	workerRuntimeCommitInitialDelay = 10 * time.Millisecond
	workerRuntimeCommitMaximumDelay = 100 * time.Millisecond
)

// Runtime probes and configuration updates can create pools concurrently.
// Keep extraction single-writer within the process while allowing every pool to reuse the result.
var workerRuntimeExtractionMu sync.Mutex

type extractedRuntime struct {
	Root          string
	BootstrapPath string
	Version       string
}

func extractWorkerRuntime(runtimeRoot string) (extractedRuntime, error) {
	entries := make([]string, 0)
	err := fs.WalkDir(embeddedWorkerRuntime, "runtime", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			entries = append(entries, path)
		}
		return nil
	})
	if err != nil {
		return extractedRuntime{}, fmt.Errorf("enumerate embedded Python worker runtime: %w", err)
	}
	sort.Strings(entries)
	hash := sha256.New()
	for _, path := range entries {
		value, err := embeddedWorkerRuntime.ReadFile(path)
		if err != nil {
			return extractedRuntime{}, fmt.Errorf("read embedded Python worker runtime %q: %w", path, err)
		}
		_, _ = hash.Write([]byte(path))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(value)
		_, _ = hash.Write([]byte{0})
	}
	version := "v1-" + hex.EncodeToString(hash.Sum(nil))[:16]
	workerRuntimeExtractionMu.Lock()
	defer workerRuntimeExtractionMu.Unlock()

	sdkRoot := filepath.Join(runtimeRoot, "_sdk")
	finalRoot := filepath.Join(sdkRoot, version)
	markerPath := filepath.Join(finalRoot, ".complete")
	if workerRuntimeMarkerMatches(markerPath, version) {
		return extractedRuntime{Root: finalRoot, BootstrapPath: filepath.Join(finalRoot, "bootstrap.py"), Version: version}, nil
	}
	if err := os.MkdirAll(sdkRoot, 0o700); err != nil {
		return extractedRuntime{}, fmt.Errorf("create Python worker runtime root: %w", err)
	}
	stageRoot := filepath.Join(sdkRoot, "."+version+"--"+uuid.NewString()+".tmp")
	if err := os.Mkdir(stageRoot, 0o700); err != nil {
		return extractedRuntime{}, fmt.Errorf("create Python worker runtime staging directory: %w", err)
	}
	defer os.RemoveAll(stageRoot)
	for _, embeddedPath := range entries {
		value, err := embeddedWorkerRuntime.ReadFile(embeddedPath)
		if err != nil {
			return extractedRuntime{}, fmt.Errorf("read embedded Python worker runtime %q: %w", embeddedPath, err)
		}
		relative := strings.TrimPrefix(filepath.ToSlash(embeddedPath), "runtime/")
		destination := filepath.Join(stageRoot, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return extractedRuntime{}, fmt.Errorf("create Python worker runtime directory: %w", err)
		}
		if err := os.WriteFile(destination, value, 0o600); err != nil {
			return extractedRuntime{}, fmt.Errorf("extract Python worker runtime %q: %w", relative, err)
		}
	}
	if err := os.WriteFile(filepath.Join(stageRoot, ".complete"), []byte(version+"\n"), 0o600); err != nil {
		return extractedRuntime{}, fmt.Errorf("write Python worker runtime marker: %w", err)
	}
	if err := commitWorkerRuntime(stageRoot, finalRoot, markerPath, version, os.Rename, time.Sleep); err != nil {
		return extractedRuntime{}, err
	}
	return extractedRuntime{Root: finalRoot, BootstrapPath: filepath.Join(finalRoot, "bootstrap.py"), Version: version}, nil
}

type workerRuntimeRenameFunc func(string, string) error
type workerRuntimeWaitFunc func(time.Duration)

func commitWorkerRuntime(
	stageRoot string,
	finalRoot string,
	markerPath string,
	version string,
	rename workerRuntimeRenameFunc,
	wait workerRuntimeWaitFunc,
) error {
	delay := workerRuntimeCommitInitialDelay
	var commitErr error
	for attempt := range workerRuntimeCommitAttempts {
		// Another process may have completed the same content-addressed runtime.
		if workerRuntimeMarkerMatches(markerPath, version) {
			return nil
		}
		commitErr = rename(stageRoot, finalRoot)
		if commitErr == nil || workerRuntimeMarkerMatches(markerPath, version) {
			return nil
		}
		if attempt+1 >= workerRuntimeCommitAttempts {
			break
		}
		wait(delay)
		delay *= 2
		if delay > workerRuntimeCommitMaximumDelay {
			delay = workerRuntimeCommitMaximumDelay
		}
	}
	return fmt.Errorf("commit Python worker runtime: %w", commitErr)
}

func workerRuntimeMarkerMatches(markerPath, version string) bool {
	value, err := os.ReadFile(markerPath)
	return err == nil && strings.TrimSpace(string(value)) == version
}
