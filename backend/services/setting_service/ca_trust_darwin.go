//go:build darwin

package settingservice

func newCACertificateTrustStore() caCertificateTrustStore {
	return securityCACertificateTrustStore{run: runCACertificateSecurity}
}
