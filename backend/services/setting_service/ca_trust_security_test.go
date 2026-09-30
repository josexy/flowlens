package settingservice

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const testCASecurityPolicies = `<dict><key>kSecTrustSettingsPolicy</key><data>KoZIhvdjZAED</data></dict>
<dict><key>kSecTrustSettingsPolicy</key><data>KoZIhvdjZAEC</data></dict>`

// All commands are simulated, including on macOS. No test opens a real login
// keychain, requests authentication or changes a user's trust settings.
type testCASecurity struct {
	t             *testing.T
	keychain      string
	certs         map[string]*x509.Certificate
	trust         map[string]*x509.Certificate
	commands      [][]string
	snapshots     []string
	exports       []string
	failCommand   string
	cancelInstall bool
	silentRemove  bool
	trustAsRoot   bool
}

func newTestCASecurity(t *testing.T) *testCASecurity {
	return &testCASecurity{t: t, keychain: "/Users/test/Library/Keychains/login with spaces.keychain-db",
		certs: make(map[string]*x509.Certificate), trust: make(map[string]*x509.Certificate)}
}

func (s *testCASecurity) run(ctx context.Context, args ...string) ([]byte, error) {
	s.t.Helper()
	if _, ok := ctx.Deadline(); !ok {
		s.t.Fatal("security command has no timeout")
	}
	s.commands = append(s.commands, append([]string(nil), args...))
	if args[0] == s.failCommand {
		if args[0] == "add-trusted-cert" || args[0] == "remove-trusted-cert" {
			s.snapshot(args[len(args)-1])
		}
		if args[0] == "trust-settings-export" {
			s.exports = append(s.exports, args[1])
		}
		return []byte("authorization denied"), errors.New("exit status 1")
	}
	switch args[0] {
	case "login-keychain":
		if !reflect.DeepEqual(args, []string{"login-keychain", "-d", "user"}) {
			s.t.Fatalf("wrong keychain domain: %v", args)
		}
		return []byte(fmt.Sprintf("    \"%s\"\n", s.keychain)), nil
	case "find-certificate":
		if !reflect.DeepEqual(args, []string{"find-certificate", "-a", "-p", s.keychain}) {
			s.t.Fatalf("query not limited to login keychain: %v", args)
		}
		var out []byte
		for _, cert := range s.certs {
			out = append(out, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})...)
		}
		return out, nil
	case "trust-settings-export":
		if len(args) != 2 {
			s.t.Fatalf("query must use only user trust: %v", args)
		}
		s.exports = append(s.exports, args[1])
		if len(s.trust) == 0 {
			return []byte("SecTrustSettingsCreateExternalRepresentation: No Trust Settings were found."), errors.New("exit status 1")
		}
		var records strings.Builder
		for _, cert := range s.trust {
			policies := testCASecurityPolicies
			if s.trustAsRoot {
				policies = strings.ReplaceAll(policies, "</dict>", "<key>kSecTrustSettingsResult</key><integer>2</integer></dict>")
			}
			records.WriteString(testCASecurityRecord(cert, "<array>"+policies+"</array>"))
		}
		return nil, os.WriteFile(args[1], []byte(testCASecurityPlist(records.String())), 0600)
	case "add-trusted-cert":
		if len(args) != 10 || args[1] != "-r" ||
			!reflect.DeepEqual(args[3:9], []string{"-p", "ssl", "-p", "basic", "-k", s.keychain}) {
			s.t.Fatalf("installation scope/policies changed: %v", args)
		}
		cert := s.snapshot(args[9])
		expectedResult := "trustRoot"
		if s.trustAsRoot {
			expectedResult = "trustAsRoot"
		}
		if args[2] != expectedResult {
			s.t.Fatalf("wrong trust result: got %q want %q", args[2], expectedResult)
		}
		s.certs[string(cert.Raw)] = cert
		if s.cancelInstall {
			return []byte("User canceled the operation."), errors.New("exit status 1")
		}
		s.trust[string(cert.Raw)] = cert
		return nil, nil
	case "remove-trusted-cert":
		if len(args) != 2 {
			s.t.Fatalf("removal must use only user trust: %v", args)
		}
		cert := s.snapshot(args[1])
		if !s.silentRemove {
			delete(s.trust, string(cert.Raw))
		}
		return nil, nil
	case "delete-certificate":
		if len(args) != 4 || args[1] != "-Z" || len(args[2]) != 64 || args[3] != s.keychain {
			s.t.Fatalf("removal must use SHA256 and login keychain: %v", args)
		}
		for key, cert := range s.certs {
			if strings.ReplaceAll(caCertificateFingerprint(cert), ":", "") == strings.ToUpper(args[2]) {
				delete(s.certs, key)
			}
		}
		return nil, nil
	default:
		s.t.Fatalf("unexpected security command: %v", args)
		return nil, nil
	}
}

