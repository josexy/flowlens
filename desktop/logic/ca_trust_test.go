package logic

import (
	"testing"

	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
)

func caInput(mutate ...func(*CATrustInput)) CATrustInput {
	in := CATrustInput{
		UseBuiltinCA:     true,
		Info:             &settingservice.CACertificateInfo{CertExists: true, KeyExists: true, ValidPair: true, IsCA: true},
		Trust:            &settingservice.CACertificateTrustStatus{Supported: true, SHA256Fingerprint: "fingerprint"},
		HasExistingFiles: true,
	}
	for _, m := range mutate {
		m(&in)
	}
	return in
}

func TestTrustActionSwitchesBetweenInstallAndRemove(t *testing.T) {
	in := caInput()
	state := DeriveCATrustState(in)
	if state.Action != CATrustInstall || state.ActionDisabled {
		t.Fatalf("not installed: %+v", state)
	}
	in.Trust.Installed = true
	state = DeriveCATrustState(in)
	if state.Action != CATrustUninstall || state.Summary != CATrustInstalled || !state.GenerationDisabled || state.GenerationHint != CAHintUninstallFirst {
		t.Fatalf("installed: %+v", state)
	}
}

func TestMissingKeyPreventsInstallButAllowsRemoval(t *testing.T) {
	in := caInput(func(in *CATrustInput) {
		in.Info = &settingservice.CACertificateInfo{CertExists: true, Error: "missing key"}
	})
	if !DeriveCATrustState(in).ActionDisabled {
		t.Fatal("install should be disabled")
	}
	in.Trust.Installed = true
	if DeriveCATrustState(in).ActionDisabled {
		t.Fatal("removal should stay available")
	}
}

func TestIncompleteInstallationIsRemovableAndBlocksRegeneration(t *testing.T) {
	state := DeriveCATrustState(caInput(func(in *CATrustInput) {
		in.Info = &settingservice.CACertificateInfo{CertExists: true, Error: "missing key"}
		in.Trust.Present = true
	}))
	if state.Action != CATrustUninstall || state.ActionDisabled || state.Summary != CATrustPartial || state.SummaryTone != CAToneWarning || !state.GenerationDisabled {
		t.Fatalf("state = %+v", state)
	}
}

func TestUnknownTrustIsNeverNotInstalled(t *testing.T) {
	for _, mutate := range []func(*CATrustInput){
		func(in *CATrustInput) { in.TrustLoadFailed = true },
		func(in *CATrustInput) { in.Trust.Error = "denied" },
	} {
		state := DeriveCATrustState(caInput(mutate))
		if state.Summary != CATrustUnavailable || !state.ActionDisabled || !state.GenerationDisabled {
			t.Fatalf("state = %+v", state)
		}
	}
	state := DeriveCATrustState(caInput(func(in *CATrustInput) { in.TrustLoading = true }))
	if state.Summary != CATrustLoading || !state.ActionDisabled {
		t.Fatalf("loading = %+v", state)
	}
}

func TestPendingWorkPreventsRepeatedTrustActions(t *testing.T) {
	for _, mutate := range []func(*CATrustInput){
		func(in *CATrustInput) { in.Generating = true },
		func(in *CATrustInput) { in.ChangingTrust = true },
		func(in *CATrustInput) { in.SettingsDirty = true },
	} {
		if !DeriveCATrustState(caInput(mutate)).ActionDisabled {
			t.Fatal("action should be disabled")
		}
	}
	if !DeriveCATrustState(caInput(func(in *CATrustInput) { in.ChangingTrust = true })).GenerationDisabled {
		t.Fatal("generation should be disabled")
	}
}

func TestMalformedCertificateCanBeRegenerated(t *testing.T) {
	state := DeriveCATrustState(caInput(func(in *CATrustInput) {
		in.Info = &settingservice.CACertificateInfo{CertExists: true, KeyExists: true, Error: "malformed"}
		in.Trust.SHA256Fingerprint, in.Trust.Error = "", "malformed"
	}))
	if !state.ActionDisabled || state.GenerationDisabled {
		t.Fatalf("state = %+v", state)
	}
}

func TestUnsupportedPlatformsRetainGeneration(t *testing.T) {
	state := DeriveCATrustState(caInput(func(in *CATrustInput) {
		in.Trust = &settingservice.CACertificateTrustStatus{Supported: false}
	}))
	if state.GenerationDisabled {
		t.Fatalf("state = %+v", state)
	}
}

func TestCustomModeAndPendingSettingsBlockGeneration(t *testing.T) {
	in := caInput(func(in *CATrustInput) { in.UseBuiltinCA = false })
	state := DeriveCATrustState(in)
	if !state.GenerationDisabled || state.GenerationHint != CAHintCustomDisabled || state.ActionDisabled {
		t.Fatalf("custom: %+v", state)
	}
	in.UseBuiltinCA, in.SettingsDirty = true, true
	state = DeriveCATrustState(in)
	if !state.GenerationDisabled || state.GenerationHint != CAHintSaveFirst {
		t.Fatalf("dirty: %+v", state)
	}
}

func TestMissingCertificateStatusDistinguishesBuiltinFromCustom(t *testing.T) {
	in := caInput(func(in *CATrustInput) { in.Info = &settingservice.CACertificateInfo{} })
	if got := DeriveCATrustState(in).Status; got != CAStatusBuiltinUnavailable {
		t.Fatalf("builtin: %v", got)
	}
	in.UseBuiltinCA = false
	if got := DeriveCATrustState(in).Status; got != CAStatusFilesMissing {
		t.Fatalf("custom: %v", got)
	}
}

func TestGenerationHintNamesTheAvailableAction(t *testing.T) {
	if got := DeriveCATrustState(caInput()).GenerationHint; got != CAHintRegenerate {
		t.Fatalf("existing files: %v", got)
	}
	in := caInput(func(in *CATrustInput) { in.HasExistingFiles = false })
	if got := DeriveCATrustState(in).GenerationHint; got != CAHintGenerate {
		t.Fatalf("no files: %v", got)
	}
}

func TestNormalizeHiddenTrafficColumns(t *testing.T) {
	got := NormalizeHiddenTrafficColumns([]string{" size ", "nope", "host", "host"})
	if len(got) != 2 || got[0] != "host" || got[1] != "size" {
		t.Fatalf("got %v", got)
	}
	all := make([]string, len(TrafficColumnKeys))
	for i, key := range TrafficColumnKeys {
		all[i] = string(key)
	}
	got = NormalizeHiddenTrafficColumns(all)
	if len(got) != len(all)-1 || got[0] == "id" {
		t.Fatalf("every column hidden: %v", got)
	}
}
