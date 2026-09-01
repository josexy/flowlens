package pythonpluginservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	workerProtocolVersion = 1
	workerSDKAPIVersion   = 1
	defaultWorkerCount    = 2
	maxSharedStateBytes   = 1024 * 1024
	maxWorkerStderrBytes  = 64 * 1024
)

type WorkerHello struct {
	ProtocolVersion int    `json:"protocolVersion"`
	SDKAPIVersion   int    `json:"sdkApiVersion"`
	PythonMajor     int    `json:"pythonMajor"`
	PythonMinor     int    `json:"pythonMinor"`
	PythonPatch     int    `json:"pythonPatch"`
	Implementation  string `json:"implementation"`
}

type WorkerLog struct {
	RequestID   string `json:"requestId"`
	ExecutionID string `json:"executionId"`
	PluginID    string `json:"pluginId"`
	Level       string `json:"level"`
	Stream      string `json:"stream"`
	Message     string `json:"message"`
	Timestamp   int64  `json:"timestamp"`
}

type WorkerLogSink func(WorkerLog)

type InvokeRequest struct {
	ExecutionID     string
	PluginID        string
	PluginName      string
	Revision        string
	Path            string
	Hook            string
	OutputDirectory string
	Context         map[string]any
	Value           any
}

type InvokeResult struct {
	Blocked     bool
	Transformed bool
	BodyChanged bool
	Value       json.RawMessage
	Shared      json.RawMessage
}

type PythonExecutionError struct {
	Code      string
	Message   string
	Traceback string
}

func (e *PythonExecutionError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

// DiagnosticDetail exposes the traceback to user-facing request plugin diagnostics
// without changing Error(), which remains concise for logs and validation state.
func (e *PythonExecutionError) DiagnosticDetail() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Traceback) != "" {
		return e.Traceback
	}
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	return e.Error()
}

type workerPoolConfig struct {
	InterpreterPath string
	RuntimeRoot     string
	Size            int
	LogSink         WorkerLogSink
}

type workerPool struct {
	config  workerPoolConfig
	runtime extractedRuntime

	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	closed   bool
	starting int
	workers  map[*pythonWorker]struct{}
	idle     chan *pythonWorker
	slots    chan struct{}
	wg       sync.WaitGroup
}

type pythonWorker struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  *tailBuffer
	tree    workerProcessTree
	hello   WorkerHello
	logSink WorkerLogSink

	waitMu   sync.Mutex
	waitErr  error
	waitDone chan struct{}
	stopOnce sync.Once
}

type workerRoundTrip struct {
	message workerMessage
	err     error
	fatal   bool
}

func newWorkerPool(config workerPoolConfig) (*workerPool, error) {
	interpreterPath, err := validateInterpreterPath(config.InterpreterPath)
	if err != nil {
		return nil, err
	}
	config.InterpreterPath = interpreterPath
	if config.Size <= 0 {
		config.Size = defaultWorkerCount
	}
	if config.Size > 32 {
		config.Size = 32
	}
	runtimeFiles, err := extractWorkerRuntime(config.RuntimeRoot)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &workerPool{
		config: config, runtime: runtimeFiles, ctx: ctx, cancel: cancel,
		workers: make(map[*pythonWorker]struct{}),
		idle:    make(chan *pythonWorker, config.Size),
		slots:   make(chan struct{}, config.Size),
	}, nil
}

func validateInterpreterPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("Python interpreter path cannot be empty")
	}
	if !filepath.IsAbs(value) {
		return "", errors.New("Python interpreter path must be absolute")
	}
	resolved, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve Python interpreter path: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("inspect Python interpreter: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("Python interpreter path must reference a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return "", errors.New("Python interpreter path is not executable")
	}
	return resolved, nil
}

func (p *workerPool) ValidateRevision(ctx context.Context, request RevisionValidationRequest) error {
	message := map[string]any{
		"type":        "validate",
		"requestId":   uuid.NewString(),
		"executionId": request.ExecutionID,
		"pluginId":    request.PluginID,
		"revision":    request.Revision,
		"path":        request.Path,
	}
	result, err := p.roundTrip(ctx, message)
	if err != nil {
		return err
	}
	if !result.Validated {
		return errors.New("Python worker did not validate the plugin revision")
	}
	return nil
}