func (s *testCASecurity) snapshot(file string) *x509.Certificate {
	s.t.Helper()
	s.snapshots = append(s.snapshots, file)
	data, err := os.ReadFile(file)
	if err != nil {
		s.t.Fatal(err)
	}
	block, rest := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 {
		s.t.Fatal("snapshot must contain only one public certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		s.t.Fatal(err)
	}
	return cert
}

func (s *testCASecurity) writes(command string) int {
	count := 0
	for _, args := range s.commands {
		if args[0] == command {
			count++
		}
	}
	return count
}

func (s *testCASecurity) assertClean(t *testing.T) {
	t.Helper()
	for _, file := range append(append([]string(nil), s.snapshots...), s.exports...) {
		if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("temporary file survived: %s: %v", file, err)
		}
	}
	for _, file := range s.exports {
		if _, err := os.Stat(filepath.Dir(file)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("temporary trust directory survived: %s: %v", file, err)
		}
	}
}

func testCASecurityPlist(records string) string {
	return `<?xml version="1.0"?><plist version="1.0"><dict><key>trustVersion</key><integer>1</integer><key>trustList</key><dict>` + records + `</dict></dict></plist>`
}

func testCASecurityRecord(cert *x509.Certificate, settings string) string {
	hash := sha1.Sum(cert.Raw)
	if settings != "" {
		settings = "<key>trustSettings</key>" + settings
	}
	return fmt.Sprintf(`<key>%s</key><dict><key>issuerName</key><data>%s</data><key>serialNumber</key><data>%s</data>%s</dict>`,
		strings.ToUpper(hex.EncodeToString(hash[:])), base64.StdEncoding.EncodeToString(cert.RawIssuer),
		base64.StdEncoding.EncodeToString(cert.SerialNumber.Bytes()),
		settings)
}

func TestSecurityCATrustLifecycle(t *testing.T) {
	s, _, cert := newCATrustTestService(t)
	fake := newTestCASecurity(t)
	s.caTrustStore = securityCACertificateTrustStore{run: fake.run}
	// A different certificate with the same common name must survive both
	// installation and removal of the configured CA.
	otherPEM, _, err := generateCACertificatePEM(cert.Subject.CommonName, 1)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(otherPEM)
	other, _ := x509.ParseCertificate(block.Bytes)
	fake.certs[string(other.Raw)], fake.trust[string(other.Raw)] = other, other
	fingerprint := caCertificateFingerprint(cert)
	for range 2 {
		status, err := s.InstallCurrentCACertificate(fingerprint)
		if err != nil || !status.Present || !status.Installed {
			t.Fatalf("installation: %+v %v", status, err)
		}
	}
	_, keyPath, _ := s.currentCAPaths()
	if err := os.Remove(keyPath); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		status, err := s.UninstallCurrentCACertificate(fingerprint)
		if err != nil || status.Present || status.Installed {
			t.Fatalf("removal: %+v %v", status, err)
		}
	}
	if fake.writes("add-trusted-cert") != 1 || fake.writes("remove-trusted-cert") != 1 || fake.writes("delete-certificate") != 1 {
		t.Fatalf("non-idempotent commands: %v", fake.commands)
	}
	if fake.certs[string(other.Raw)] == nil || fake.trust[string(other.Raw)] == nil {
		t.Fatal("same-name certificate was altered")
	}
	if _, err := s.GenerateCurrentCACertificate(GenerateCACertificateRequest{Overwrite: true}); err != nil {
		t.Fatal(err)
	}
	fake.assertClean(t)
}

