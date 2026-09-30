package settingservice

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/josexy/flowlens/backend/pkg/logger"
)

const (
	caSecurityQueryTimeout  = 15 * time.Second
	caSecurityActionTimeout = 2 * time.Minute
	caSecurityOutputLimit   = 8 << 20
)

// The command adapter is portable so tests can exercise macOS behavior using a
// fake runner on every platform, without writing to a real trust store.
type caSecurityRunner func(context.Context, ...string) ([]byte, error)

type securityCACertificateTrustStore struct {
	run caSecurityRunner
}

func (securityCACertificateTrustStore) supported() bool { return true }

func (s securityCACertificateTrustStore) command(timeout time.Duration, args ...string) ([]byte, error) {
	if len(args) == 0 {
		err := errors.New("security command arguments are empty")
		logger.G().Warnf("CA trust security command rejected: error=%v", err)
		return nil, err
	}
	command := args[0]
	started := time.Now()
	logger.G().Debugf("CA trust security command started: command=%s timeout=%s", command, timeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := s.run(ctx, args...)
	duration := time.Since(started)
	if ctx.Err() != nil {
		wrapped := fmt.Errorf("ca_trust_timeout: %w", ctx.Err())
		logger.G().Warnf("CA trust security command timed out: command=%s timeout=%s duration=%s output_bytes=%d output=%q error=%v", command, timeout, duration, len(out), caTrustLogText(string(out)), wrapped)
		return nil, wrapped
	}
	if err != nil {
		wrapped := fmt.Errorf("security %s: %w: %s", command, err, strings.TrimSpace(string(out)))
		if caSecurityExpectedEmptyResult(command, out) {
			logger.G().Debugf("CA trust security command returned an expected empty result: command=%s duration=%s output_bytes=%d", command, duration, len(out))
		} else {
			logger.G().Warnf("CA trust security command failed: command=%s duration=%s output_bytes=%d error=%q output=%q", command, duration, len(out), caTrustLogText(err.Error()), caTrustLogText(string(out)))
		}
		return out, wrapped
	}
	logger.G().Debugf("CA trust security command succeeded: command=%s duration=%s output_bytes=%d", command, duration, len(out))
	return out, nil
}

func (s securityCACertificateTrustStore) loginKeychain() (string, error) {
	out, err := s.command(caSecurityQueryTimeout, "login-keychain")
	if err != nil {
		return "", err
	}
	keychain := strings.TrimSpace(string(out))
	if len(keychain) >= 2 && keychain[0] == '"' && keychain[len(keychain)-1] == '"' {
		keychain = keychain[1 : len(keychain)-1]
	}
	// Do not fall back to the search list, a hardcoded login path, or a machine
	// keychain if the user has no usable login keychain.
	clean := path.Clean(keychain)
	if !path.IsAbs(clean) || strings.ContainsAny(keychain, "\x00\r\n") ||
		strings.HasPrefix(clean, "/Library/Keychains/") || strings.HasPrefix(clean, "/System/") {
		logger.G().Warnf("CA trust login keychain rejected: path=%q", caTrustLogText(keychain))
		return "", errors.New("ca_trust_login_keychain_unavailable")
	}
	logger.G().Debugf("CA trust login keychain resolved: path=%q", keychain)
	return keychain, nil
}

func (s securityCACertificateTrustStore) certificatePresent(keychain string, der []byte) (bool, error) {
	out, err := s.command(caSecurityQueryTimeout, "find-certificate", "-a", "-p", keychain)
	if err != nil {
		if caSecurityMissingItem(out) {
			return false, nil
		}
		return false, err
	}
	present := false
	for len(bytes.TrimSpace(out)) > 0 {
		out = bytes.TrimSpace(out)
		// pem.Decode skips leading non-PEM text. Reject it explicitly so a
		// keychain-open diagnostic cannot be mistaken for a successful query.
		if !bytes.HasPrefix(out, []byte("-----BEGIN CERTIFICATE-----")) {
			return false, errors.New("invalid security certificate output")
		}
		block, rest := pem.Decode(out)
		if block == nil || block.Type != "CERTIFICATE" {
			return false, errors.New("invalid security certificate output")
		}
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			return false, fmt.Errorf("invalid keychain certificate: %w", err)
		}
		present = present || bytes.Equal(block.Bytes, der)
		out = rest
	}
	return present, nil
}

func (s securityCACertificateTrustStore) userTrust(cert *x509.Certificate) (present, trusted bool, err error) {
	dir, err := os.MkdirTemp("", "flowlens-ca-trust-*")
	if err != nil {
		return false, false, err
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "user-trust.plist")
	out, err := s.command(caSecurityQueryTimeout, "trust-settings-export", file)
	if err != nil {
		// An untouched user's trust domain may not have a record at all. Only
		// this specific error is an empty store; permission/IPC errors are unknown.
		if caSecurityNoTrustSettings(out) {
			return false, false, nil
		}
		return false, false, err
	}
	f, err := os.Open(file)
	if err != nil {
		return false, false, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, caSecurityOutputLimit+1))
	if err != nil {
		return false, false, err
	}
	if len(data) > caSecurityOutputLimit {
		return false, false, errors.New("user trust settings exceed size limit")
	}
	return caSecurityTrustSettings(data, cert)
}