func (p *workerPool) Invoke(ctx context.Context, request InvokeRequest) (InvokeResult, error) {
	if request.Hook != "onRequest" && request.Hook != "onResponse" {
		return InvokeResult{}, fmt.Errorf("unsupported Python hook %q", request.Hook)
	}
	if err := validateSharedValue(request.Context); err != nil {
		return InvokeResult{}, err
	}
	requestID := uuid.NewString()
	message := map[string]any{
		"type":            "invoke",
		"requestId":       requestID,
		"executionId":     request.ExecutionID,
		"pluginId":        request.PluginID,
		"pluginName":      request.PluginName,
		"revision":        request.Revision,
		"path":            request.Path,
		"hook":            request.Hook,
		"outputDirectory": request.OutputDirectory,
		"context":         request.Context,
		"value":           request.Value,
	}
	result, err := p.roundTrip(ctx, message)
	if err != nil {
		return InvokeResult{}, err
	}
	if len(result.Shared) == 0 {
		result.Shared = json.RawMessage(`{}`)
	}
	if err := validateSharedJSON(result.Shared); err != nil {
		return InvokeResult{}, err
	}
	return InvokeResult{
		Blocked: result.Blocked, Transformed: result.Transformed, BodyChanged: result.BodyChanged,
		Value:  append(json.RawMessage(nil), result.Value...),
		Shared: append(json.RawMessage(nil), result.Shared...),
	}, nil
}

func (p *workerPool) Probe(ctx context.Context) (WorkerHello, error) {
	worker, release, err := p.acquire(ctx)
	if err != nil {
		return WorkerHello{}, err
	}
	release(true)
	return worker.hello, nil
}

func (p *workerPool) roundTrip(ctx context.Context, request any) (workerMessage, error) {
	worker, release, err := p.acquire(ctx)
	if err != nil {
		return workerMessage{}, err
	}
	requestID, err := protocolRequestID(request)
	if err != nil {
		release(true)
		return workerMessage{}, err
	}
	result := worker.do(ctx, requestID, request)
	release(!result.fatal)
	return result.message, result.err
}

func (p *workerPool) acquire(ctx context.Context) (*pythonWorker, func(bool), error) {
	operationContext, cancelOperation := mergeWorkerContexts(ctx, p.ctx)
	select {
	case p.slots <- struct{}{}:
	case <-operationContext.Done():
		cancelOperation()
		return nil, nil, operationContext.Err()
	}
	p.wg.Add(1)
	released := false
	var releaseMu sync.Mutex
	releaseSlot := func() {
		releaseMu.Lock()
		defer releaseMu.Unlock()
		if released {
			return
		}
		released = true
		<-p.slots
		p.wg.Done()
		cancelOperation()
	}

	for {
		select {
		case worker := <-p.idle:
			return worker, func(healthy bool) {
				p.releaseWorker(worker, healthy)
				releaseSlot()
			}, nil
		default:
		}

		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			releaseSlot()
			return nil, nil, errors.New("Python worker pool is closed")
		}
		if len(p.workers)+p.starting < p.config.Size {
			p.starting++
			p.mu.Unlock()
			worker, err := startPythonWorker(operationContext, p.config.InterpreterPath, p.runtime, p.config.LogSink)
			p.mu.Lock()
			p.starting--
			if err == nil && !p.closed {
				p.workers[worker] = struct{}{}
			}
			closed := p.closed
			p.mu.Unlock()
			if err != nil {
				releaseSlot()
				return nil, nil, err
			}
			if closed {
				worker.terminate()
				releaseSlot()
				return nil, nil, errors.New("Python worker pool is closed")
			}
			return worker, func(healthy bool) {
				p.releaseWorker(worker, healthy)
				releaseSlot()
			}, nil
		}
		p.mu.Unlock()

		select {
		case worker := <-p.idle:
			return worker, func(healthy bool) {
				p.releaseWorker(worker, healthy)
				releaseSlot()
			}, nil
		case <-operationContext.Done():
			releaseSlot()
			return nil, nil, operationContext.Err()
		}
	}
}

func (p *workerPool) releaseWorker(worker *pythonWorker, healthy bool) {
	if worker == nil {
		return
	}
	p.mu.Lock()
	if !healthy {
		delete(p.workers, worker)
	}
	closed := p.closed
	p.mu.Unlock()
	if !healthy || closed {
		worker.terminate()
		if closed {
			p.mu.Lock()
			delete(p.workers, worker)
			p.mu.Unlock()
		}
		return
	}
	select {
	case p.idle <- worker:
	case <-p.ctx.Done():
		worker.terminate()
		p.mu.Lock()
		delete(p.workers, worker)
		p.mu.Unlock()
	}
}

