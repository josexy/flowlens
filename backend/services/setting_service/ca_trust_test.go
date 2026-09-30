package settingservice

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type testCATrustStore struct {
	unsupported bool
	certs       map[string]bool
	queryErr    error
	installErr  error
	deleteErr   error
	installRuns int
	deleteRuns  int
	onInstall   func()
	noWrite     bool
}

func (s *testCATrustStore) supported() bool { return !s.unsupported }
func (s *testCATrustStore) status(der []byte) (caTrustState, error) {
	installed := s.certs[string(der)]
	return caTrustState{present: installed, installed: installed}, s.queryErr
}
func (s *testCATrustStore) install(der []byte) error {
	s.installRuns++
	if s.onInstall != nil {
		s.onInstall()
	}
	if s.installErr == nil && !s.noWrite {
		s.certs[string(der)] = true
	}
	return s.installErr
}
func (s *testCATrustStore) uninstall(der []byte) error {
	s.deleteRuns++
	if s.deleteErr == nil {
		delete(s.certs, string(der))
	}
	return s.deleteErr
}

func newCATrustTestService(t *testing.T) (*SettingService, *testCATrustStore, *x509.Certificate) {
	t.Helper()
	s := newTestSettingServiceWithCAPaths(t)
	store := &testCATrustStore{certs: make(map[string]bool)}
	s.caTrustStore = store
	if _, err := s.GenerateCurrentCACertificate(GenerateCACertificateRequest{}); err != nil {
		t.Fatal(err)
	}
	path, _, _ := s.currentCAPaths()
	cert, _, err := readCAPublicCertificate(path)
	if err != nil {
		t.Fatal(err)
	}
	return s, store, cert
}

func TestCATrustInstallUninstallIdempotent(t *testing.T) {
	s, store, cert := newCATrustTestService(t)
	fingerprint := caCertificateFingerprint(cert)
	certPath, keyPath, _ := s.currentCAPaths()
	beforeCert, _ := os.ReadFile(certPath)
	beforeKey, _ := os.ReadFile(keyPath)
	for range 2 {
		status, err := s.InstallCurrentCACertificate(fingerprint)
		if err != nil || status == nil || !status.Installed || status.SHA256Fingerprint != fingerprint {
			t.Fatalf("install: status=%+v err=%v", status, err)
		}
	}
	for range 2 {
		status, err := s.UninstallCurrentCACertificate(fingerprint)
		if err != nil || status == nil || status.Installed {
			t.Fatalf("uninstall: status=%+v err=%v", status, err)
		}
	}
	if store.installRuns != 1 || store.deleteRuns != 1 {
		t.Fatalf("non-idempotent writes: %+v", store)
	}
	afterCert, _ := os.ReadFile(certPath)
	afterKey, _ := os.ReadFile(keyPath)
	if !bytes.Equal(beforeCert, afterCert) || !bytes.Equal(beforeKey, afterKey) {
		t.Fatal("trust changes altered CA files")
	}
}

func TestCATrustRejectsChangedFingerprint(t *testing.T) {
	s, store, cert := newCATrustTestService(t)
	old := caCertificateFingerprint(cert)
	if _, err := s.GenerateCurrentCACertificate(GenerateCACertificateRequest{Overwrite: true}); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(string) (*CACertificateTrustStatus, error){s.InstallCurrentCACertificate, s.UninstallCurrentCACertificate} {
		if _, err := change(old); !errors.Is(err, errCATrustCertificateChanged) {
			t.Fatalf("expected certificate change, got %v", err)
		}
	}
	if store.installRuns+store.deleteRuns != 0 {
		t.Fatal("changed certificate mutated trust")
	}
}

