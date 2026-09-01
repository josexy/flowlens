package pythonpluginservice

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	interpreterDiscoveryTimeout  = 8 * time.Second
	interpreterProbeTimeout      = 2 * time.Second
	maxInterpreterDiscoveryPaths = 96
	maxInterpreterProbeOutput    = 16 * 1024
	interpreterProbeConcurrency  = 4
)

const interpreterProbeSource = `import json, sys; print(json.dumps({"executable": sys.executable, "implementation": sys.implementation.name, "version": list(sys.version_info[:3])}))`

type InterpreterCandidate struct {
	InterpreterPath string `json:"interpreterPath"`
	PythonMajor     int    `json:"pythonMajor"`
	PythonMinor     int    `json:"pythonMinor"`
	PythonPatch     int    `json:"pythonPatch"`
	Implementation  string `json:"implementation"`
	Current         bool   `json:"current"`
}

type interpreterPathCandidate struct {
	path    string
	current bool
}

type interpreterProbeFunc func(context.Context, string) (InterpreterCandidate, error)

type interpreterProbePayload struct {
	Executable     string `json:"executable"`
	Implementation string `json:"implementation"`
	Version        []int  `json:"version"`
}

func (s *PythonPluginService) DiscoverInterpreters(configuredPath string) ([]InterpreterCandidate, error) {
	parent := s.operationContext()
	if err := parent.Err(); err != nil {
		return nil, fmt.Errorf("Python interpreter discovery is unavailable: %w", err)
	}
	ctx, cancel := context.WithTimeout(parent, interpreterDiscoveryTimeout)
	defer cancel()

	paths := enumerateInterpreterPathCandidates(ctx, configuredPath)
	candidates := probeInterpreterPathCandidates(ctx, paths, probeInterpreter)
	if err := parent.Err(); err != nil {
		return nil, fmt.Errorf("Python interpreter discovery was canceled: %w", err)
	}
	return candidates, nil
}

func enumerateInterpreterPathCandidates(ctx context.Context, configuredPath string) []interpreterPathCandidate {
	paths := make([]interpreterPathCandidate, 0, 16)
	seen := make(map[string]int)
	add := func(path string, current bool) {
		if len(paths) >= maxInterpreterDiscoveryPaths {
			return
		}
		path = strings.Trim(strings.TrimSpace(path), `"`)
		if path == "" || shouldSkipInterpreterDiscoveryPath(path) {
			return
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return
		}
		info, err := os.Stat(absolute)
		if err != nil || !info.Mode().IsRegular() {
			return
		}
		key := interpreterPathKey(absolute)
		if index, ok := seen[key]; ok {
			paths[index].current = paths[index].current || current
			return
		}
		seen[key] = len(paths)
		paths = append(paths, interpreterPathCandidate{path: absolute, current: current})
	}

	add(configuredPath, true)
	for _, path := range environmentInterpreterPaths() {
		add(path, false)
	}
	for _, path := range pathEnvironmentInterpreters() {
		add(path, false)
	}
	for _, path := range platformInterpreterPaths(ctx) {
		add(path, false)
	}
	return paths
}

func environmentInterpreterPaths() []string {
	paths := make([]string, 0, 8)
	for _, key := range []string{"VIRTUAL_ENV", "CONDA_PREFIX"} {
		root := strings.TrimSpace(os.Getenv(key))
		if root == "" {
			continue
		}
		if runtime.GOOS == "windows" {
			paths = append(paths,
				filepath.Join(root, "Scripts", "python.exe"),
				filepath.Join(root, "python.exe"),
			)
		} else {
			paths = append(paths,
				filepath.Join(root, "bin", "python3"),
				filepath.Join(root, "bin", "python"),
			)
		}
	}
	if path := strings.Trim(strings.TrimSpace(os.Getenv("UV_PYTHON")), `"`); filepath.IsAbs(path) {
		paths = append(paths, path)
	}
	return paths
}

func pathEnvironmentInterpreters() []string {
	names := []string{"python3", "python"}
	for minor := 11; minor <= 30; minor++ {
		names = append(names, fmt.Sprintf("python3.%d", minor))
	}
	if runtime.GOOS == "windows" {
		for index := range names {
			names[index] += ".exe"
		}
	}
	paths := make([]string, 0, len(names))
	for _, directory := range filepath.SplitList(os.Getenv("PATH")) {
		directory = strings.Trim(strings.TrimSpace(directory), `"`)
		if directory == "" {
			continue
		}
		for _, name := range names {
			candidate := filepath.Join(directory, name)
			if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
				paths = append(paths, candidate)
			}
		}
	}
	return paths
}

