//go:build windows

package settingservice

import (
	"bytes"
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsCACertificateTrustStore struct{}

func newCACertificateTrustStore() caCertificateTrustStore {
	return windowsCACertificateTrustStore{}
}

func (windowsCACertificateTrustStore) supported() bool { return true }

func openCurrentUserRootStore(provider uintptr, flags uint32) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString("ROOT")
	if err != nil {
		return 0, err
	}
	store, err := windows.CertOpenStore(provider, 0, 0, windows.CERT_SYSTEM_STORE_CURRENT_USER|flags, uintptr(unsafe.Pointer(name)))
	runtime.KeepAlive(name)
	return store, err
}

func missingWindowsCertificate(err error) bool {
	return errors.Is(err, syscall.Errno(windows.CRYPT_E_NOT_FOUND)) ||
		errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND)
}

func normalizeWindowsCATrustError(err error) error {
	// Match the native result code, independent of Windows' display language.
	if errors.Is(err, windows.ERROR_CANCELLED) {
		return fmt.Errorf("%w: %w", errCATrustCanceled, err)
	}
	return err
}

func (windowsCACertificateTrustStore) status(der []byte) (caTrustState, error) {
	// SYSTEM_REGISTRY opens only the user's physical Root store. The logical
	// SYSTEM store also enumerates machine/group-policy roots, which this
	// feature must neither report as installed by the user nor remove.
	store, err := openCurrentUserRootStore(windows.CERT_STORE_PROV_SYSTEM_REGISTRY, windows.CERT_STORE_READONLY_FLAG|windows.CERT_STORE_OPEN_EXISTING_FLAG)
	if missingWindowsCertificate(err) {
		return caTrustState{}, nil
	}
	if err != nil {
		return caTrustState{}, fmt.Errorf("query current-user Root store: %w", err)
	}
	defer windows.CertCloseStore(store, 0)
	present, err := windowsStoreContainsCertificate(store, der)
	return caTrustState{present: present, installed: present}, err
}

func (windowsCACertificateTrustStore) install(der []byte) error {
	// Use the standard Root provider for insertion, preserving Windows root
	// protection and any native security confirmation. Do not bypass it with
	// CERT_SYSTEM_STORE_UNPROTECTED_FLAG or direct registry writes.
	store, err := openCurrentUserRootStore(windows.CERT_STORE_PROV_SYSTEM, 0)
	if err != nil {
		return fmt.Errorf("open current-user Root store for installation: %w", err)
	}
	defer windows.CertCloseStore(store, 0)
	return addWindowsCertificate(store, der)
}

func (windowsCACertificateTrustStore) uninstall(der []byte) error {
	store, err := openCurrentUserRootStore(windows.CERT_STORE_PROV_SYSTEM_REGISTRY, windows.CERT_STORE_OPEN_EXISTING_FLAG)
	if missingWindowsCertificate(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open current-user Root store for removal: %w", err)
	}
	defer windows.CertCloseStore(store, 0)
	return deleteWindowsCertificates(store, der)
}

// findWindowsCertificate returns an owned context; enumeration frees each
// previous context. Match the complete DER, never a subject or issuer name.
func findWindowsCertificate(store windows.Handle, der []byte) (*windows.CertContext, error) {
	var previous *windows.CertContext
	for {
		cert, err := windows.CertEnumCertificatesInStore(store, previous)
		if errors.Is(err, syscall.Errno(windows.CRYPT_E_NOT_FOUND)) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if bytes.Equal(unsafe.Slice(cert.EncodedCert, int(cert.Length)), der) {
			return cert, nil
		}
		previous = cert
	}
}

func windowsStoreContainsCertificate(store windows.Handle, der []byte) (bool, error) {
	cert, err := findWindowsCertificate(store, der)
	if err != nil || cert == nil {
		return false, err
	}
	defer windows.CertFreeCertificateContext(cert)
	return true, nil
}

func addWindowsCertificate(store windows.Handle, der []byte) error {
	if len(der) == 0 {
		return errCATrustInvalidCA
	}
	cert, err := windows.CertCreateCertificateContext(windows.X509_ASN_ENCODING, &der[0], uint32(len(der)))
	if err != nil {
		return err
	}
	defer windows.CertFreeCertificateContext(cert)
	// The service checks the physical user store first. ALWAYS ensures a
	// machine certificate inherited by the logical store cannot suppress
	// insertion into the user's store or replace a different certificate.
	return normalizeWindowsCATrustError(windows.CertAddCertificateContextToStore(store, cert, windows.CERT_STORE_ADD_ALWAYS, nil))
}

func deleteWindowsCertificates(store windows.Handle, der []byte) error {
	for {
		cert, err := findWindowsCertificate(store, der)
		if err != nil || cert == nil {
			return err
		}
		// This API frees the context even on failure; do not free it again.
		if err := windows.CertDeleteCertificateFromStore(cert); err != nil {
			return normalizeWindowsCATrustError(err)
		}
	}
}
