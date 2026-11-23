package proxyservice

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"slices"
	"testing"
	"time"

	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
)

// Custom CA fixtures belong to the test, not the app's generation API.
func writeCustomTestCA(t *testing.T, settings *settingservice.SettingService) {
	t.Helper()
	config, err := settings.GetProxyConfig()
	if err != nil {
		t.Fatal(err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "FlowLens Custom Test CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string][]byte{
		config.CACertPath: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		config.CAKeyPath:  pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}),
	} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProxyUsesSelectedCA(t *testing.T) {
	setTestConfigDir(t)
	service := newTestProxyService(t, &settingservice.ProxyConfig{
		UseBuiltinCA: true, CACertPath: t.TempDir() + "/custom.crt", CAKeyPath: t.TempDir() + "/custom.key",
		DisableProxy: true,
	})
	t.Cleanup(service.baseCancel)
	if err := service.settingService.ServiceStartup(t.Context()); err != nil {
		t.Fatal(err)
	}
	config, _ := service.getProxyConfig()
	certPath, _, err := settingservice.ResolveProxyCAPaths(config)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := service.buildHandlerLocked(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer handler.Cleanup()
	if handler.CACertPath() != certPath {
		t.Fatalf("builtin CA path = %q, want %q", handler.CACertPath(), certPath)
	}
	custom := *config
	custom.UseBuiltinCA = false
	if err := service.settingService.Update(&settingservice.Settings{ProxyConfig: &custom}); err != nil {
		t.Fatal(err)
	}
	writeCustomTestCA(t, service.settingService)
	customHandler, err := service.buildHandlerLocked(t.Context(), &custom)
	if err != nil {
		t.Fatal(err)
	}
	defer customHandler.Cleanup()
	if customHandler.CACertPath() != custom.CACertPath {
		t.Fatalf("custom CA path = %q, want %q", customHandler.CACertPath(), custom.CACertPath)
	}
}

func TestCASelectionRestartReasons(t *testing.T) {
	builtin := &settingservice.ProxyConfig{UseBuiltinCA: true, CACertPath: "remembered.crt", CAKeyPath: "remembered.key"}
	changed := *builtin
	changed.CACertPath = "other.crt"
	if proxyRestartRequired(builtin, &changed) {
		t.Fatal("inactive custom paths requested a restart")
	}
	changed.UseBuiltinCA = false
	if !proxyRestartRequired(builtin, &changed) {
		t.Fatal("changing CA mode did not request a restart")
	}
	custom := changed
	changed.CAKeyPath = "other.key"
	if !proxyRestartRequired(&custom, &changed) {
		t.Fatal("changing an active custom path did not request a restart")
	}
}

func TestProxyRestartReasonsKeepTheirOrder(t *testing.T) {
	start := &settingservice.ProxyConfig{Mode: settingservice.ProxyModeHTTP, Host: "127.0.0.1", Port: 8080, UseBuiltinCA: true}
	current := *start
	current.Mode = settingservice.ProxyModeSOCKS5
	current.Port = 8081
	current.UseBuiltinCA = false
	got := proxyRestartReasons(start, &current)
	want := []ProxyRestartReason{ProxyRestartReasonProxyMode, ProxyRestartReasonListenAddress, ProxyRestartReasonMITMCA}
	if !slices.Equal(got, want) {
		t.Fatalf("reasons = %q, want %q", got, want)
	}
	if string(ProxyRestartReasonProxyMode) != "proxy mode changed" || string(ProxyRestartReasonListenAddress) != "listen address changed" || string(ProxyRestartReasonMITMCA) != "mitm ca changed" {
		t.Fatal("restart reasons changed their logged values")
	}
}
