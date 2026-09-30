//go:build !windows

package settingservice

import "testing"

func TestNonWindowsCATrustUnavailable(t *testing.T) {
	s := &SettingService{}
	status, err := s.GetCurrentCACertificateTrustStatus()
	if err != nil || status.Supported {
		t.Fatalf("non-Windows trust: %+v %v", status, err)
	}
}
