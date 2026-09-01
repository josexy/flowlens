package pythonpluginservice

import (
	"context"
	"errors"
	"path/filepath"
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
			return InterpreterCandidate{InterpreterPath: canonicalOld, PythonMajor: 3, PythonMinor: 11, PythonPatch: 9, Implementation: "cpython"}, nil
		case "python-new":
			return InterpreterCandidate{InterpreterPath: path, PythonMajor: 3, PythonMinor: 13, PythonPatch: 2, Implementation: "cpython"}, nil
		case "pypy":
			return InterpreterCandidate{InterpreterPath: path, PythonMajor: 3, PythonMinor: 11, PythonPatch: 1, Implementation: "pypy"}, nil
		case "python-too-old":
			return InterpreterCandidate{InterpreterPath: path, PythonMajor: 3, PythonMinor: 10, PythonPatch: 9, Implementation: "cpython"}, nil
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
	pythonPath := requirePython311(t)
	service, _ := newServiceHarness(t)
	candidates, err := service.DiscoverInterpreters(pythonPath)
	if err != nil {
		t.Fatalf("DiscoverInterpreters: %v", err)
	}
	for _, candidate := range candidates {
		if candidate.Current && candidate.InterpreterPath != "" && candidate.Implementation == "cpython" && candidate.PythonMajor == 3 && candidate.PythonMinor >= 11 {
			return
		}
	}
	t.Fatalf("candidates = %+v, want the configured CPython marked current", candidates)
}
