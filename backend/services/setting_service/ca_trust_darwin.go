//go:build darwin

package settingservice

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"time"
)

func newCACertificateTrustStore() caCertificateTrustStore {
	return securityCACertificateTrustStore{run: runCACertificateSecurity}
}

func runCACertificateSecurity(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "/usr/bin/security", args...)
	// Keep diagnostic matching independent of the app's language. Do not
	// replace the user's environment, which security needs for their session.
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	cmd.WaitDelay = time.Second
	out := &caSecurityOutput{}
	cmd.Stdout, cmd.Stderr = out, out
	err := cmd.Run()
	if out.overflow {
		return nil, errors.New("security output exceeds size limit")
	}
	return out.buffer.Bytes(), err
}