func TestCATrustUninstallWithoutPrivateKey(t *testing.T) {
	s, store, cert := newCATrustTestService(t)
	store.certs[string(cert.Raw)] = true
	_, keyPath, _ := s.currentCAPaths()
	if err := os.Remove(keyPath); err != nil {
		t.Fatal(err)
	}
	status, err := s.GetCurrentCACertificateTrustStatus()
	if err != nil || !status.Installed {
		t.Fatalf("query without key: %+v %v", status, err)
	}
	if _, err := s.InstallCurrentCACertificate(status.SHA256Fingerprint); !errors.Is(err, errCATrustInvalidPair) {
		t.Fatalf("install without key: %v", err)
	}
	if _, err := s.UninstallCurrentCACertificate(status.SHA256Fingerprint); err != nil {
		t.Fatal(err)
	}
	if store.certs[string(cert.Raw)] {
		t.Fatal("certificate still installed")
	}
}

func TestCATrustMalformedCertificateCanBeRepaired(t *testing.T) {
	s, store, _ := newCATrustTestService(t)
	certPath, _, _ := s.currentCAPaths()
	if err := os.WriteFile(certPath, []byte("invalid certificate"), 0644); err != nil {
		t.Fatal(err)
	}
	status, err := s.GetCurrentCACertificateTrustStatus()
	if err != nil || status.Error == "" || status.SHA256Fingerprint != "" {
		t.Fatalf("malformed certificate status: %+v %v", status, err)
	}
	store.queryErr = errors.New("store must not be queried for an invalid certificate")
	info, err := s.GenerateCurrentCACertificate(GenerateCACertificateRequest{Overwrite: true})
	if err != nil || !info.ValidPair || !info.IsCA {
		t.Fatalf("repair malformed certificate: %+v %v", info, err)
	}
}

func TestCATrustRegenerationFailsClosed(t *testing.T) {
	for _, queryFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "installed", true: "query failed"}[queryFailure], func(t *testing.T) {
			s, store, cert := newCATrustTestService(t)
			store.certs[string(cert.Raw)] = true
			if queryFailure {
				store.queryErr = errors.New("access denied")
			}
			if _, err := s.GenerateCurrentCACertificate(GenerateCACertificateRequest{Overwrite: true}); err == nil {
				t.Fatal("regeneration should fail")
			}
			certPath, _, _ := s.currentCAPaths()
			unchanged, _, err := readCAPublicCertificate(certPath)
			if err != nil || !bytes.Equal(cert.Raw, unchanged.Raw) {
				t.Fatal("blocked regeneration changed certificate")
			}
			backups, _ := filepath.Glob(certPath + ".bak*")
			if len(backups) > 0 {
				t.Fatal("blocked regeneration backed up certificate")
			}
			store.queryErr = nil
			if _, err := s.UninstallCurrentCACertificate(caCertificateFingerprint(cert)); err != nil {
				t.Fatal(err)
			}
			if _, err := s.GenerateCurrentCACertificate(GenerateCACertificateRequest{Overwrite: true}); err != nil {
				t.Fatalf("regenerate after uninstall: %v", err)
			}
		})
	}
}

func TestCATrustQueryAndMutationFailures(t *testing.T) {
	s, store, cert := newCATrustTestService(t)
	store.queryErr = errors.New("query denied")
	status, err := s.GetCurrentCACertificateTrustStatus()
	if err != nil || status.Error == "" || status.SHA256Fingerprint == "" {
		t.Fatalf("unknown trust status was lost: %+v %v", status, err)
	}
	if _, err := s.InstallCurrentCACertificate(status.SHA256Fingerprint); err == nil || store.installRuns != 0 {
		t.Fatal("installation must not run when query fails")
	}
	store.queryErr = nil
	store.installErr = errors.New("installation denied")
	if _, err := s.InstallCurrentCACertificate(caCertificateFingerprint(cert)); !errors.Is(err, store.installErr) {
		t.Fatalf("lost install error: %v", err)
	}
	store.certs[string(cert.Raw)] = true
	store.deleteErr = errors.New("removal denied")
	if _, err := s.UninstallCurrentCACertificate(caCertificateFingerprint(cert)); !errors.Is(err, store.deleteErr) {
		t.Fatalf("lost removal error: %v", err)
	}
	status, _ = s.GetCurrentCACertificateTrustStatus()
	if !status.Installed {
		t.Fatal("failed removal changed displayed state")
	}
}

