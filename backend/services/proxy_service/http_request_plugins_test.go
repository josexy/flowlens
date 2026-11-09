package proxyservice

import (
	"fmt"
	"strings"
	"testing"
)

type detailedHTTPRequestPluginError struct {
	message string
	detail  string
}

func (e *detailedHTTPRequestPluginError) Error() string {
	return e.message
}

func (e *detailedHTTPRequestPluginError) DiagnosticDetail() string {
	return e.detail
}

func TestCleanHTTPRequestPluginDiagnosticMessagePreservesDetailedError(t *testing.T) {
	detail := "Traceback (most recent call last):\n" + strings.Repeat("x", 3000) +
		"\nSyntaxError: invalid syntax"
	err := fmt.Errorf("validate current request Python script: %w", &detailedHTTPRequestPluginError{
		message: "validation_failed: invalid syntax",
		detail:  detail,
	})

	if got := cleanHTTPRequestPluginDiagnosticMessage(err); got != detail {
		t.Fatalf("diagnostic = %q, want complete detail", got)
	}
}
