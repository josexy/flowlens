package pythonpluginservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	inlineHTTPRequestPluginID   = "current-request-script"
	inlineHTTPRequestPluginName = "Current Request Script"
	maxInlinePythonScriptBytes  = 1024 * 1024
)

type diagnosticDetailError struct {
	err    error
	detail string
}

func (e *diagnosticDetailError) Error() string {
	return e.err.Error()
}

func (e *diagnosticDetailError) Unwrap() error {
	return e.err
}

func (e *diagnosticDetailError) DiagnosticDetail() string {
	return e.detail
}

func (m *packageManager) createInlineRevision(
	ctx context.Context,
	executionID string,
	source string,
) (string, *RevisionLease, error) {
	if m == nil {
		return "", nil, errors.New("Python package manager is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	executionID = strings.TrimSpace(executionID)
	if executionID == "" {
		return "", nil, errors.New("Current request Python execution ID cannot be empty")
	}
	if strings.TrimSpace(source) == "" {
		return "", nil, errors.New("Current request Python script cannot be empty")
	}
	if len([]byte(source)) > maxInlinePythonScriptBytes {
		return "", nil, fmt.Errorf("Current request Python script exceeds the %d byte limit", maxInlinePythonScriptBytes)
	}
	if m.validator == nil {
		return "", nil, errors.New("Python revision validator is unavailable")
	}

	digest := sha256.New()
	_, _ = digest.Write([]byte("flowlens-current-request-script-v1\x00"))
	_, _ = digest.Write([]byte(executionID))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(source))
	revision := hex.EncodeToString(digest.Sum(nil))

	revisionPath, err := os.MkdirTemp(m.runtimeRoot, ".current-request-script-*")
	if err != nil {
		return "", nil, fmt.Errorf("create current request Python runtime directory: %w", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(revisionPath)
	}
	if err := atomicWriteManagedFile(filepath.Join(revisionPath, mainFileName), []byte(source)); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := m.validator.ValidateRevision(ctx, RevisionValidationRequest{
		ExecutionID: executionID,
		PluginID:    inlineHTTPRequestPluginID,
		Revision:    revision,
		Path:        revisionPath,
	}); err != nil {
		if _, ok := errors.AsType[interface {
			error
			DiagnosticDetail() string
		}](err); ok {
			err = &diagnosticDetailError{
				err:    err,
				detail: sanitizeRunnerDiagnostic(err, revisionPath),
			}
		}
		cleanup()
		return "", nil, fmt.Errorf("validate current request Python script: %w", err)
	}
	return revision, &RevisionLease{Path: revisionPath, release: cleanup}, nil
}
