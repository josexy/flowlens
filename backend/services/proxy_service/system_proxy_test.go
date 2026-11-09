package proxyservice

import (
	"testing"

	"github.com/josexy/flowlens/backend/pkg/systemproxy"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
)

func TestResolveEffectiveUpstreamProxyUsesIndependentMode(t *testing.T) {
	tests := []struct {
		name string
		cfg  *settingservice.ProxyConfig
		want string
	}{
		{
			name: "none ignores configured upstream proxy",
			cfg: &settingservice.ProxyConfig{
				UpstreamMode:  settingservice.UpstreamProxyModeNone,
				UpstreamProxy: "http://proxy.example:8080",
			},
			want: "",
		},
		{
			name: "custom trims configured upstream proxy",
			cfg: &settingservice.ProxyConfig{
				UpstreamMode:  settingservice.UpstreamProxyModeCustom,
				UpstreamProxy: " http://proxy.example:8080 ",
			},
			want: "http://proxy.example:8080",
		},
		{
			name: "legacy custom falls back from upstream proxy value",
			cfg: &settingservice.ProxyConfig{
				UpstreamProxy: "http://proxy.example:8080",
			},
			want: "http://proxy.example:8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveEffectiveUpstreamProxy(tt.cfg); got != tt.want {
				t.Fatalf("resolveEffectiveUpstreamProxy() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSystemProxyEndpointFromConfigUsesLoopbackForAll(t *testing.T) {
	endpoint, err := systemProxyEndpointFromConfig(&settingservice.ProxyConfig{
		Mode: settingservice.ProxyModeHTTP,
		Host: "0.0.0.0",
		Port: 8080,
	})
	if err != nil {
		t.Fatalf("systemProxyEndpointFromConfig: %v", err)
	}
	want := systemproxy.Endpoint{Mode: systemproxy.ModeHTTP, Host: "127.0.0.1", Port: 8080}
	if endpoint != want {
		t.Fatalf("endpoint = %#v, want %#v", endpoint, want)
	}
}

func TestResolveEffectiveUpstreamProxyWithCapturedSystemProxy(t *testing.T) {
	cfg := &settingservice.ProxyConfig{UpstreamMode: settingservice.UpstreamProxyModeSystem}
	if got := resolveEffectiveUpstreamProxyWithSystem(cfg, "http://proxy.example:3128"); got != "http://proxy.example:3128" {
		t.Fatalf("resolved upstream = %q", got)
	}
	if got := resolveEffectiveUpstreamProxyWithSystem(cfg, ""); got != "" {
		t.Fatalf("empty captured upstream = %q, want direct", got)
	}
}

func TestProxyURLMatchesEndpoint(t *testing.T) {
	endpoint := systemproxy.Endpoint{Mode: systemproxy.ModeHTTP, Host: "127.0.0.1", Port: 8080}
	if !proxyURLMatchesEndpoint("http://127.0.0.1:8080", endpoint) {
		t.Fatal("expected proxy URL to match endpoint")
	}
	if proxyURLMatchesEndpoint("http://127.0.0.1:8081", endpoint) {
		t.Fatal("unexpected proxy URL match")
	}
}

func TestInactiveSystemProxyStatusUsesValidDefaultMode(t *testing.T) {
	status := systemProxyStatusFromController(
		systemproxy.ControllerState{Supported: true},
		settingservice.ProxyModeHTTP,
		true,
	)
	if status.Mode != settingservice.ProxyModeHTTP || !status.ModeSupported || status.Active || status.Address != "" {
		t.Fatalf("unexpected inactive system proxy status: %#v", status)
	}
}

func TestSetSystemProxyEnabledRejectedDuringShutdown(t *testing.T) {
	svc := &ProxyService{
		systemProxy:             systemproxy.NewController(),
		systemProxyShuttingDown: true,
	}
	if _, err := svc.SetSystemProxyEnabled(true); err == nil {
		t.Fatal("expected system proxy enable to be rejected during shutdown")
	}
}
