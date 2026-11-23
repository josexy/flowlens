package logic

import settingservice "github.com/josexy/flowlens/internal/services/setting_service"

// CATrustInput is what decides the certificate panel's actions.
type CATrustInput struct {
	UseBuiltinCA    bool
	Info            *settingservice.CACertificateInfo
	Trust           *settingservice.CACertificateTrustStatus
	TrustLoading    bool
	TrustLoadFailed bool
	ChangingTrust   bool
	// SettingsDirty is set while the draft's certificate configuration
	// differs from the saved one, which the actions apply to.
	SettingsDirty    bool
	HasExistingFiles bool
	Generating       bool
}

// CATrustAction is what the trust button does.
type CATrustAction int

// Trust button actions.
const (
	CATrustInstall CATrustAction = iota
	CATrustUninstall
)

// CATrustSummary describes the trust state.
type CATrustSummary int

// Trust states.
const (
	CATrustLoading CATrustSummary = iota
	CATrustUnavailable
	CATrustPartial
	CATrustInstalled
	CATrustNotInstalled
)

// CATone is the emphasis of a status line.
type CATone int

// Tones.
const (
	CAToneNeutral CATone = iota
	CAToneSuccess
	CAToneWarning
)

// CAActionHint explains the trust button's state.
type CAActionHint int

// Trust button hints.
const (
	CAActionHintAction CAActionHint = iota // names the action
	CAActionHintLoading
	CAActionHintSaveFirst
	CAActionHintUnavailable
)

// CAPanelHint is the note the certificate panel shows under its status.
type CAPanelHint int

// Panel hints.
const (
	CAPanelHintNone CAPanelHint = iota
	CAPanelHintSaveFirst
	CAPanelHintUninstallFirst
)

// CAGenerationHint explains the generate button's state.
type CAGenerationHint int

// Generation hints.
const (
	CAHintCustomDisabled CAGenerationHint = iota
	CAHintSaveFirst
	CAHintLoading
	CAHintUninstallFirst
	CAHintStatusUnavailable
	CAHintRegenerate
	CAHintGenerate
)

// CAStatus summarizes the certificate files.
type CAStatus int

// Certificate states.
const (
	CAStatusUnavailable CAStatus = iota
	CAStatusReady
	CAStatusBuiltinUnavailable
	CAStatusFilesMissing
	CAStatusNotCA
	CAStatusIncomplete
)

// CATrustState is the certificate panel's derived state.
type CATrustState struct {
	// TrustActions is set when the platform offers the trust button.
	TrustActions bool
	// ShowSummary is set when the panel shows the trust summary.
	ShowSummary        bool
	Action             CATrustAction
	ActionDisabled     bool
	ActionHint         CAActionHint
	GenerationDisabled bool
	GenerationHint     CAGenerationHint
	Summary            CATrustSummary
	SummaryTone        CATone
	Status             CAStatus
	PanelHint          CAPanelHint
}

// DeriveCATrustState computes the certificate panel's state.
func DeriveCATrustState(in CATrustInput) CATrustState {
	info, trust := in.Info, in.Trust
	ready := info != nil && info.CertExists && info.KeyExists && info.ValidPair && info.IsCA && info.Error == ""
	unknown := in.TrustLoading || in.TrustLoadFailed || (trust != nil && trust.Error != "")
	hasEntry := trust != nil && (trust.Present || trust.Installed)
	installed := trust != nil && trust.Installed
	fingerprint := trust != nil && trust.SHA256Fingerprint != ""
	supported := trust != nil && trust.Supported

	var state CATrustState
	state.TrustActions = supported
	state.ShowSummary = supported || in.TrustLoadFailed
	if hasEntry {
		state.Action = CATrustUninstall
	}
	state.ActionDisabled = unknown || in.Generating || in.ChangingTrust || in.SettingsDirty ||
		!fingerprint || (!hasEntry && !ready)
	switch {
	case in.TrustLoading:
		state.ActionHint = CAActionHintLoading
	case in.SettingsDirty:
		state.ActionHint = CAActionHintSaveFirst
	case unknown:
		state.ActionHint = CAActionHintUnavailable
	}
	state.GenerationDisabled = !in.UseBuiltinCA || in.SettingsDirty || in.Generating ||
		in.TrustLoading || in.TrustLoadFailed || in.ChangingTrust ||
		(supported && (hasEntry || (trust.Error != "" && fingerprint)))

	switch {
	case !in.UseBuiltinCA:
		state.GenerationHint = CAHintCustomDisabled
	case in.SettingsDirty:
		state.GenerationHint = CAHintSaveFirst
	case in.TrustLoading:
		state.GenerationHint = CAHintLoading
	case supported && hasEntry:
		state.GenerationHint = CAHintUninstallFirst
	case state.GenerationDisabled && unknown:
		state.GenerationHint = CAHintStatusUnavailable
	case in.HasExistingFiles:
		state.GenerationHint = CAHintRegenerate
	default:
		state.GenerationHint = CAHintGenerate
	}

	switch {
	case in.TrustLoading:
		state.Summary, state.SummaryTone = CATrustLoading, CAToneNeutral
	case unknown:
		state.Summary, state.SummaryTone = CATrustUnavailable, CAToneWarning
	case hasEntry && !installed:
		state.Summary, state.SummaryTone = CATrustPartial, CAToneWarning
	case installed:
		state.Summary, state.SummaryTone = CATrustInstalled, CAToneSuccess
	default:
		state.Summary, state.SummaryTone = CATrustNotInstalled, CAToneNeutral
	}

	switch {
	case info == nil:
		state.Status = CAStatusUnavailable
	case ready:
		state.Status = CAStatusReady
	case in.UseBuiltinCA:
		state.Status = CAStatusBuiltinUnavailable
	case !info.CertExists || !info.KeyExists:
		state.Status = CAStatusFilesMissing
	case info.Error == "" && !info.IsCA:
		state.Status = CAStatusNotCA
	default:
		state.Status = CAStatusIncomplete
	}

	switch {
	case in.SettingsDirty && (in.UseBuiltinCA || supported):
		state.PanelHint = CAPanelHintSaveFirst
	case in.UseBuiltinCA && supported && hasEntry && !unknown:
		state.PanelHint = CAPanelHintUninstallFirst
	}
	return state
}
