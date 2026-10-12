package settingservice

import (
	"crypto/x509"
	"errors"
	"os"
	"path/filepath"

	"github.com/josexy/flowlens/backend/pkg/fs"
)

// ApplicationResetGuard holds CA and persistence serialization until the app
// commits a pending reset, preventing a concurrent save from recreating the CA.
type ApplicationResetGuard struct {
	service     *SettingService
	certPath    string
	certificate *x509.Certificate
	fingerprint string
	finished    bool
}

func BeginApplicationReset(service *SettingService) (*ApplicationResetGuard, error) {
	if err := service.ensureLoaded(); err != nil {
		return nil, hideBuiltinCAError(err)
	}
	service.caMu.Lock()
	service.persistMu.Lock()
	if service.applicationReset.Load() {
		service.persistMu.Unlock()
		service.caMu.Unlock()
		return nil, errors.New("application_reset_in_progress")
	}
	service.applicationReset.Store(true)
	guard := &ApplicationResetGuard{service: service}
	baseDir, err := fs.BaseStoragePath()
	if err == nil {
		guard.certPath = filepath.Join(baseDir, "certs", "ca.crt")
		guard.certificate, _, err = readCAPublicCertificate(guard.certPath)
		if errors.Is(err, os.ErrNotExist) {
			err = nil
		} else if err == nil {
			guard.fingerprint = caCertificateFingerprint(guard.certificate)
			if store := service.certificateTrustStore(); store.supported() {
				_, err = store.status(guard.certificate.Raw)
			}
		}
	}
	if err != nil {
		guard.Cancel()
		return nil, hideBuiltinCAError(err)
	}
	return guard, nil
}

// RemoveBuiltinCATrust checks the same public certificate snapshot even when
// custom CA mode is selected. External custom certificates are never touched.
func (g *ApplicationResetGuard) RemoveBuiltinCATrust() (err error) {
	defer func() { err = hideBuiltinCAError(err) }()
	if g.certificate == nil {
		if _, err := os.Lstat(g.certPath); !errors.Is(err, os.ErrNotExist) {
			return errCATrustCertificateChanged
		}
		return nil
	}
	certificate, _, err := readCAPublicCertificate(g.certPath)
	if err != nil || caCertificateFingerprint(certificate) != g.fingerprint {
		return errCATrustCertificateChanged
	}
	store := g.service.certificateTrustStore()
	if !store.supported() {
		return nil
	}
	state, err := store.status(g.certificate.Raw)
	if err != nil {
		return err
	}
	if state.present {
		if err := store.uninstall(g.certificate.Raw); err != nil {
			return err
		}
	}
	state, err = store.status(g.certificate.Raw)
	if err != nil {
		return err
	}
	if state.present {
		return errors.New("ca_trust_verification_failed")
	}
	return nil
}

func (g *ApplicationResetGuard) Commit() { g.finish(true) }
func (g *ApplicationResetGuard) Cancel() { g.finish(false) }

func (g *ApplicationResetGuard) finish(committed bool) {
	if g.finished {
		return
	}
	g.finished = true
	g.service.applicationReset.Store(committed)
	g.service.persistMu.Unlock()
	g.service.caMu.Unlock()
}
