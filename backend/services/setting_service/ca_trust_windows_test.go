//go:build windows

package settingservice

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsCATrustCancellationError(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		canceled bool
	}{
		{"success", nil, false},
		{"native cancellation", windows.ERROR_CANCELLED, true},
		{"wrapped cancellation", fmt.Errorf("certificate operation: %w", windows.ERROR_CANCELLED), true},
		{"access denied", windows.ERROR_ACCESS_DENIED, false},
		{"operation aborted", windows.ERROR_OPERATION_ABORTED, false},
		{"message without native code", errors.New("The operation was canceled by the user."), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := normalizeWindowsCATrustError(tc.err)
			if errors.Is(err, errCATrustCanceled) != tc.canceled {
				t.Fatalf("unexpected cancellation classification: %v", err)
			}
			if !errors.Is(err, tc.err) {
				t.Fatalf("native error was lost: %v", err)
			}
		})
	}
}

func TestWindowsCATrustExactCertificateInMemory(t *testing.T) {
	store, err := windows.CertOpenStore(windows.CERT_STORE_PROV_MEMORY, 0, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CertCloseStore(store, 0)
	certificates := make([]*x509.Certificate, 2)
	for i := range certificates {
		data, _, err := generateCACertificatePEM("Same subject", 1)
		if err != nil {
			t.Fatal(err)
		}
		block, _ := pem.Decode(data)
		certificates[i], err = x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		if err := addWindowsCertificate(store, certificates[i].Raw); err != nil {
			t.Fatal(err)
		}
	}
	// Deliberately create a duplicate to exercise complete removal and context
	// ownership. No real Root store is opened or modified by this test.
	if err := addWindowsCertificate(store, certificates[0].Raw); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := deleteWindowsCertificates(store, certificates[0].Raw); err != nil {
			t.Fatal(err)
		}
	}
	deleted, err := windowsStoreContainsCertificate(store, certificates[0].Raw)
	if err != nil || deleted {
		t.Fatalf("exact certificate still present: %t %v", deleted, err)
	}
	other, err := windowsStoreContainsCertificate(store, certificates[1].Raw)
	if err != nil || !other {
		t.Fatalf("same-subject certificate was removed: %t %v", other, err)
	}
}