func (p *workerPool) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.cancel()
	workers := make([]*pythonWorker, 0, len(p.workers))
	for worker := range p.workers {
		workers = append(workers, worker)
	}
	p.mu.Unlock()
	for _, worker := range workers {
		worker.terminate()
	}
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func startPythonWorker(ctx context.Context, interpreterPath string, runtimeFiles extractedRuntime, logSink WorkerLogSink) (*pythonWorker, error) {
	command := exec.Command(interpreterPath, "-I", "-u", runtimeFiles.BootstrapPath, runtimeFiles.Root)
	command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONIOENCODING=utf-8")
	configureWorkerCommand(command)
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open Python worker stdin: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("open Python worker stdout: %w", err)
	}
	stderrPipe, err := command.StderrPipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("open Python worker stderr: %w", err)
	}
	if err := command.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		stderrPipe.Close()
		return nil, fmt.Errorf("start Python worker: %w", err)
	}
	worker := &pythonWorker{
		command: command, stdin: stdin, stdout: stdout, stderr: newTailBuffer(maxWorkerStderrBytes),
		tree: attachWorkerProcessTree(command), logSink: logSink, waitDone: make(chan struct{}),
	}
	go func() {
		_, _ = io.Copy(worker.stderr, stderrPipe)
		_ = stderrPipe.Close()
	}()
	go func() {
		err := command.Wait()
		worker.waitMu.Lock()
		worker.waitErr = err
		worker.waitMu.Unlock()
		close(worker.waitDone)
	}()

	messageResult := make(chan workerRoundTrip, 1)
	go func() {
		message, err := readProtocolFrame(stdout)
		messageResult <- workerRoundTrip{message: message, err: err, fatal: err != nil}
	}()
	select {
	case result := <-messageResult:
		if result.err != nil {
			worker.terminate()
			return nil, worker.startError(result.err)
		}
		hello, err := validateWorkerHello(result.message)
		if err != nil {
			worker.terminate()
			return nil, err
		}
		worker.hello = hello
		return worker, nil
	case <-ctx.Done():
		worker.terminate()
		return nil, ctx.Err()
	case <-worker.waitDone:
		worker.terminate()
		return nil, worker.startError(errors.New("Python worker exited before handshake"))
	}
}

func validateWorkerHello(message workerMessage) (WorkerHello, error) {
	if message.Type != "hello" {
		return WorkerHello{}, fmt.Errorf("Python worker sent %q before hello", message.Type)
	}
	if message.ProtocolVersion != workerProtocolVersion {
		return WorkerHello{}, fmt.Errorf("unsupported Python worker protocol version %d", message.ProtocolVersion)
	}
	if message.SDKAPIVersion != workerSDKAPIVersion {
		return WorkerHello{}, fmt.Errorf("unsupported FlowLens Python SDK API version %d", message.SDKAPIVersion)
	}
	if len(message.PythonVersion) < 2 {
		return WorkerHello{}, errors.New("Python worker hello is missing version information")
	}
	implementation := strings.ToLower(strings.TrimSpace(message.Implementation))
	if implementation != "cpython" {
		return WorkerHello{}, fmt.Errorf("CPython is required; worker reported %q", message.Implementation)
	}
	major, minor, patch := message.PythonVersion[0], message.PythonVersion[1], 0
	if len(message.PythonVersion) > 2 {
		patch = message.PythonVersion[2]
	}
	if major != 3 || minor < 11 {
		return WorkerHello{}, fmt.Errorf("Python 3.11 or newer is required; worker reported %d.%d.%d", major, minor, patch)
	}
	return WorkerHello{
		ProtocolVersion: message.ProtocolVersion,
		SDKAPIVersion:   message.SDKAPIVersion,
		PythonMajor:     major,
		PythonMinor:     minor,
		PythonPatch:     patch,
		Implementation:  implementation,
	}, nil
}

