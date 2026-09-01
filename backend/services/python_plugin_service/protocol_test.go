package pythonpluginservice

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProtocolFrameRoundTripAndMalformedInput(t *testing.T) {
	var buffer bytes.Buffer
	input := map[string]any{
		"type":      "result",
		"requestId": "request-1",
		"blocked":   true,
		"shared":    map[string]any{"value": "你好"},
	}
	if err := writeProtocolFrame(&buffer, input); err != nil {
		t.Fatalf("writeProtocolFrame: %v", err)
	}
	message, err := readProtocolFrame(&buffer)
	if err != nil {
		t.Fatalf("readProtocolFrame: %v", err)
	}
	if message.Type != "result" || message.RequestID != "request-1" || !message.Blocked || !strings.Contains(string(message.Shared), "你好") {
		t.Fatalf("decoded message = %+v", message)
	}

	for name, value := range map[string][]byte{
		"truncated header": {0, 0, 0},
		"truncated body":   {0, 0, 0, 5, '{'},
		"malformed JSON":   {0, 0, 0, 1, '{'},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := readProtocolFrame(bytes.NewReader(value)); err == nil {
				t.Fatal("malformed frame was accepted")
			}
		})
	}
}

func TestProtocolFrameRejectsOversizedLengthBeforeAllocation(t *testing.T) {
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], maxProtocolFrameBytes+1)
	if _, err := readProtocolFrame(bytes.NewReader(header[:])); !errors.Is(err, errProtocolFrameTooLarge) {
		t.Fatalf("oversized frame error = %v", err)
	}
}

func TestValidateWorkerHelloRejectsVersionMismatches(t *testing.T) {
	valid := workerMessage{
		Type: "hello", ProtocolVersion: 1, SDKAPIVersion: 1,
		PythonVersion: []int{3, 11, 0}, Implementation: "cpython",
	}
	if _, err := validateWorkerHello(valid); err != nil {
		t.Fatalf("valid hello: %v", err)
	}
	for name, mutate := range map[string]func(*workerMessage){
		"message type":     func(value *workerMessage) { value.Type = "result" },
		"protocol version": func(value *workerMessage) { value.ProtocolVersion = 2 },
		"SDK version":      func(value *workerMessage) { value.SDKAPIVersion = 2 },
		"Python version":   func(value *workerMessage) { value.PythonVersion = []int{3, 10, 9} },
		"implementation":   func(value *workerMessage) { value.Implementation = "pypy" },
	} {
		t.Run(name, func(t *testing.T) {
			value := valid
			mutate(&value)
			if _, err := validateWorkerHello(value); err == nil {
				t.Fatal("invalid hello was accepted")
			}
		})
	}
}

func TestExtractWorkerRuntimeIsVersionedAndIdempotent(t *testing.T) {
	root := t.TempDir()
	first, err := extractWorkerRuntime(root)
	if err != nil {
		t.Fatalf("first extraction: %v", err)
	}
	second, err := extractWorkerRuntime(root)
	if err != nil {
		t.Fatalf("second extraction: %v", err)
	}
	if first != second || !strings.HasPrefix(first.Version, "v1-") {
		t.Fatalf("runtime extraction mismatch: first=%+v second=%+v", first, second)
	}
}

func TestExtractWorkerRuntimeIsSafeForConcurrentCallers(t *testing.T) {
	root := t.TempDir()
	const callers = 8
	results := make([]extractedRuntime, callers)
	errorsByCaller := make([]error, callers)
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(callers)
	for index := range callers {
		go func() {
			defer workers.Done()
			<-start
			results[index], errorsByCaller[index] = extractWorkerRuntime(root)
		}()
	}
	close(start)
	workers.Wait()

	for index, err := range errorsByCaller {
		if err != nil {
			t.Fatalf("caller %d: %v", index, err)
		}
		if results[index] != results[0] {
			t.Fatalf("caller %d result = %+v, want %+v", index, results[index], results[0])
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "_sdk"))
	if err != nil {
		t.Fatalf("read SDK root: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != results[0].Version {
		t.Fatalf("SDK entries = %#v, want only %q", entries, results[0].Version)
	}
}

func TestCommitWorkerRuntimeRetriesRenameFailure(t *testing.T) {
	root := t.TempDir()
	stageRoot := filepath.Join(root, ".stage.tmp")
	finalRoot := filepath.Join(root, "v1-test")
	if err := os.Mkdir(stageRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stageRoot, ".complete"), []byte("v1-test\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	attempts := 0
	waits := make([]time.Duration, 0)
	err := commitWorkerRuntime(
		stageRoot,
		finalRoot,
		filepath.Join(finalRoot, ".complete"),
		"v1-test",
		func(oldPath, newPath string) error {
			attempts++
			if attempts < 3 {
				return &os.LinkError{Op: "rename", Old: oldPath, New: newPath, Err: os.ErrPermission}
			}
			return os.Rename(oldPath, newPath)
		},
		func(delay time.Duration) { waits = append(waits, delay) },
	)
	if err != nil {
		t.Fatalf("commitWorkerRuntime: %v", err)
	}
	if attempts != 3 || len(waits) != 2 || waits[0] <= 0 || waits[1] <= waits[0] {
		t.Fatalf("attempts = %d, waits = %v", attempts, waits)
	}
	if value, err := os.ReadFile(filepath.Join(finalRoot, ".complete")); err != nil || strings.TrimSpace(string(value)) != "v1-test" {
		t.Fatalf("committed marker = %q, err=%v", value, err)
	}
}
