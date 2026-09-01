package systemproxy

import "testing"

func TestProxyURLFromWindowsProxyServer(t *testing.T) {
	tests := []struct {
		name        string
		proxyServer string
		want        string
	}{
		{
			name:        "single proxy",
			proxyServer: "127.0.0.1:7890",
			want:        "http://127.0.0.1:7890",
		},
		{
			name:        "http entry",
			proxyServer: "http=127.0.0.1:8080;https=127.0.0.1:8443",
			want:        "http://127.0.0.1:8080",
		},
		{
			name:        "https entry uses http proxy scheme",
			proxyServer: "https=secure-proxy.example:8443",
			want:        "http://secure-proxy.example:8443",
		},
		{
			name:        "socks entry",
			proxyServer: "socks=127.0.0.1:1080",
			want:        "socks5://127.0.0.1:1080",
		},
		{
			name:        "keeps explicit scheme",
			proxyServer: "http=http://proxy.example:8080",
			want:        "http://proxy.example:8080",
		},
		{
			name:        "invalid entry",
			proxyServer: "http=://bad",
			want:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := proxyURLFromWindowsProxyServer(tt.proxyServer); got != tt.want {
				t.Fatalf("proxyURLFromWindowsProxyServer() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProxyURLFromSystemProxyCandidates(t *testing.T) {
	tests := []struct {
		name       string
		candidates []systemProxyCandidate
		want       string
	}{
		{
			name: "http has priority",
			candidates: []systemProxyCandidate{
				{Enabled: true, Host: "127.0.0.1", Port: 8080, DefaultScheme: "http"},
				{Enabled: true, Host: "secure-proxy.example", Port: 8443, DefaultScheme: "http"},
			},
			want: "http://127.0.0.1:8080",
		},
		{
			name: "https falls back to http proxy scheme",
			candidates: []systemProxyCandidate{
				{Enabled: false, Host: "127.0.0.1", Port: 8080, DefaultScheme: "http"},
				{Enabled: true, Host: "secure-proxy.example", Port: 8443, DefaultScheme: "http"},
			},
			want: "http://secure-proxy.example:8443",
		},
		{
			name: "socks fallback",
			candidates: []systemProxyCandidate{
				{Enabled: false, Host: "127.0.0.1", Port: 8080, DefaultScheme: "http"},
				{Enabled: false, Host: "secure-proxy.example", Port: 8443, DefaultScheme: "http"},
				{Enabled: true, Host: "127.0.0.1", Port: 1080, DefaultScheme: "socks5"},
			},
			want: "socks5://127.0.0.1:1080",
		},
		{
			name: "keeps explicit scheme and port",
			candidates: []systemProxyCandidate{
				{Enabled: true, Host: "http://proxy.example:9000", Port: 8080, DefaultScheme: "http"},
			},
			want: "http://proxy.example:9000",
		},
		{
			name: "adds port to explicit scheme",
			candidates: []systemProxyCandidate{
				{Enabled: true, Host: "http://proxy.example", Port: 8080, DefaultScheme: "http"},
			},
			want: "http://proxy.example:8080",
		},
		{
			name: "joins ipv6 host and port",
			candidates: []systemProxyCandidate{
				{Enabled: true, Host: "::1", Port: 1080, DefaultScheme: "socks5"},
			},
			want: "socks5://[::1]:1080",
		},
		{
			name: "disabled candidates are ignored",
			candidates: []systemProxyCandidate{
				{Enabled: false, Host: "127.0.0.1", Port: 8080, DefaultScheme: "http"},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := proxyURLFromSystemProxyCandidates(tt.candidates); got != tt.want {
				t.Fatalf("proxyURLFromSystemProxyCandidates() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFromEnvironmentNormalizesBareHost(t *testing.T) {
	t.Setenv("HTTP_PROXY", "127.0.0.1:8888")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("REQUEST_METHOD", "")

	if got := FromEnvironment(); got != "http://127.0.0.1:8888" {
		t.Fatalf("FromEnvironment() = %q, want %q", got, "http://127.0.0.1:8888")
	}
}
