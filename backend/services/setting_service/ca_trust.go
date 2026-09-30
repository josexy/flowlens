package settingservice

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/josexy/flowlens/backend/pkg/logger"
)

const caTrustLogTextLimit = 2 << 10

var (
	errCATrustCanceled           = errors.New("ca_trust_canceled")
	errCATrustUnsupported        = errors.New("ca_trust_unsupported")
	errCATrustCertificateChanged = errors.New("ca_trust_certificate_changed")
	errCATrustInvalidCA          = errors.New("ca_trust_invalid_ca")
	errCATrustInvalidPair        = errors.New("ca_trust_invalid_pair")
	errCATrustInvalidValidity    = errors.New("ca_trust_invalid_validity")
	errCATrustUninstallFirst     = errors.New("ca_trust_uninstall_first")
)

// Platform implementations accept public certificate DER only. The service
// owns configuration, validation and serialization with CA generation.
type caCertificateTrustStore interface {
	supported() bool
	status([]byte) (caTrustState, error)
	install([]byte) error
	uninstall([]byte) error
}

type caTrustState struct {
	// present includes incomplete installations, such as a keychain certificate
	// without trust settings, or trust settings without a keychain certificate.
	present   bool
	installed bool
}

func (s *SettingService) certificateTrustStore() caCertificateTrustStore {
	if s.caTrustStore != nil {
		return s.caTrustStore
	}
	return newCACertificateTrustStore()
}

func (s *SettingService) GetCurrentCACertificateTrustStatus() (*CACertificateTrustStatus, error) {
	s.caMu.Lock()
	defer s.caMu.Unlock()
	logger.G().Debug("CA trust status request started")
	store := s.certificateTrustStore()
	if !store.supported() {
		logger.G().Debug("CA trust status request skipped: platform unsupported")
		return &CACertificateTrustStatus{}, nil
	}
	certPath, _, err := s.currentCAPaths()
	if err != nil {
		logger.G().Warnf("CA trust status request failed to resolve certificate path: error=%q", caTrustLogText(err.Error()))
		return nil, err
	}
	status := caTrustStatus(store, certPath)
	if status.Error != "" {
		logger.G().Warnf("CA trust status request completed with unknown state: cert=%q fingerprint=%s error=%q", certPath, status.SHA256Fingerprint, caTrustLogText(status.Error))
	} else {
		logger.G().Debugf("CA trust status request completed: cert=%q fingerprint=%s present=%t installed=%t", certPath, status.SHA256Fingerprint, status.Present, status.Installed)
	}
	return status, nil
}

func caTrustStatus(store caCertificateTrustStore, certPath string) *CACertificateTrustStatus {
	status := &CACertificateTrustStatus{Supported: store.supported()}
	cert, _, err := readCAPublicCertificate(certPath)
	if errors.Is(err, os.ErrNotExist) {
		// No configured certificate yet is an ordinary initial state.
		return status
	}
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.SHA256Fingerprint = caCertificateFingerprint(cert)
	state, err := store.status(cert.Raw)
	status.Present, status.Installed = state.present, state.installed
	if err != nil {
		status.Error = err.Error()
	}
	return status
}

func (s *SettingService) InstallCurrentCACertificate(expectedSHA256Fingerprint string) (*CACertificateTrustStatus, error) {
	return s.changeCurrentCACertificateTrust(expectedSHA256Fingerprint, true)
}

func (s *SettingService) UninstallCurrentCACertificate(expectedSHA256Fingerprint string) (*CACertificateTrustStatus, error) {
	return s.changeCurrentCACertificateTrust(expectedSHA256Fingerprint, false)
}