func TestSecurityCATrustCanceledInstallation(t *testing.T) {
	s, _, cert := newCATrustTestService(t)
	fake := newTestCASecurity(t)
	fake.cancelInstall = true
	s.caTrustStore = securityCACertificateTrustStore{run: fake.run}
	fingerprint := caCertificateFingerprint(cert)
	if _, err := s.InstallCurrentCACertificate(fingerprint); err == nil {
		t.Fatal("canceled authentication was reported as success")
	}
	status, err := s.GetCurrentCACertificateTrustStatus()
	if err != nil || !status.Present || status.Installed || status.Error != "" {
		t.Fatalf("incomplete installation lost: %+v %v", status, err)
	}
	if _, err := s.GenerateCurrentCACertificate(GenerateCACertificateRequest{Overwrite: true}); !errors.Is(err, errCATrustUninstallFirst) {
		t.Fatalf("regeneration must preserve imported CA: %v", err)
	}
	if _, err := s.UninstallCurrentCACertificate(fingerprint); err != nil {
		t.Fatal(err)
	}
	if fake.writes("remove-trusted-cert") != 0 || fake.writes("delete-certificate") != 1 {
		t.Fatal("an imported untrusted certificate was not cleaned up")
	}
	fake.assertClean(t)
}

func TestSecurityCATrustNonSelfSignedCA(t *testing.T) {
	s, _, cert := newCATrustTestService(t)
	certPath, _, _ := s.currentCAPaths()
	parentPEM, parentKey, err := generateCACertificatePEM("parent CA", 1)
	if err != nil {
		t.Fatal(err)
	}
	parentPair, err := tls.X509KeyPair(parentPEM, parentKey)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := x509.ParseCertificate(parentPair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, parent, cert.PublicKey, parentPair.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0644); err != nil {
		t.Fatal(err)
	}
	cert, err = x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	fake := newTestCASecurity(t)
	fake.trustAsRoot = true
	s.caTrustStore = securityCACertificateTrustStore{run: fake.run}
	status, err := s.InstallCurrentCACertificate(caCertificateFingerprint(cert))
	if err != nil || !status.Installed {
		t.Fatalf("non-self-signed CA: %+v %v", status, err)
	}
	fake.assertClean(t)
}

func TestSecurityCATrustRejectsMalformedQueryOutput(t *testing.T) {
	_, _, cert := newCATrustTestService(t)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	for _, out := range [][]byte{[]byte("malformed output"), append([]byte("keychain could not be opened\n"), certPEM...), append(append([]byte(nil), certPEM...), []byte("query failed\n")...)} {
		s := securityCACertificateTrustStore{run: func(context.Context, ...string) ([]byte, error) { return out, nil }}
		if _, err := s.certificatePresent("login.keychain-db", cert.Raw); err == nil {
			t.Fatal("query diagnostics were ignored")
		}
	}
}

func TestSecurityCATrustOrphanAndPartialRemoval(t *testing.T) {
	s, _, cert := newCATrustTestService(t)
	fingerprint := caCertificateFingerprint(cert)
	for _, mode := range []string{"orphan", "delete denied", "trust denied", "silent trust removal"} {
		t.Run(mode, func(t *testing.T) {
			fake := newTestCASecurity(t)
			fake.trust[string(cert.Raw)] = cert
			if mode != "orphan" {
				fake.certs[string(cert.Raw)] = cert
			}
			switch mode {
			case "delete denied":
				fake.failCommand = "delete-certificate"
			case "trust denied":
				fake.failCommand = "remove-trusted-cert"
			case "silent trust removal":
				fake.silentRemove = true
			}
			s.caTrustStore = securityCACertificateTrustStore{run: fake.run}
			_, err := s.UninstallCurrentCACertificate(fingerprint)
			if (err != nil) != (mode != "orphan") {
				t.Fatalf("removal error: %v", err)
			}
			status, err := s.GetCurrentCACertificateTrustStatus()
			if err != nil || status.Error != "" || status.Present != (mode != "orphan") {
				t.Fatalf("residual state: %+v %v", status, err)
			}
			if mode == "delete denied" && status.Installed {
				t.Fatal("certificate without user trust reported as installed")
			}
			if mode == "trust denied" && fake.writes("delete-certificate") != 0 {
				t.Fatal("failed trust removal deleted the certificate")
			}
			fake.assertClean(t)
		})
	}
}

