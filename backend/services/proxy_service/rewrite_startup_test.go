package proxyservice

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/josexy/flowlens/backend/pkg/database"
	rewriteservice "github.com/josexy/flowlens/backend/services/rewrite_service"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	http "github.com/josexy/xhttp"
	"github.com/josexy/xhttp/httptest"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestProxyStartWaitsForSavedRewriteRules(t *testing.T) {
	setTestConfigDir(t)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.Header.Get("X-Rule-Ready"))
	}))
	defer origin.Close()
	db, err := database.OpenAt(filepath.Join(t.TempDir(), "rules.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seed := rewriteservice.New(db)
	if err := seed.Load(); err != nil {
		t.Fatal(err)
	}
	rule := rewriteTestRule(rewriteservice.ActionRequest)
	rule.URLPattern = origin.URL + "/*"
	rule.Action.Headers = []rewriteservice.FieldOperation{{Operation: "set", Name: "X-Rule-Ready", Value: "yes"}}
	state, err := seed.SaveRule(rule, 0)
	if err != nil {
		t.Fatal(err)
	}
	state, err = seed.SetRuleEnabled(state.Rules[0].ID, true, state.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seed.SetEnabled(true, state.Revision); err != nil {
		t.Fatal(err)
	}
	seed.Shutdown()
	dir := t.TempDir()
	svc := newTestProxyService(t, &settingservice.ProxyConfig{
		Mode: settingservice.ProxyModeHTTP, Host: "127.0.0.1", Port: reserveTestTCPPort(t),
		CACertPath: filepath.Join(dir, "ca.crt"), CAKeyPath: filepath.Join(dir, "ca.key"), DisableProxy: true,
	})
	defer svc.Shutdown()
	if _, err := svc.settingService.GenerateCurrentCACertificate(settingservice.GenerateCACertificateRequest{}); err != nil {
		t.Fatal(err)
	}
	rules := rewriteservice.New(db)
	defer rules.Shutdown()
	SetRewriteService(svc, rules)
	held, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if err := rules.ServiceStartup(t.Context(), application.ServiceOptions{}); err != nil {
		t.Fatal(err)
	}
	startDone := make(chan error, 1)
	go func() { _, err := svc.Start(); startDone <- err }()
	select {
	case err := <-startDone:
		t.Fatalf("proxy start completed before rules loaded: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if svc.GetStatus().Running {
		t.Fatal("proxy published running state before rule readiness")
	}
	cfg, err := svc.getProxyConfig()
	if err != nil {
		t.Fatal(err)
	}
	probe, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP(cfg.Host), Port: cfg.Port})
	if err != nil {
		t.Fatalf("proxy bound its port while rules were unavailable: %v", err)
	}
	probe.Close()
	held.Close()
	select {
	case err := <-startDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("proxy start did not resume after rule loading")
	}
	proxyURL, _ := url.Parse("http://" + svc.GetStatus().Address)
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	resp, err := client.Get(origin.URL + "/first")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || string(body) != "yes" {
		t.Fatalf("first proxy request bypassed saved rules: %q, %v", body, err)
	}
}

func TestProxyStartRejectsFailedRuleLoad(t *testing.T) {
	db, err := database.OpenAt(filepath.Join(t.TempDir(), "rules.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	rules := rewriteservice.New(db)
	defer rules.Shutdown()
	if err := rules.ServiceStartup(t.Context(), application.ServiceOptions{}); err != nil {
		t.Fatal(err)
	}
	svc := newTestProxyService(t, nil)
	defer svc.baseCancel()
	SetRewriteService(svc, rules)
	_, err = svc.Start()
	if err == nil || !strings.Contains(err.Error(), "rewrite rules unavailable") || !strings.Contains(err.Error(), "database is closed") {
		t.Fatalf("proxy startup hid the rule load failure: %v", err)
	}
	if svc.GetStatus().Running {
		t.Fatal("proxy started after failed rule loading")
	}
}

func TestProxyShutdownInterruptsStartWaitingForRules(t *testing.T) {
	setTestConfigDir(t)
	db, err := database.OpenAt(filepath.Join(t.TempDir(), "rules.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	held, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	rules := rewriteservice.New(db)
	defer rules.Shutdown()
	if err := rules.ServiceStartup(t.Context(), application.ServiceOptions{}); err != nil {
		t.Fatal(err)
	}
	svc := newTestProxyService(t, nil)
	SetRewriteService(svc, rules)
	entered := make(chan struct{})
	svc.lifecycleOperationHook = func(operation string) {
		if operation == "start" {
			close(entered)
		}
	}
	startDone := make(chan error, 1)
	go func() { _, err := svc.Start(); startDone <- err }()
	<-entered
	stopDone := make(chan error, 1)
	go func() { stopDone <- svc.Shutdown() }()
	for _, done := range []<-chan error{startDone, stopDone} {
		select {
		case err := <-done:
			if done == startDone && !errors.Is(err, context.Canceled) || done == stopDone && err != nil {
				t.Fatalf("unexpected lifecycle result: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("shutdown deadlocked with proxy startup waiting for rules")
		}
	}
}
