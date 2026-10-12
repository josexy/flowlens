package settingservice

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/josexy/flowlens/backend/pkg/fs"
)

// Keep the saved CA authoritative while ordinary settings are staged by Update.
type caConfiguration struct {
	useBuiltinCA bool
	certPath     string
	keyPath      string
}

func caConfigurationFrom(config *ProxyConfig) caConfiguration {
	return caConfiguration{config.UseBuiltinCA, config.CACertPath, config.CAKeyPath}
}

func (config caConfiguration) apply(target *ProxyConfig) {
	target.UseBuiltinCA = config.useBuiltinCA
	target.CACertPath = config.certPath
	target.CAKeyPath = config.keyPath
}

func (s *SettingService) currentCAConfiguration() (caConfiguration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.savedCAConfig != nil {
		return *s.savedCAConfig, nil
	}
	if s.settings == nil || s.settings.ProxyConfig == nil {
		return caConfiguration{}, errors.New("proxy config not loaded")
	}
	return caConfigurationFrom(s.settings.ProxyConfig), nil
}

// ResolveProxyCAPaths is for backend consumers only. Internal paths are never
// written into ProxyConfig or returned as certificate information to the UI.
func ResolveProxyCAPaths(config *ProxyConfig) (string, string, error) {
	if config == nil {
		return "", "", errors.New("proxy config not loaded")
	}
	if !config.UseBuiltinCA {
		certPath, keyPath := strings.TrimSpace(config.CACertPath), strings.TrimSpace(config.CAKeyPath)
		if certPath == "" || keyPath == "" {
			return "", "", errors.New("ca_custom_paths_required")
		}
		return certPath, keyPath, nil
	}
	baseDir, err := fs.GetBaseStorageDir()
	if err != nil {
		return "", "", hideBuiltinCAError(err)
	}
	return filepath.Join(baseDir, "certs", "ca.crt"), filepath.Join(baseDir, "certs", "ca.key"), nil
}

// caMu must be held. Only a completely absent pair may be initialized.
func (s *SettingService) ensureBuiltinCALocked() error {
	certPath, keyPath, err := ResolveProxyCAPaths(&ProxyConfig{UseBuiltinCA: true})
	if err != nil {
		return err
	}
	_, certErr := os.Lstat(certPath)
	_, keyErr := os.Lstat(keyPath)
	if errors.Is(certErr, os.ErrNotExist) && errors.Is(keyErr, os.ErrNotExist) {
		_, err = s.generateCALocked(certPath, keyPath, GenerateCACertificateRequest{})
		return err
	}
	if certErr != nil {
		return certErr
	}
	if keyErr != nil {
		return keyErr
	}
	info := inspectCACertificate(certPath, keyPath)
	if info.Error != "" {
		return errors.New(info.Error)
	}
	if !info.IsCA {
		return errCATrustInvalidCA
	}
	return nil
}

func (s *SettingService) publicCAInfo(info *CACertificateInfo) *CACertificateInfo {
	config, _ := s.currentCAConfiguration()
	if info != nil && config.useBuiltinCA {
		info.CertPath, info.KeyPath = "", ""
		if info.Error != "" {
			info.Error = "ca_builtin_unavailable"
		}
	}
	return info
}

type builtinCAError struct {
	cause   error
	message string
}

func (err builtinCAError) Error() string { return err.message }
func (err builtinCAError) Unwrap() error { return err.cause }

// Preserve machine-readable trust codes and errors.Is without exposing paths
// from filesystem or platform diagnostics in user-facing errors.
func hideBuiltinCAError(err error) error {
	if err == nil {
		return nil
	}
	message := "ca_builtin_unavailable"
	for _, word := range strings.Fields(err.Error()) {
		if strings.HasPrefix(word, "ca_trust_") {
			message = strings.TrimRight(word, ":,.")
			break
		}
	}
	return builtinCAError{cause: err, message: message}
}