func TestSecurityCATrustQueryFailsClosed(t *testing.T) {
	s, _, cert := newCATrustTestService(t)
	for _, command := range []string{"login-keychain", "find-certificate", "trust-settings-export"} {
		t.Run(command, func(t *testing.T) {
			fake := newTestCASecurity(t)
			fake.failCommand = command
			s.caTrustStore = securityCACertificateTrustStore{run: fake.run}
			status, err := s.GetCurrentCACertificateTrustStatus()
			if err != nil || !status.Supported || status.Error == "" {
				t.Fatalf("query failure must be unknown: %+v %v", status, err)
			}
			if _, err := s.InstallCurrentCACertificate(caCertificateFingerprint(cert)); err == nil || fake.writes("add-trusted-cert") != 0 {
				t.Fatal("query failure allowed installation")
			}
			if _, err := s.GenerateCurrentCACertificate(GenerateCACertificateRequest{Overwrite: true}); err == nil {
				t.Fatal("query failure allowed regeneration")
			}
			fake.assertClean(t)
		})
	}
}

func TestSecurityCATrustSettingsPolicies(t *testing.T) {
	_, _, cert := newCATrustTestService(t)
	cases := []struct {
		name, settings string
		trusted        bool
	}{
		{"explicit ssl and basic", "<array>" + testCASecurityPolicies + "</array>", true},
		{"unrestricted", "", true},
		{"empty array", "<array/>", true},
		{"ssl only", `<array><dict><key>kSecTrustSettingsPolicy</key><data>KoZIhvdjZAED</data></dict></array>`, false},
		{"string policy OIDs", `<array><dict><key>kSecTrustSettingsPolicy</key><string>1.2.840.113635.100.1.3</string></dict><dict><key>kSecTrustSettingsPolicy</key><string>1.2.840.113635.100.1.2</string></dict></array>`, true},
		{"hostname constraint", `<array><dict><key>kSecTrustSettingsPolicyString</key><string>example.com</string></dict></array>`, false},
		{"application constraint", `<array><dict><key>kSecTrustSettingsApplication</key><data>YWJj</data></dict></array>`, false},
		{"denied", `<array><dict><key>kSecTrustSettingsResult</key><integer>3</integer></dict></array>`, false},
		{"deny overrides allow", "<array>" + testCASecurityPolicies + `<dict><key>kSecTrustSettingsResult</key><integer>3</integer></dict></array>`, false},
		{"unspecified", `<array><dict><key>kSecTrustSettingsResult</key><integer>4</integer></dict></array>`, false},
		{"allowed error", `<array><dict><key>kSecTrustSettingsAllowedError</key><integer>-1</integer></dict></array>`, false},
		{"key usage", `<array><dict><key>kSecTrustSettingsKeyUsage</key><integer>1</integer></dict></array>`, false},
		{"unknown constraint", `<array><dict><key>futureConstraint</key><string>value</string></dict></array>`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			present, trusted, err := caSecurityTrustSettings([]byte(testCASecurityPlist(testCASecurityRecord(cert, tc.settings))), cert)
			if err != nil || !present || trusted != tc.trusted {
				t.Fatalf("trust settings: present=%t trusted=%t err=%v", present, trusted, err)
			}
		})
	}
	valid := testCASecurityPlist(testCASecurityRecord(cert, "<array>"+testCASecurityPolicies+"</array>"))
	for _, invalid := range []string{"", "not xml", strings.Replace(valid, "<integer>1</integer>", "<integer>2</integer>", 1),
		strings.Replace(valid, base64.StdEncoding.EncodeToString(cert.RawIssuer), "YWJj", 1),
		strings.Replace(valid, "<key>trustList</key>", "<key>trustVersion</key>", 1)} {
		if _, _, err := caSecurityTrustSettings([]byte(invalid), cert); err == nil {
			t.Fatal("invalid trust settings were accepted")
		}
	}
}

func TestSecurityCACommandBounds(t *testing.T) {
	s := securityCACertificateTrustStore{run: func(ctx context.Context, _ ...string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	if _, err := s.command(time.Millisecond, "test"); !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "ca_trust_timeout") {
		t.Fatalf("timeout error lost: %v", err)
	}
	var output caSecurityOutput
	_, _ = output.Write(make([]byte, caSecurityOutputLimit+1))
	_, _ = output.Write([]byte("more output"))
	if !output.overflow || output.buffer.Len() != caSecurityOutputLimit {
		t.Fatal("output was not bounded")
	}
	for _, keychain := range []string{"", "relative.keychain", "/Library/Keychains/System.keychain", "/System/Library/Keychains/SystemRootCertificates.keychain"} {
		fake := newTestCASecurity(t)
		fake.keychain = keychain
		s := securityCACertificateTrustStore{run: fake.run}
		if _, err := s.loginKeychain(); err == nil {
			t.Fatalf("unsafe login keychain accepted: %q", keychain)
		}
	}
}
