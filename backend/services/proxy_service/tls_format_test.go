package proxyservice

import (
	"crypto/tls"
	"testing"
)

func TestFormatSupportedTLSVersionLabelsGrease(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version uint16
		want    string
	}{
		{name: "tls 1.3", version: tls.VersionTLS13, want: "TLS 1.3"},
		{name: "tls 1.2", version: tls.VersionTLS12, want: "TLS 1.2"},
		{name: "grease 7a7a", version: 0x7a7a, want: "GREASE (0x7A7A)"},
		{name: "unknown non grease", version: 0x1234, want: "0x1234"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := formatSupportedTLSVersion(tt.version); got != tt.want {
				t.Fatalf("formatSupportedTLSVersion(%#04x) = %q, want %q", tt.version, got, tt.want)
			}
		})
	}
}

func TestFormatSupportedTLSCipherSuiteLabelsGrease(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cipherSuite uint16
		want        string
	}{
		{
			name:        "known cipher suite",
			cipherSuite: tls.TLS_AES_128_GCM_SHA256,
			want:        "TLS_AES_128_GCM_SHA256",
		},
		{name: "grease aaaa", cipherSuite: 0xaaaa, want: "GREASE (0xAAAA)"},
		{name: "unknown non grease", cipherSuite: 0x1234, want: "0x1234"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := formatSupportedTLSCipherSuite(tt.cipherSuite); got != tt.want {
				t.Fatalf(
					"formatSupportedTLSCipherSuite(%#04x) = %q, want %q",
					tt.cipherSuite,
					got,
					tt.want,
				)
			}
		})
	}
}