func (w *pythonWorker) do(ctx context.Context, requestID string, request any) workerRoundTrip {
	resultChannel := make(chan workerRoundTrip, 1)
	go func() {
		if err := writeProtocolFrame(w.stdin, request); err != nil {
			resultChannel <- workerRoundTrip{err: fmt.Errorf("send Python worker request: %w", err), fatal: true}
			return
		}
		for {
			message, err := readProtocolFrame(w.stdout)
			if err != nil {
				resultChannel <- workerRoundTrip{err: fmt.Errorf("receive Python worker response: %w", err), fatal: true}
				return
			}
			switch message.Type {
			case "log":
				if w.logSink != nil {
					w.logSink(WorkerLog{
						RequestID: message.RequestID, ExecutionID: message.ExecutionID,
						PluginID: message.PluginID, Level: message.Level, Stream: message.Stream,
						Message: message.Message, Timestamp: message.Timestamp,
					})
				}
			case "result":
				if message.RequestID != requestID {
					resultChannel <- workerRoundTrip{err: fmt.Errorf("Python worker response ID %q does not match request %q", message.RequestID, requestID), fatal: true}
					return
				}
				resultChannel <- workerRoundTrip{message: message}
				return
			case "error":
				if message.RequestID != requestID {
					resultChannel <- workerRoundTrip{err: fmt.Errorf("Python worker error ID %q does not match request %q", message.RequestID, requestID), fatal: true}
					return
				}
				fatal := message.Code == "protocol_error"
				resultChannel <- workerRoundTrip{err: &PythonExecutionError{
					Code: message.Code, Message: message.Message, Traceback: message.Traceback,
				}, fatal: fatal}
				return
			default:
				resultChannel <- workerRoundTrip{err: fmt.Errorf("unexpected Python worker message type %q", message.Type), fatal: true}
				return
			}
		}
	}()
	select {
	case result := <-resultChannel:
		return result
	case <-ctx.Done():
		w.terminate()
		return workerRoundTrip{err: ctx.Err(), fatal: true}
	case <-w.waitDone:
		return workerRoundTrip{err: w.startError(errors.New("Python worker exited during request")), fatal: true}
	}
}

func (w *pythonWorker) startError(cause error) error {
	stderr := strings.TrimSpace(w.stderr.String())
	w.waitMu.Lock()
	waitErr := w.waitErr
	w.waitMu.Unlock()
	if waitErr != nil {
		cause = fmt.Errorf("%w: %v", cause, waitErr)
	}
	if stderr != "" {
		return fmt.Errorf("%w; stderr: %s", cause, stderr)
	}
	return cause
}

func (w *pythonWorker) terminate() {
	if w == nil {
		return
	}
	w.stopOnce.Do(func() {
		_ = w.stdin.Close()
		terminateWorkerProcessTree(w.command, w.tree)
		select {
		case <-w.waitDone:
		case <-time.After(5 * time.Second):
			if w.command != nil && w.command.Process != nil {
				_ = w.command.Process.Kill()
			}
		}
		_ = w.stdout.Close()
	})
}

func protocolRequestID(request any) (string, error) {
	value, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("encode Python worker request: %w", err)
	}
	if len(value) > maxProtocolFrameBytes {
		return "", errProtocolFrameTooLarge
	}
	var envelope struct {
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(value, &envelope); err != nil {
		return "", fmt.Errorf("decode Python worker request envelope: %w", err)
	}
	if strings.TrimSpace(envelope.RequestID) == "" {
		return "", errors.New("Python worker request ID cannot be empty")
	}
	return envelope.RequestID, nil
}

func validateSharedValue(contextValue map[string]any) error {
	if contextValue == nil {
		return errors.New("Python hook context cannot be nil")
	}
	shared, ok := contextValue["shared"]
	if !ok || shared == nil {
		contextValue["shared"] = map[string]any{}
		return nil
	}
	value, err := json.Marshal(shared)
	if err != nil {
		return fmt.Errorf("encode context.shared: %w", err)
	}
	return validateSharedJSON(value)
}

func validateSharedJSON(value []byte) error {
	if len(value) > maxSharedStateBytes {
		return fmt.Errorf("context.shared exceeds %d bytes", maxSharedStateBytes)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(value, &decoded); err != nil {
		return fmt.Errorf("context.shared must be a JSON object: %w", err)
	}
	if decoded == nil {
		return errors.New("context.shared must be a JSON object")
	}
	return nil
}

func mergeWorkerContexts(callContext, poolContext context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(callContext)
	stop := context.AfterFunc(poolContext, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

type tailBuffer struct {
	mu       sync.Mutex
	value    []byte
	capacity int
}

func newTailBuffer(capacity int) *tailBuffer {
	return &tailBuffer{capacity: capacity}
}

func (b *tailBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	originalLength := len(value)
	if len(value) >= b.capacity {
		b.value = append(b.value[:0], value[len(value)-b.capacity:]...)
		return originalLength, nil
	}
	if overflow := len(b.value) + len(value) - b.capacity; overflow > 0 {
		copy(b.value, b.value[overflow:])
		b.value = b.value[:len(b.value)-overflow]
	}
	b.value = append(b.value, value...)
	return originalLength, nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(append([]byte(nil), b.value...))
}
