//go:build windows

package settingservice

import (
	"crypto/x509"
	"encoding/pem"
	"testing"

	"golang.org/x/sys/windows"
)

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
