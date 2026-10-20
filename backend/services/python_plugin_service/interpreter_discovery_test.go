package pythonpluginservice

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestProbeInterpreterPathCandidatesFiltersDeduplicatesAndSorts(t *testing.T) {
	root := t.TempDir()
	paths := []interpreterPathCandidate{
		{path: filepath.Join(root, "python-old")},
		{path: filepath.Join(root, "python-old-alias"), current: true},
		{path: filepath.Join(root, "python-new")},
		{path: filepath.Join(root, "pypy")},
		{path: filepath.Join(root, "python-too-old")},
		{path: filepath.Join(root, "broken")},
	}
	canonicalOld := filepath.Join(root, "canonical-python-old")
	probe := func(_ context.Context, path string) (InterpreterCandidate, error) {
		switch filepath.Base(path) {
		case "python-old", "python-old-alias":
			return InterpreterCandidate{InterpreterPath: canonicalOld, PythonMajor: 3, PythonMinor: 10, PythonPatch: 9, Implementation: "cpython"}, nil
		case "python-new":
			return InterpreterCandidate{InterpreterPath: path, PythonMajor: 3, PythonMinor: 13, PythonPatch: 2, Implementation: "cpython"}, nil
		case "pypy":
			return InterpreterCandidate{InterpreterPath: path, PythonMajor: 3, PythonMinor: 11, PythonPatch: 1, Implementation: "pypy"}, nil
		case "python-too-old":
			return InterpreterCandidate{InterpreterPath: path, PythonMajor: 2, PythonMinor: 7, PythonPatch: 18, Implementation: "cpython"}, nil
		default:
			return InterpreterCandidate{}, errors.New("probe failed")
		}
	}

	discovered := probeInterpreterPathCandidates(context.Background(), paths, probe)
	if len(discovered) != 2 {
		t.Fatalf("discovered = %#v, want two supported CPython interpreters", discovered)
	}
	if discovered[0].PythonMinor != 13 || discovered[0].Current {
		t.Fatalf("first candidate = %+v, want newest non-current interpreter", discovered[0])
	}
	if discovered[1].InterpreterPath != canonicalOld || !discovered[1].Current {
		t.Fatalf("second candidate = %+v, want deduplicated current interpreter", discovered[1])
	}
}

func TestDiscoverInterpretersIncludesConfiguredCPython(t *testing.T) {
	pythonPath := requirePython3(t)
	service, _ := newServiceHarness(t)
	candidates, err := service.DiscoverInterpreters(pythonPath)
	if err != nil {
		t.Fatalf("DiscoverInterpreters: %v", err)
	}
	for _, candidate := range candidates {
		if candidate.Current && candidate.InterpreterPath != "" && isSupportedInterpreter(candidate) {
			return
		}
	}
	t.Fatalf("candidates = %+v, want the configured CPython marked current", candidates)
}

func TestPathEnvironmentInterpretersIncludesOlderPython3Versions(t *testing.T) {
	root := t.TempDir()
	names := []string{"python3.7", "python3.8", "python3.10"}
	for index, name := range names {
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, nil, 0o700); err != nil {
			t.Fatal(err)
		}
		names[index] = path
	}
	t.Setenv("PATH", root)
	paths := pathEnvironmentInterpreters()
	for _, want := range names {
		found := false
		for _, path := range paths {
			if path == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("PATH candidates = %v, missing %s", paths, want)
		}
	}
}
