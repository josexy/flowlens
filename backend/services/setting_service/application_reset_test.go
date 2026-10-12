package settingservice

import (
	"context"
	"crypto/x509"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestApplicationResetRemovesOnlyBuiltinTrustInCustomMode(t *testing.T) {
	service, store, certificate := newCATrustTestService(t)
	store.certs[string(certificate.Raw)] = true
	store.certs["external custom certificate"] = true
	external := filepath.Join(t.TempDir(), "external.crt")
	if err := os.WriteFile(external, []byte("external custom certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, _ := service.Get()
	settings.ProxyConfig.UseBuiltinCA = false
	settings.ProxyConfig.CACertPath = external
	settings.ProxyConfig.CAKeyPath = external + ".key"
	guard, err := BeginApplicationReset(service)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Cancel()
	if err := guard.RemoveBuiltinCATrust(); err != nil {
		t.Fatal(err)
	}
	if store.certs[string(certificate.Raw)] || !store.certs["external custom certificate"] || store.deleteRuns != 1 {
		t.Fatalf("incorrect trust removal: %+v", store)
	}
	if _, err := os.Stat(external); err != nil {
		t.Fatal("external certificate was modified")
	}
	if _, err := os.Stat(guard.certPath); err != nil {
		t.Fatal("CA file removed before shutdown")
	}
}

func TestApplicationResetUninstallsExpiredCAWithoutKey(t *testing.T) {
	service, store, _ := newCATrustTestService(t)
	certificate := reissueTestCA(t, service, func(cert *x509.Certificate) { cert.NotAfter = time.Now().Add(-time.Hour) })
	store.certs[string(certificate.Raw)] = true
	_, keyPath, _ := service.currentCAPaths()
	if err := os.Remove(keyPath); err != nil {
		t.Fatal(err)
	}
	guard, err := BeginApplicationReset(service)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Cancel()
	if err := guard.RemoveBuiltinCATrust(); err != nil || store.certs[string(certificate.Raw)] {
		t.Fatalf("expired/missing-key uninstall: %v", err)
	}
}

func TestApplicationResetFailsClosedAndCancellationReleasesService(t *testing.T) {
	for _, stage := range []string{"query", "uninstall", "changed", "malformed"} {
		t.Run(stage, func(t *testing.T) {
			service, store, certificate := newCATrustTestService(t)
			store.certs[string(certificate.Raw)] = true
			failure := errors.New("native trust operation failed")
			if stage == "query" {
				store.queryErr = failure
			}
			if stage == "malformed" {
				certPath, _, _ := service.currentCAPaths()
				if err := os.WriteFile(certPath, []byte("invalid"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			guard, err := BeginApplicationReset(service)
			if stage == "query" || stage == "malformed" {
				if err == nil || service.applicationReset.Load() || store.deleteRuns != 0 {
					t.Fatalf("failed preflight changed service: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Cancel()
			if stage == "uninstall" {
				store.deleteErr = failure
			} else {
				reissueTestCA(t, service, func(certificate *x509.Certificate) { certificate.NotAfter = certificate.NotAfter.Add(-time.Minute) })
			}
			if err := guard.RemoveBuiltinCATrust(); err == nil {
				t.Fatal("trust failure accepted")
			}
			guard.Cancel()
			if service.applicationReset.Load() || !store.certs[string(certificate.Raw)] {
				t.Fatal("failed reset did not restore service or retained trust")
			}
			if _, err := service.Get(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestApplicationResetBlocksSettingsAndCertificateWrites(t *testing.T) {
	configureTestSettingsPath(t)
	service := newPersistentTestSettingService(t)
	service.caTrustStore = &testCATrustStore{certs: make(map[string]bool)}
	if err := service.Load(); err != nil {
		t.Fatal(err)
	}
	settings, _ := service.Get()
	if err := service.Save(); err != nil {
		t.Fatal(err)
	}
	guard, err := BeginApplicationReset(service)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Cancel()
	guard.Commit()
	for _, write := range []func() error{
		service.Save,
		func() error { return service.UpdatePreservingShortcuts(settings) },
		func() error { return service.Update(settings) },
		func() error { return service.SaveTrafficTableConfig(settings.TrafficTableConfig) },
		func() error { return service.SaveTrafficDetailConfig(settings.TrafficDetailConfig) },
		func() error { return service.SaveShortcutConfig(settings.Shortcuts) },
		func() error { return service.SavePythonPluginConfig(settings.PythonPluginConfig) },
		func() error {
			_, err := service.GenerateCurrentCACertificate(GenerateCACertificateRequest{})
			return err
		},
	} {
		if err := write(); err == nil || !strings.Contains(err.Error(), "application_reset_in_progress") {
			t.Fatalf("write not blocked: %v", err)
		}
	}
}

func TestApplicationResetWaitsForExistingSaveAndBlocksQueuedSave(t *testing.T) {
	configureTestSettingsPath(t)
	service := newPersistentTestSettingService(t)
	service.caTrustStore = &testCATrustStore{certs: make(map[string]bool)}
	if err := service.Load(); err != nil {
		t.Fatal(err)
	}
	connection, err := service.repository.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	saveDone := make(chan error, 1)
	go func() { saveDone <- service.Save() }()
	deadline := time.Now().Add(5 * time.Second)
	for service.caMu.TryLock() {
		service.caMu.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("existing save never began")
		}
		time.Sleep(time.Millisecond)
	}
	done := make(chan *ApplicationResetGuard, 1)
	go func() {
		guard, err := BeginApplicationReset(service)
		if err != nil {
			done <- nil
			return
		}
		done <- guard
	}()
	select {
	case <-done:
		t.Fatal("reset bypassed CA/save serialization")
	case <-time.After(20 * time.Millisecond):
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-saveDone; err != nil {
		t.Fatal(err)
	}
	select {
	case guard := <-done:
		if guard == nil {
			t.Fatal("reset failed after save released")
		}
		if err := service.Save(); err == nil {
			guard.Cancel()
			t.Fatal("save was accepted while reset preparation held serialization")
		}
		guard.Cancel()
		if err := service.Save(); err != nil {
			t.Fatalf("canceled reset did not restore saves: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reset never acquired save serialization")
	}
}

type incompleteResetTrustStore struct{ *testCATrustStore }

func (store incompleteResetTrustStore) status(der []byte) (caTrustState, error) {
	state, err := store.testCATrustStore.status(der)
	state.installed = false
	return state, err
}

func TestApplicationResetRemovesIncompleteTrust(t *testing.T) {
	service, store, certificate := newCATrustTestService(t)
	store.certs[string(certificate.Raw)] = true
	service.caTrustStore = incompleteResetTrustStore{store}
	guard, err := BeginApplicationReset(service)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Cancel()
	if err := guard.RemoveBuiltinCATrust(); err != nil || store.deleteRuns != 1 || store.certs[string(certificate.Raw)] {
		t.Fatalf("incomplete trust not removed: %v", err)
	}
}