func probeInterpreterPathCandidates(ctx context.Context, paths []interpreterPathCandidate, probe interpreterProbeFunc) []InterpreterCandidate {
	if len(paths) == 0 {
		return []InterpreterCandidate{}
	}
	results := make([]*InterpreterCandidate, len(paths))
	jobs := make(chan int, len(paths))
	for index := range paths {
		jobs <- index
	}
	close(jobs)

	workerCount := min(interpreterProbeConcurrency, len(paths))
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for index := range jobs {
				if ctx.Err() != nil {
					continue
				}
				candidate, err := probe(ctx, paths[index].path)
				if err != nil || !isSupportedInterpreter(candidate) {
					continue
				}
				candidate.Current = paths[index].current
				results[index] = &candidate
			}
		}()
	}
	workers.Wait()

	discovered := make([]InterpreterCandidate, 0, len(results))
	seen := make(map[string]int)
	for _, candidate := range results {
		if candidate == nil {
			continue
		}
		key := interpreterPathKey(candidate.InterpreterPath)
		if index, ok := seen[key]; ok {
			discovered[index].Current = discovered[index].Current || candidate.Current
			continue
		}
		seen[key] = len(discovered)
		discovered = append(discovered, *candidate)
	}
	sort.Slice(discovered, func(left, right int) bool {
		a, b := discovered[left], discovered[right]
		if a.PythonMajor != b.PythonMajor {
			return a.PythonMajor > b.PythonMajor
		}
		if a.PythonMinor != b.PythonMinor {
			return a.PythonMinor > b.PythonMinor
		}
		if a.PythonPatch != b.PythonPatch {
			return a.PythonPatch > b.PythonPatch
		}
		return strings.ToLower(a.InterpreterPath) < strings.ToLower(b.InterpreterPath)
	})
	return discovered
}

func probeInterpreter(ctx context.Context, path string) (InterpreterCandidate, error) {
	resolved, err := validateInterpreterPath(path)
	if err != nil {
		return InterpreterCandidate{}, err
	}
	stdout, stderr, err := runInterpreterDiscoveryCommand(
		ctx,
		interpreterProbeTimeout,
		resolved,
		"-I",
		"-c",
		interpreterProbeSource,
	)
	if err != nil {
		if detail := strings.TrimSpace(stderr); detail != "" {
			return InterpreterCandidate{}, fmt.Errorf("probe Python interpreter: %w: %s", err, detail)
		}
		return InterpreterCandidate{}, fmt.Errorf("probe Python interpreter: %w", err)
	}
	var payload interpreterProbePayload
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &payload); err != nil {
		return InterpreterCandidate{}, fmt.Errorf("decode Python interpreter probe: %w", err)
	}
	if len(payload.Version) < 3 {
		return InterpreterCandidate{}, fmt.Errorf("Python interpreter probe returned an incomplete version")
	}
	actualPath, err := validateInterpreterPath(payload.Executable)
	if err != nil {
		return InterpreterCandidate{}, fmt.Errorf("validate discovered Python interpreter: %w", err)
	}
	candidate := InterpreterCandidate{
		InterpreterPath: actualPath,
		PythonMajor:     payload.Version[0],
		PythonMinor:     payload.Version[1],
		PythonPatch:     payload.Version[2],
		Implementation:  strings.ToLower(strings.TrimSpace(payload.Implementation)),
	}
	if !isSupportedInterpreter(candidate) {
		return InterpreterCandidate{}, fmt.Errorf(
			"unsupported Python interpreter %s %d.%d.%d",
			candidate.Implementation,
			candidate.PythonMajor,
			candidate.PythonMinor,
			candidate.PythonPatch,
		)
	}
	return candidate, nil
}

func runInterpreterDiscoveryCommand(ctx context.Context, timeout time.Duration, path string, args ...string) (string, string, error) {
	commandContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(commandContext, path, args...)
	configureWorkerCommand(command)
	stdout := newTailBuffer(maxInterpreterProbeOutput)
	stderr := newTailBuffer(maxInterpreterProbeOutput)
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	if commandContext.Err() != nil {
		err = commandContext.Err()
	}
	return stdout.String(), stderr.String(), err
}

func isSupportedInterpreter(candidate InterpreterCandidate) bool {
	return strings.EqualFold(candidate.Implementation, "cpython") &&
		candidate.PythonMajor == 3 && candidate.PythonMinor >= 11
}

func interpreterPathKey(path string) string {
	path = filepath.Clean(strings.TrimSpace(path))
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}
