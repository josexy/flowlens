//go:build !windows

package settingservice

type unsupportedCACertificateTrustStore struct{}

func newCACertificateTrustStore() caCertificateTrustStore {
	return unsupportedCACertificateTrustStore{}
}

func (unsupportedCACertificateTrustStore) supported() bool { return false }
func (unsupportedCACertificateTrustStore) contains([]byte) (bool, error) {
	return false, errCATrustUnsupported
}
func (unsupportedCACertificateTrustStore) install([]byte) error   { return errCATrustUnsupported }
func (unsupportedCACertificateTrustStore) uninstall([]byte) error { return errCATrustUnsupported }