func (s *SettingService) changeCurrentCACertificateTrust(expectedFingerprint string, install bool) (status *CACertificateTrustStatus, err error) {
	s.caMu.Lock()
	defer s.caMu.Unlock()
	action := "uninstall"
	if install {
		action = "install"
	}
	started := time.Now()
	logger.G().Infof("CA trust change requested: action=%s expected_fingerprint=%q", action, caTrustLogText(expectedFingerprint))
	fingerprint := ""
	stage := "resolve_configuration"
	defer func() {
		if errors.Is(err, errCATrustCanceled) {
			logger.G().Infof("CA trust change canceled: action=%s stage=%s fingerprint=%s duration=%s", action, stage, fingerprint, time.Since(started))
			return
		}
		if err != nil {
			logger.G().Errorf("CA trust change failed: action=%s stage=%s fingerprint=%s duration=%s error=%q", action, stage, fingerprint, time.Since(started), caTrustLogText(err.Error()))
			return
		}
		logger.G().Infof("CA trust change succeeded: action=%s fingerprint=%s present=%t installed=%t duration=%s", action, fingerprint, status.Present, status.Installed, time.Since(started))
	}()
	store := s.certificateTrustStore()
	if !store.supported() {
		return nil, errCATrustUnsupported
	}
	certPath, keyPath, err := s.currentCAPaths()
	if err != nil {
		return nil, err
	}
	stage = "read_certificate"
	cert, certBytes, err := readCAPublicCertificate(certPath)
	if err != nil {
		return nil, err
	}
	fingerprint = caCertificateFingerprint(cert)
	stage = "validate_certificate"
	if expectedFingerprint == "" || expectedFingerprint != fingerprint {
		return nil, errCATrustCertificateChanged
	}
	if install {
		if !cert.IsCA || !cert.BasicConstraintsValid {
			return nil, errCATrustInvalidCA
		}
		now := time.Now()
		if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
			return nil, errCATrustInvalidValidity
		}
		keyBytes, readErr := os.ReadFile(keyPath)
		if readErr != nil {
			return nil, fmt.Errorf("%w: %v", errCATrustInvalidPair, readErr)
		}
		// Validate the same certificate snapshot that will be installed.
		if _, pairErr := tls.X509KeyPair(certBytes, keyBytes); pairErr != nil {
			return nil, fmt.Errorf("%w: %v", errCATrustInvalidPair, pairErr)
		}
	}
	stage = "query_status"
	state, err := store.status(cert.Raw)
	if err != nil {
		return nil, err
	}
	logger.G().Debugf("CA trust change initial state: action=%s fingerprint=%s present=%t installed=%t", action, fingerprint, state.present, state.installed)
	if (install && !state.installed) || (!install && state.present) {
		stage = action
		if install {
			err = store.install(cert.Raw)
		} else {
			err = store.uninstall(cert.Raw)
		}
		if err != nil {
			return nil, err
		}
	}
	// Verify against the same public certificate, without depending on the key
	// or a path that another settings operation might have changed.
	stage = "verify_status"
	state, err = store.status(cert.Raw)
	if err != nil {
		return nil, err
	}
	if (install && !state.installed) || (!install && state.present) {
		return nil, errors.New("ca_trust_verification_failed")
	}
	return &CACertificateTrustStatus{Supported: true, Present: state.present, Installed: state.installed, SHA256Fingerprint: fingerprint}, nil
}

func (s *SettingService) checkCAReplacement(certPath string) error {
	store := s.certificateTrustStore()
	if !store.supported() {
		return nil
	}
	cert, data, err := readCAPublicCertificate(certPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || data != nil {
			// Permit repair of an absent, empty or malformed certificate. A read
			// failure is different: do not overwrite an unreadable certificate.
			return nil
		}
		return err
	}
	state, err := store.status(cert.Raw)
	if err != nil {
		return err
	}
	if state.present {
		return errCATrustUninstallFirst
	}
	return nil
}

func readCAPublicCertificate(path string) (*x509.Certificate, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	remaining := data
	for {
		block, rest := pem.Decode(remaining)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			cert, err := x509.ParseCertificate(block.Bytes)
			return cert, data, err
		}
		remaining = rest
	}
	cert, err := x509.ParseCertificate(data)
	return cert, data, err
}

func caCertificateFingerprint(cert *x509.Certificate) string {
	fingerprint := sha256.Sum256(cert.Raw)
	return colonHex(fingerprint[:])
}

// Diagnostics may contain partial PEM output when security fails or times out.
// Keep those payloads out of logs, including errors wrapped by the service.
func caTrustLogText(text string) string {
	if strings.Contains(text, "-----BEGIN ") || strings.Contains(text, "-----END ") {
		return "<PEM output omitted>"
	}
	if strings.Contains(text, "<?xml") || strings.Contains(text, "<plist") || strings.Contains(text, "bplist00") {
		return "<trust settings output omitted>"
	}
	preview := strings.Join(strings.Fields(text), " ")
	if len(preview) <= caTrustLogTextLimit {
		return preview
	}
	return strings.ToValidUTF8(preview[:caTrustLogTextLimit], "") + "... [truncated]"
}
