package settingservice

import (
	"bytes"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestBuiltinCAStartupAndReuse(t *testing.T) {
	configureTestSettingsPath(t)
	s := newPersistentTestSettingService(t)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	config, _ := s.GetProxyConfig()
	if !config.UseBuiltinCA || config.CACertPath != "" || config.CAKeyPath != "" {
		t.Fatalf("new installation defaults = %+v", config)
	}
	if err := s.ServiceStartup(t.Context(), application.ServiceOptions{}); err != nil {
		t.Fatal(err)
	}
	first, err := s.GetCACertificateInfo()
	if err != nil || !first.ValidPair || first.Error != "" || first.CertPath != "" || first.KeyPath != "" {
		t.Fatalf("builtin information = %+v, %v", first, err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	restarted := New(s.repository.db)
	if err := restarted.ServiceStartup(t.Context(), application.ServiceOptions{}); err != nil {
		t.Fatal(err)
	}
	second, err := restarted.GetCACertificateInfo()
	if err != nil || second.SHA256Fingerprint != first.SHA256Fingerprint {
		t.Fatalf("startup rotated the CA: %+v, %v", second, err)
	}
}

func TestLegacyCAConfigurationRemainsCustom(t *testing.T) {
	for _, payload := range []string{
		`{"caCertPath":"certs/ca.crt","caKeyPath":"certs/ca.key"}`,
		`{"caCertPath":"/custom/root.pem","caKeyPath":"/custom/root.key"}`,
		`{}`,
	} {
		settings := &Settings{}
		if err := decodeSettingSection(settings, settingsSectionProxy, []byte(payload)); err != nil {
			t.Fatal(err)
		}
		if settings.ProxyConfig.UseBuiltinCA || settings.ProxyConfig.CACertPath == "" || settings.ProxyConfig.CAKeyPath == "" {
			t.Fatalf("legacy configuration changed: %+v", settings.ProxyConfig)
		}
		if strings.Contains(payload, "/custom/") && settings.ProxyConfig.CACertPath != "/custom/root.pem" {
			t.Fatal("legacy custom path changed")
		}
	}
	settings := &Settings{}
	if err := decodeSettingSection(settings, settingsSectionProxy, []byte(`{"useBuiltinCA":false,"caCertPath":"","caKeyPath":""}`)); err != nil {
		t.Fatal(err)
	}
	setupDefaultProxyConfig(settings.ProxyConfig)
	if _, _, err := ResolveProxyCAPaths(settings.ProxyConfig); err == nil {
		t.Fatal("empty custom paths silently acquired defaults")
	}
}

func TestBuiltinCAStartupNeverRepairsExistingFiles(t *testing.T) {
	for _, scenario := range []string{"missing key", "missing cert", "malformed", "mismatched", "expired", "unreadable"} {
		t.Run(scenario, func(t *testing.T) {
			s := newTestSettingServiceWithCAPaths(t)
			certPath, keyPath, _ := s.currentCAPaths()
			if err := os.MkdirAll(filepath.Dir(certPath), 0700); err != nil {
				t.Fatal(err)
			}
			certPEM, keyPEM, err := generateCACertificatePEM("Existing CA", 1)
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "malformed":
				certPEM = []byte("invalid certificate")
			case "mismatched":
				_, keyPEM, err = generateCACertificatePEM("Other CA", 1)
			case "expired":
				certBlock, _ := pem.Decode(certPEM)
				keyBlock, _ := pem.Decode(keyPEM)
				cert, parseErr := x509.ParseCertificate(certBlock.Bytes)
				if parseErr != nil {
					t.Fatal(parseErr)
				}
				key, parseErr := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
				if parseErr != nil {
					t.Fatal(parseErr)
				}
				cert.NotBefore = time.Now().Add(-2 * time.Hour)
				cert.NotAfter = time.Now().Add(-time.Hour)
				var der []byte
				der, err = x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
				certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
			}
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "unreadable" {
				if err := os.Mkdir(certPath, 0700); err != nil {
					t.Fatal(err)
				}
			} else if scenario != "missing cert" {
				if err := os.WriteFile(certPath, certPEM, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if scenario != "missing key" {
				if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.ServiceStartup(t.Context(), application.ServiceOptions{}); err != nil {
				t.Fatalf("CA failure blocked settings startup: %v", err)
			}
			if scenario == "missing cert" && pathExists(certPath) || scenario == "missing key" && pathExists(keyPath) {
				t.Fatal("startup created a replacement for an incomplete pair")
			}
			info, err := s.GetCACertificateInfo()
			if err != nil || info.Error == "" || info.CertPath != "" || info.KeyPath != "" || strings.Contains(info.Error, certPath) {
				t.Fatalf("invalid builtin information = %+v, %v", info, err)
			}
			if scenario != "missing cert" && scenario != "unreadable" {
				got, _ := os.ReadFile(certPath)
				if !bytes.Equal(got, certPEM) {
					t.Fatal("startup changed existing certificate bytes")
				}
			}
			if scenario != "missing key" {
				got, _ := os.ReadFile(keyPath)
				if !bytes.Equal(got, keyPEM) {
					t.Fatal("startup changed existing key bytes")
				}
			}
		})
	}
}

func cloneCASettings(t *testing.T, s *SettingService) *Settings {
	t.Helper()
	settings, err := s.Get()
	if err != nil {
		t.Fatal(err)
	}
	candidate := *settings
	proxy := *settings.ProxyConfig
	candidate.ProxyConfig = &proxy
	return &candidate
}

func TestCustomCAGenerationCannotWriteFiles(t *testing.T) {
	s := newTestSettingServiceWithCAPaths(t)
	config := cloneCASettings(t, s)
	config.ProxyConfig.UseBuiltinCA = false
	config.ProxyConfig.CACertPath = filepath.Join(t.TempDir(), "custom.crt")
	config.ProxyConfig.CAKeyPath = filepath.Join(t.TempDir(), "custom.key")
	for _, path := range []string{config.ProxyConfig.CACertPath, config.ProxyConfig.CAKeyPath} {
		if err := os.WriteFile(path, []byte("user-owned file"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Update(config); err != nil {
		t.Fatal(err)
	}
	for _, overwrite := range []bool{false, true} {
		if _, err := s.GenerateCurrentCACertificate(GenerateCACertificateRequest{Overwrite: overwrite}); err == nil || err.Error() != "ca_custom_generation_disabled" {
			t.Fatalf("custom generation was allowed: %v", err)
		}
	}
	for _, path := range []string{config.ProxyConfig.CACertPath, config.ProxyConfig.CAKeyPath} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "user-owned file" {
			t.Fatalf("custom file was changed: %s, %v", data, err)
		}
	}
}

func TestCASwitchSaveAndFailure(t *testing.T) {
	for _, failure := range []string{"none", "database", "initialization"} {
		t.Run(failure, func(t *testing.T) {
			configureTestSettingsPath(t)
			s := newPersistentTestSettingService(t)
			if err := s.Load(); err != nil {
				t.Fatal(err)
			}
			custom := cloneCASettings(t, s)
			custom.ProxyConfig.UseBuiltinCA = false
			custom.ProxyConfig.CACertPath = "user.crt"
			custom.ProxyConfig.CAKeyPath = "user.key"
			if err := s.UpdatePreservingShortcuts(custom); err != nil {
				t.Fatal(err)
			}
			if err := s.Save(); err != nil {
				t.Fatal(err)
			}
			if failure == "database" {
				if _, err := s.repository.db.Exec(`CREATE TRIGGER fail_ca_save BEFORE INSERT ON app_settings BEGIN SELECT RAISE(FAIL, 'save rejected'); END`); err != nil {
					t.Fatal(err)
				}
			}
			if failure == "initialization" {
				certPath, _, _ := ResolveProxyCAPaths(&ProxyConfig{UseBuiltinCA: true})
				if err := os.MkdirAll(filepath.Dir(certPath), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(certPath, []byte("incomplete pair"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			candidate := cloneCASettings(t, s)
			candidate.ProxyConfig.UseBuiltinCA = true
			if err := s.UpdatePreservingShortcuts(candidate); err != nil {
				t.Fatal(err)
			}
			active, _ := s.GetProxyConfig()
			if active.UseBuiltinCA {
				t.Fatal("unsaved mode changed active CA")
			}
			err := s.Save()
			active, _ = s.GetProxyConfig()
			if failure != "none" {
				if err == nil || active.UseBuiltinCA || active.CACertPath != "user.crt" || active.CAKeyPath != "user.key" {
					t.Fatalf("failed save changed active CA: %+v, %v", active, err)
				}
				persisted, readErr := s.repository.load(t.Context())
				if readErr != nil || persisted.ProxyConfig.UseBuiltinCA || persisted.ProxyConfig.CACertPath != "user.crt" {
					t.Fatalf("failed save changed persisted CA: %+v, %v", persisted, readErr)
				}
				return
			}
			if err != nil || !active.UseBuiltinCA || active.CACertPath != "user.crt" || active.CAKeyPath != "user.key" {
				t.Fatalf("saved selection = %+v, %v", active, err)
			}
			first, _ := s.GetCACertificateInfo()
			for _, enabled := range []bool{false, true} {
				next := cloneCASettings(t, s)
				next.ProxyConfig.UseBuiltinCA = enabled
				if err := s.UpdatePreservingShortcuts(next); err != nil {
					t.Fatal(err)
				}
				if err := s.Save(); err != nil {
					t.Fatal(err)
				}
			}
			second, _ := s.GetCACertificateInfo()
			if first.SHA256Fingerprint != second.SHA256Fingerprint {
				t.Fatal("switching back rotated the builtin CA")
			}
		})
	}
}

func TestCustomCAStartupDoesNotCreateFiles(t *testing.T) {
	configureTestSettingsPath(t)
	s := &SettingService{}
	dir := t.TempDir()
	config := &ProxyConfig{CACertPath: filepath.Join(dir, "user.crt"), CAKeyPath: filepath.Join(dir, "user.key")}
	if err := s.Update(&Settings{ProxyConfig: config}); err != nil {
		t.Fatal(err)
	}
	if err := s.ServiceStartup(t.Context(), application.ServiceOptions{}); err != nil {
		t.Fatal(err)
	}
	if pathExists(config.CACertPath) || pathExists(config.CAKeyPath) {
		t.Fatal("startup generated custom certificate files")
	}
	builtinCert, builtinKey, _ := ResolveProxyCAPaths(&ProxyConfig{UseBuiltinCA: true})
	if pathExists(builtinCert) || pathExists(builtinKey) {
		t.Fatal("custom startup generated an inactive builtin CA")
	}
}

func TestBuiltinCAErrorsHideInternalPaths(t *testing.T) {
	s := newTestSettingServiceWithCAPaths(t)
	certPath, _, _ := s.currentCAPaths()
	if err := os.MkdirAll(certPath, 0700); err != nil {
		t.Fatal(err)
	}
	status, err := s.GetCurrentCACertificateTrustStatus()
	if err != nil || status.Error != "ca_builtin_unavailable" {
		t.Fatalf("trust status leaked internal error: %+v, %v", status, err)
	}
	_, err = s.InstallCurrentCACertificate("fingerprint")
	if err == nil || strings.Contains(err.Error(), certPath) {
		t.Fatalf("trust operation leaked internal path: %v", err)
	}
	_, err = s.GenerateCurrentCACertificate(GenerateCACertificateRequest{Overwrite: true})
	if err == nil || strings.Contains(err.Error(), certPath) {
		t.Fatalf("generation leaked internal path: %v", err)
	}
}

func TestCASettingsUpdateWaitsForTrustOperation(t *testing.T) {
	s, store, cert := newCATrustTestService(t)
	entered, release := make(chan struct{}), make(chan struct{})
	store.onInstall = func() { close(entered); <-release }
	installDone := make(chan error, 1)
	go func() { _, err := s.InstallCurrentCACertificate(caCertificateFingerprint(cert)); installDone <- err }()
	<-entered
	candidate := cloneCASettings(t, s)
	candidate.ProxyConfig.UseBuiltinCA = false
	updateDone := make(chan error, 1)
	go func() { updateDone <- s.Update(candidate) }()
	select {
	case err := <-updateDone:
		close(release)
		t.Fatalf("CA selection changed during installation: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if err := <-installDone; err != nil {
		t.Fatal(err)
	}
	if err := <-updateDone; err != nil {
		t.Fatal(err)
	}
	wrapped := hideBuiltinCAError(errCATrustInvalidPair)
	if !errors.Is(wrapped, errCATrustInvalidPair) {
		t.Fatal("path hiding lost trust error identity")
	}
}
