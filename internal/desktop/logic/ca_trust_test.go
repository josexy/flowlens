package logic

import (
	"testing"

	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
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

func TestTrustButtonHintPrefersLoadingThenPendingSettingsThenUnknownStatus(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*CATrustInput)
		want   CAActionHint
	}{
		{"ready", func(*CATrustInput) {}, CAActionHintAction},
		{"loading", func(in *CATrustInput) { in.TrustLoading, in.SettingsDirty = true, true }, CAActionHintLoading},
		{"dirty", func(in *CATrustInput) { in.SettingsDirty, in.TrustLoadFailed = true, true }, CAActionHintSaveFirst},
		{"failed", func(in *CATrustInput) { in.TrustLoadFailed = true }, CAActionHintUnavailable},
		{"error", func(in *CATrustInput) { in.Trust.Error = "denied" }, CAActionHintUnavailable},
		{"generating", func(in *CATrustInput) { in.Generating = true }, CAActionHintAction},
	} {
		if got := DeriveCATrustState(caInput(test.mutate)).ActionHint; got != test.want {
			t.Errorf("%s: hint = %v, want %v", test.name, got, test.want)
		}
	}
}

func TestPanelHintAsksToSaveOrUninstallFirst(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*CATrustInput)
		want   CAPanelHint
	}{
		{"saved", func(*CATrustInput) {}, CAPanelHintNone},
		{"dirty builtin", func(in *CATrustInput) { in.SettingsDirty = true }, CAPanelHintSaveFirst},
		{"dirty custom with trust", func(in *CATrustInput) { in.SettingsDirty, in.UseBuiltinCA = true, false }, CAPanelHintSaveFirst},
		{"dirty custom without trust", func(in *CATrustInput) {
			in.SettingsDirty, in.UseBuiltinCA = true, false
			in.Trust.Supported = false
		}, CAPanelHintNone},
		{"dirty and installed", func(in *CATrustInput) { in.SettingsDirty, in.Trust.Installed = true, true }, CAPanelHintSaveFirst},
		{"installed", func(in *CATrustInput) { in.Trust.Installed = true }, CAPanelHintUninstallFirst},
		{"partial", func(in *CATrustInput) { in.Trust.Present = true }, CAPanelHintUninstallFirst},
		{"installed custom", func(in *CATrustInput) { in.Trust.Installed, in.UseBuiltinCA = true, false }, CAPanelHintNone},
		{"installed but loading", func(in *CATrustInput) { in.Trust.Installed, in.TrustLoading = true, true }, CAPanelHintNone},
		{"installed but unreadable", func(in *CATrustInput) { in.Trust.Installed, in.Trust.Error = true, "denied" }, CAPanelHintNone},
		{"installed unsupported", func(in *CATrustInput) { in.Trust.Installed, in.Trust.Supported = true, false }, CAPanelHintNone},
	} {
		if got := DeriveCATrustState(caInput(test.mutate)).PanelHint; got != test.want {
			t.Errorf("%s: hint = %v, want %v", test.name, got, test.want)
		}
	}
}

func TestTrustControlsFollowPlatformSupport(t *testing.T) {
	state := DeriveCATrustState(caInput())
	if !state.TrustActions || !state.ShowSummary {
		t.Fatalf("supported: %+v", state)
	}
	unsupported := func(in *CATrustInput) { in.Trust.Supported = false }
	state = DeriveCATrustState(caInput(unsupported))
	if state.TrustActions || state.ShowSummary {
		t.Fatalf("unsupported: %+v", state)
	}
	// A failed status read still reports why trust is unknown.
	state = DeriveCATrustState(caInput(unsupported, func(in *CATrustInput) { in.TrustLoadFailed = true }))
	if state.TrustActions || !state.ShowSummary || state.Summary != CATrustUnavailable {
		t.Fatalf("failed: %+v", state)
	}
	state = DeriveCATrustState(caInput(func(in *CATrustInput) { in.Trust, in.TrustLoadFailed = nil, true }))
	if state.TrustActions || !state.ShowSummary {
		t.Fatalf("never read: %+v", state)
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
