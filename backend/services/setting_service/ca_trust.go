package settingservice

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/josexy/flowlens/backend/pkg/logger"
)

var (
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
	contains([]byte) (bool, error)
	install([]byte) error
	uninstall([]byte) error
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
	store := s.certificateTrustStore()
	if !store.supported() {
		return &CACertificateTrustStatus{}, nil
	}
	certPath, _, err := s.currentCAPaths()
	if err != nil {
		return nil, err
	}
	return caTrustStatus(store, certPath), nil
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
	status.Installed, err = store.contains(cert.Raw)
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
	store := s.certificateTrustStore()
	if !store.supported() {
		return nil, errCATrustUnsupported
	}
	certPath, keyPath, err := s.currentCAPaths()
	if err != nil {
		return nil, err
	}
	cert, certBytes, err := readCAPublicCertificate(certPath)
	if err != nil {
		return nil, err
	}
	fingerprint := caCertificateFingerprint(cert)
	if expectedFingerprint == "" || expectedFingerprint != fingerprint {
		return nil, errCATrustCertificateChanged
	}
	defer func() {
		if err != nil {
			logger.G().Errorf("CA trust change failed: install=%t fingerprint=%s error=%v", install, fingerprint, err)
		} else {
			logger.G().Infof("CA trust change succeeded: install=%t fingerprint=%s", install, fingerprint)
		}
	}()
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
	installed, err := store.contains(cert.Raw)
	if err != nil {
		return nil, err
	}
	if install != installed {
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
	installed, err = store.contains(cert.Raw)
	if err != nil {
		return nil, err
	}
	if installed != install {
		return nil, errors.New("ca_trust_verification_failed")
	}
	return &CACertificateTrustStatus{Supported: true, Installed: installed, SHA256Fingerprint: fingerprint}, nil
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
	installed, err := store.contains(cert.Raw)
	if err != nil {
		return err
	}
	if installed {
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