func (s securityCACertificateTrustStore) status(der []byte) (caTrustState, error) {
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return caTrustState{}, err
	}
	fingerprint := caCertificateFingerprint(cert)
	logger.G().Debugf("CA trust status query started: fingerprint=%s", fingerprint)
	keychain, err := s.loginKeychain()
	if err != nil {
		logger.G().Warnf("CA trust status query failed: stage=login_keychain fingerprint=%s error=%q", fingerprint, caTrustLogText(err.Error()))
		return caTrustState{}, err
	}
	certificatePresent, err := s.certificatePresent(keychain, der)
	if err != nil {
		logger.G().Warnf("CA trust status query failed: stage=keychain_certificate fingerprint=%s keychain=%q error=%q", fingerprint, keychain, caTrustLogText(err.Error()))
		return caTrustState{}, err
	}
	trustPresent, trusted, err := s.userTrust(cert)
	state := caTrustState{present: certificatePresent || trustPresent, installed: certificatePresent && trusted}
	if err != nil {
		logger.G().Warnf("CA trust status query failed: stage=user_trust fingerprint=%s keychain=%q certificate_present=%t trust_present=%t trusted=%t error=%q", fingerprint, keychain, certificatePresent, trustPresent, trusted, caTrustLogText(err.Error()))
		return state, err
	}
	logger.G().Debugf("CA trust status query completed: fingerprint=%s keychain=%q certificate_present=%t trust_present=%t trusted=%t installed=%t", fingerprint, keychain, certificatePresent, trustPresent, trusted, state.installed)
	return state, nil
}

func (s securityCACertificateTrustStore) install(der []byte) error {
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return err
	}
	keychain, err := s.loginKeychain()
	if err != nil {
		return err
	}
	fingerprint := caCertificateFingerprint(cert)
	result := "trustAsRoot"
	if caSecuritySelfSigned(cert) {
		result = "trustRoot"
	}
	logger.G().Infof("CA trust installation started: fingerprint=%s keychain=%q trust_result=%s policies=ssl,basic", fingerprint, keychain, result)
	return withCATrustPublicSnapshot(der, func(file string) error {
		// Omitting -d keeps trust in the current-user domain. Both policies are
		// explicit; no private key, shell, sudo, or administrator domain is used.
		_, err := s.command(caSecurityActionTimeout, "add-trusted-cert", "-r", result,
			"-p", "ssl", "-p", "basic", "-k", keychain, file)
		return err
	})
}

func (s securityCACertificateTrustStore) uninstall(der []byte) error {
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return err
	}
	keychain, err := s.loginKeychain()
	if err != nil {
		return err
	}
	// Remove user trust first, including an orphaned trust record. This also
	// avoids deleting the certificate before an authentication cancellation.
	trustPresent, _, err := s.userTrust(cert)
	if err != nil {
		return err
	}
	fingerprintText := caCertificateFingerprint(cert)
	logger.G().Infof("CA certificate removal started: fingerprint=%s keychain=%q trust_present=%t", fingerprintText, keychain, trustPresent)
	if trustPresent {
		logger.G().Debugf("CA user trust removal started: fingerprint=%s", fingerprintText)
		err = withCATrustPublicSnapshot(der, func(file string) error {
			out, err := s.command(caSecurityActionTimeout, "remove-trusted-cert", file)
			if err != nil && !caSecurityNoTrustSettings(out) && !caSecurityMissingItem(out) {
				return err
			}
			return nil
		})
		if err != nil {
			return err
		}
		logger.G().Debugf("CA user trust removal command completed: fingerprint=%s", fingerprintText)
	}
	// The login keychain is explicitly scoped, and full DER was checked before
	// using SHA256 for removal. Never identify a certificate by its subject.
	fingerprint := sha256.Sum256(der)
	for attempt := 1; attempt <= 64; attempt++ {
		present, err := s.certificatePresent(keychain, der)
		if err != nil || !present {
			if err != nil {
				logger.G().Warnf("CA certificate removal query failed: fingerprint=%s keychain=%q attempt=%d error=%q", fingerprintText, keychain, attempt, caTrustLogText(err.Error()))
			} else {
				logger.G().Infof("CA certificate removal completed: fingerprint=%s keychain=%q attempts=%d", fingerprintText, keychain, attempt-1)
			}
			return err
		}
		out, err := s.command(caSecurityActionTimeout, "delete-certificate", "-Z", hex.EncodeToString(fingerprint[:]), keychain)
		if err != nil && !caSecurityMissingItem(out) {
			return err
		}
	}
	logger.G().Errorf("CA certificate removal verification failed: fingerprint=%s keychain=%q attempts=%d", fingerprintText, keychain, 64)
	return errors.New("ca_trust_verification_failed")
}

func withCATrustPublicSnapshot(der []byte, action func(string) error) error {
	f, err := os.CreateTemp("", "flowlens-ca-*.crt")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: der})
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return action(f.Name())
}

func caSecuritySelfSigned(cert *x509.Certificate) bool {
	return bytes.Equal(cert.RawSubject, cert.RawIssuer) && cert.CheckSignatureFrom(cert) == nil
}

func caSecurityMissingItem(out []byte) bool {
	return bytes.Contains(out, []byte("The specified item could not be found in the keychain.")) || bytes.Contains(out, []byte(": -25300"))
}

func caSecurityNoTrustSettings(out []byte) bool {
	return bytes.Contains(out, []byte("No Trust Settings were found.")) || bytes.Contains(out, []byte(": -25263"))
}

func caSecurityExpectedEmptyResult(command string, out []byte) bool {
	if command == "trust-settings-export" || command == "remove-trusted-cert" {
		if caSecurityNoTrustSettings(out) {
			return true
		}
	}
	if command == "find-certificate" || command == "remove-trusted-cert" || command == "delete-certificate" {
		return caSecurityMissingItem(out)
	}
	return false
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

type caSecurityOutput struct {
	buffer   bytes.Buffer
	overflow bool
}

func (b *caSecurityOutput) Write(p []byte) (int, error) {
	length := len(p)
	if remaining := caSecurityOutputLimit - b.buffer.Len(); length > remaining {
		p = p[:remaining]
		b.overflow = true
	}
	_, _ = b.buffer.Write(p)
	return length, nil
}