func TestCATrustVerifiesWrite(t *testing.T) {
	s, store, cert := newCATrustTestService(t)
	store.noWrite = true
	if _, err := s.InstallCurrentCACertificate(caCertificateFingerprint(cert)); err == nil {
		t.Fatal("silent write failure was reported as success")
	}
}

// Reissue with the same key so these tests exercise CA validity independently
// of certificate/key matching. This never touches a Windows certificate store.
func reissueTestCA(t *testing.T, s *SettingService, mutate func(*x509.Certificate)) *x509.Certificate {
	t.Helper()
	certPath, keyPath, _ := s.currentCAPaths()
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	mutate(cert)
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, cert.PublicKey, pair.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0644); err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestCATrustInstallationValidation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*x509.Certificate)
		want   error
	}{
		{"not CA", func(c *x509.Certificate) { c.IsCA = false }, errCATrustInvalidCA},
		{"expired", func(c *x509.Certificate) { c.NotAfter = time.Now().Add(-time.Hour) }, errCATrustInvalidValidity},
		{"not yet valid", func(c *x509.Certificate) { c.NotBefore = time.Now().Add(time.Hour) }, errCATrustInvalidValidity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, store, _ := newCATrustTestService(t)
			cert := reissueTestCA(t, s, tc.mutate)
			if _, err := s.InstallCurrentCACertificate(caCertificateFingerprint(cert)); !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
			if store.installRuns != 0 {
				t.Fatal("invalid certificate was installed")
			}
			// Expired and non-CA certificates already in Root can still be removed.
			store.certs[string(cert.Raw)] = true
			if _, err := s.UninstallCurrentCACertificate(caCertificateFingerprint(cert)); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("key mismatch", func(t *testing.T) {
		s, store, cert := newCATrustTestService(t)
		_, keyPath, _ := s.currentCAPaths()
		_, otherKey, err := generateCACertificatePEM("other", 1)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(keyPath, otherKey, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.InstallCurrentCACertificate(caCertificateFingerprint(cert)); !errors.Is(err, errCATrustInvalidPair) || store.installRuns != 0 {
			t.Fatalf("expected key mismatch rejection, got %v", err)
		}
	})
}

func TestCATrustOperationsSerialize(t *testing.T) {
	s, store, cert := newCATrustTestService(t)
	entered, release := make(chan struct{}), make(chan struct{})
	store.onInstall = func() { close(entered); <-release }
	firstDone := make(chan error, 1)
	go func() { _, err := s.InstallCurrentCACertificate(caCertificateFingerprint(cert)); firstDone <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("installation did not start")
	}
	secondDone := make(chan error, 1)
	go func() {
		_, err := s.GenerateCurrentCACertificate(GenerateCACertificateRequest{Overwrite: true})
		secondDone <- err
	}()
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; !errors.Is(err, errCATrustUninstallFirst) {
		t.Fatalf("generation raced installation: %v", err)
	}
}

func TestCATrustUnsupported(t *testing.T) {
	s := &SettingService{caTrustStore: &testCATrustStore{unsupported: true}}
	status, err := s.GetCurrentCACertificateTrustStatus()
	if err != nil || status.Supported {
		t.Fatalf("unsupported query: %+v %v", status, err)
	}
	for _, change := range []func(string) (*CACertificateTrustStatus, error){s.InstallCurrentCACertificate, s.UninstallCurrentCACertificate} {
		if _, err := change(""); !errors.Is(err, errCATrustUnsupported) {
			t.Fatalf("unsupported mutation: %v", err)
		}
	}
}
