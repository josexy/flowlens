//go:build !windows && !darwin

package settingservice

type unsupportedCACertificateTrustStore struct{}

func newCACertificateTrustStore() caCertificateTrustStore {
	return unsupportedCACertificateTrustStore{}
}

func (unsupportedCACertificateTrustStore) supported() bool { return false }
func (unsupportedCACertificateTrustStore) status([]byte) (caTrustState, error) {
	return caTrustState{}, errCATrustUnsupported
}
func (unsupportedCACertificateTrustStore) install([]byte) error   { return errCATrustUnsupported }
func (unsupportedCACertificateTrustStore) uninstall([]byte) error { return errCATrustUnsupported }
