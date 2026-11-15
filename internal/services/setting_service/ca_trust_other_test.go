//go:build !windows && !darwin

package settingservice

import "testing"

func TestUnsupportedPlatformCATrustUnavailable(t *testing.T) {
	s := &SettingService{}
	status, err := s.GetCurrentCACertificateTrustStatus()
	if err != nil || status.Supported {
		t.Fatalf("unsupported platform trust: %+v %v", status, err)
	}
}
