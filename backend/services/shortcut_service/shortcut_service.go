package shortcutservice

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
)

const (
	CommandShowMainWindow = "app.showMainWindow"
	CommandToggleProxy    = "capture.toggleProxy"
)

type ShortcutRuntimeStatus string

const (
	ShortcutStatusActive        ShortcutRuntimeStatus = "active"
	ShortcutStatusConflict      ShortcutRuntimeStatus = "conflict"
	ShortcutStatusUnsupported   ShortcutRuntimeStatus = "unsupported"
	ShortcutStatusPortalPending ShortcutRuntimeStatus = "portalPending"
)

const (
	WarningWaylandPortalControlsBinding = "waylandPortalControlsBinding"
	WarningMacNonANSICompatibility      = "macNonANSICompatibility"
)

type ShortcutCommandRuntimeState struct {
	Status      ShortcutRuntimeStatus `json:"status"`
	Accelerator string                `json:"accelerator,omitempty"`
}

type ShortcutRuntimeState struct {
	Commands map[string]ShortcutCommandRuntimeState `json:"commands"`
	Warnings []string                               `json:"warnings"`
}

type ShortcutApplyResult struct {
	Applied bool `json:"applied"`
	// Config is always the effective configuration after the attempt. On a
	// rejected or rolled-back rebind it is the restored previous configuration.
	Config       *settingservice.ShortcutConfig `json:"config"`
	RuntimeState ShortcutRuntimeState           `json:"runtimeState"`
	Warnings     []string                       `json:"warnings"`
	ErrorCode    string                         `json:"errorCode,omitempty"`
	ErrorMessage string                         `json:"errorMessage,omitempty"`
}

const (
	ApplyErrorInvalidConfig      = "invalidConfig"
	ApplyErrorUnsupported        = "unsupported"
	ApplyErrorRegistration       = "registrationConflict"
	ApplyErrorPersistence        = "persistenceFailed"
	ApplyErrorUnregister         = "unregisterFailed"
	ApplyErrorRollbackIncomplete = "rollbackIncomplete"
)

type Registrar interface {
	Register(accelerator string, callback func()) error
	Unregister(accelerator string) error
	IsRegistered(accelerator string) bool
}

type ConfigStore interface {
	GetShortcutConfig() (*settingservice.ShortcutConfig, error)
	SaveShortcutConfig(config *settingservice.ShortcutConfig) error
}

type Environment struct {
	OS      string
	Wayland bool
}

type ShortcutService struct {
	mu              sync.Mutex
	registrar       Registrar
	store           ConfigStore
	invoke          func(commandID string)
	notifyChanged   func(result ShortcutApplyResult)
	environment     Environment
	mapping         atomic.Value
	effectiveConfig *settingservice.ShortcutConfig
	runtimeState    ShortcutRuntimeState
}

func New(registrar Registrar, store ConfigStore, invoke func(commandID string)) *ShortcutService {
	return newForEnvironment(registrar, store, invoke, nil, detectEnvironment())
}

func NewForEnvironment(registrar Registrar, store ConfigStore, invoke func(commandID string), environment Environment) *ShortcutService {
	return newForEnvironment(registrar, store, invoke, nil, environment)
}

func NewWithNotifier(registrar Registrar, store ConfigStore, invoke func(commandID string), notifier func(result ShortcutApplyResult)) *ShortcutService {
	return newForEnvironment(registrar, store, invoke, notifier, detectEnvironment())
}

func NewForEnvironmentWithNotifier(registrar Registrar, store ConfigStore, invoke func(commandID string), notifier func(result ShortcutApplyResult), environment Environment) *ShortcutService {
	return newForEnvironment(registrar, store, invoke, notifier, environment)
}

func newForEnvironment(registrar Registrar, store ConfigStore, invoke func(commandID string), notifier func(result ShortcutApplyResult), environment Environment) *ShortcutService {
	if environment.OS == "" {
		environment.OS = runtime.GOOS
	}
	service := &ShortcutService{
		registrar:       registrar,
		store:           store,
		invoke:          invoke,
		notifyChanged:   notifier,
		environment:     environment,
		effectiveConfig: settingservice.NormalizeShortcutConfig(nil),
		runtimeState:    emptyRuntimeState(),
	}
	service.mapping.Store(map[string]string{})
	return service
}

func detectEnvironment() Environment {
	sessionType := strings.TrimSpace(strings.ToLower(os.Getenv("XDG_SESSION_TYPE")))
	return Environment{
		OS:      runtime.GOOS,
		Wayland: runtime.GOOS == "linux" && (sessionType == "wayland" || os.Getenv("WAYLAND_DISPLAY") != ""),
	}
}

// Start applies the currently persisted configuration. Main should call this
// after the main window reports frontend-ready so callbacks cannot race the
// frontend shortcut host during application startup.
//
//wails:ignore
func (s *ShortcutService) Start() (*ShortcutApplyResult, error) {
	if s.store == nil {
		return s.failureResult(ApplyErrorPersistence, errors.New("shortcut config store is not available"), nil), nil
	}
	config, err := s.store.GetShortcutConfig()
	if err != nil {
		loadErr := fmt.Errorf("load shortcut config: %w", err)
		return s.failureResult(ApplyErrorPersistence, loadErr, nil), loadErr
	}

	// The database configuration is the rollback baseline for the initial OS
	// registration attempt. It remains effective even when an OS conflict keeps
	// a requested accelerator inactive.
	s.mu.Lock()
	s.effectiveConfig = settingservice.NormalizeShortcutConfig(config)
	s.mu.Unlock()
	result, applyErr := s.applyShortcutConfig(config, false)
	if result != nil && !result.Applied {
		s.mu.Lock()
		s.runtimeState = cloneRuntimeState(result.RuntimeState)
		s.mu.Unlock()
	}
	s.notifyChange(result, true)
	return result, applyErr
}

func (s *ShortcutService) ApplyShortcutConfig(config *settingservice.ShortcutConfig) (*ShortcutApplyResult, error) {
	result, applyErr := s.applyShortcutConfig(config, true)
	s.notifyChange(result, false)
	return result, applyErr
}

func (s *ShortcutService) notifyChange(result *ShortcutApplyResult, includeFailure bool) {
	if result == nil || !result.Applied && !includeFailure {
		return
	}
	s.mu.Lock()
	notifier := s.notifyChanged
	s.mu.Unlock()
	if notifier != nil {
		notifier(*result)
	}
}

func (s *ShortcutService) applyShortcutConfig(config *settingservice.ShortcutConfig, persist bool) (*ShortcutApplyResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if config == nil {
		return s.failureResultLocked(ApplyErrorInvalidConfig, errors.New("shortcut config cannot be nil"), nil), nil
	}
	if s.store == nil {
		return s.failureResultLocked(ApplyErrorPersistence, errors.New("shortcut config store is not available"), nil), nil
	}

	normalized := settingservice.NormalizeShortcutConfig(config)
	candidateMapping, candidateState, validationErr := s.resolveConfig(config, normalized)
	if validationErr != nil {
		code := ApplyErrorUnsupported
		for _, commandState := range candidateState.Commands {
			if commandState.Status == ShortcutStatusConflict {
				code = ApplyErrorRegistration
				break
			}
		}
		return s.failureResultLocked(code, validationErr, &candidateState), nil
	}

	oldConfig := settingservice.NormalizeShortcutConfig(s.effectiveConfig)
	oldMapping := cloneMapping(s.currentMapping())
	additions, removals := mappingSetChanges(oldMapping, candidateMapping)

	registeredAdditions := make([]string, 0, len(additions))
	for _, accelerator := range additions {
		if s.registrar == nil {
			failureState := markAcceleratorStatus(s.runtimeState, candidateMapping, accelerator, ShortcutStatusUnsupported)
			return s.failureResultLocked(ApplyErrorUnsupported, errors.New("global shortcut registrar is not available"), &failureState), nil
		}
		if err := s.registrar.Register(accelerator, s.callbackFor(accelerator)); err != nil {
			failureState := markAcceleratorStatus(s.runtimeState, candidateMapping, accelerator, ShortcutStatusConflict)
			rollbackErr := s.unregisterOwned(registeredAdditions)
			applyErr := errors.Join(
				fmt.Errorf("register global shortcut %q: %w", accelerator, err),
				rollbackErr,
			)
			code := ApplyErrorRegistration
			if rollbackErr != nil {
				code = ApplyErrorRollbackIncomplete
			}
			return s.failureResultLocked(code, applyErr, &failureState), nil
		}
		registeredAdditions = append(registeredAdditions, accelerator)
	}

	if persist {
		if err := s.store.SaveShortcutConfig(normalized); err != nil {
			rollbackErr := s.unregisterOwned(registeredAdditions)
			applyErr := errors.Join(fmt.Errorf("persist shortcut config: %w", err), rollbackErr)
			code := ApplyErrorPersistence
			if rollbackErr != nil {
				code = ApplyErrorRollbackIncomplete
			}
			return s.failureResultLocked(code, applyErr, nil), nil
		}
	}

	s.mapping.Store(cloneMapping(candidateMapping))
	unregisteredRemovals := make([]string, 0, len(removals))
	for _, accelerator := range removals {
		if s.registrar == nil {
			continue
		}
		if err := s.registrar.Unregister(accelerator); err != nil {
			rollbackErr := s.rollbackAfterUnregisterFailure(oldConfig, oldMapping, registeredAdditions, append(unregisteredRemovals, accelerator), persist)
			applyErr := errors.Join(
				fmt.Errorf("unregister old global shortcut %q: %w", accelerator, err),
				rollbackErr,
			)
			code := ApplyErrorUnregister
			if rollbackErr != nil {
				code = ApplyErrorRollbackIncomplete
			}
			return s.failureResultLocked(code, applyErr, nil), nil
		}
		unregisteredRemovals = append(unregisteredRemovals, accelerator)
	}

	s.effectiveConfig = settingservice.NormalizeShortcutConfig(normalized)
	s.runtimeState = cloneRuntimeState(candidateState)
	result := s.successResultLocked()
	return &result, nil
}

func (s *ShortcutService) GetShortcutRuntimeState() ShortcutRuntimeState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneRuntimeState(s.runtimeState)
}

//wails:ignore
func (s *ShortcutService) Shutdown() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	accelerators := sortedMappingKeys(s.currentMapping())
	s.mapping.Store(map[string]string{})
	s.runtimeState = emptyRuntimeState()
	var shutdownErr error
	if s.registrar != nil {
		for _, accelerator := range accelerators {
			if !s.registrar.IsRegistered(accelerator) {
				continue
			}
			if err := s.registrar.Unregister(accelerator); err != nil {
				shutdownErr = errors.Join(shutdownErr, fmt.Errorf("unregister global shortcut %q during shutdown: %w", accelerator, err))
			}
		}
	}
	return shutdownErr
}

func (s *ShortcutService) ServiceShutdown() error {
	return s.Shutdown()
}

func (s *ShortcutService) callbackFor(accelerator string) func() {
	return func() {
		commandID := s.currentMapping()[accelerator]
		if commandID != "" && s.invoke != nil {
			s.invoke(commandID)
		}
	}
}

func (s *ShortcutService) currentMapping() map[string]string {
	mapping, _ := s.mapping.Load().(map[string]string)
	return mapping
}

func (s *ShortcutService) resolveConfig(raw, normalized *settingservice.ShortcutConfig) (map[string]string, ShortcutRuntimeState, error) {
	state := emptyRuntimeState()
	for commandID, override := range raw.Overrides {
		if strings.EqualFold(strings.TrimSpace(string(override.Scope)), string(settingservice.ShortcutScopeGlobal)) && !isGlobalCommandAllowed(commandID) {
			state.Commands[commandID] = ShortcutCommandRuntimeState{Status: ShortcutStatusUnsupported}
			return nil, state, fmt.Errorf("command %q cannot use global shortcut scope", commandID)
		}
	}

	mapping := make(map[string]string)
	commandIDs := make([]string, 0, len(normalized.Overrides))
	for commandID := range normalized.Overrides {
		commandIDs = append(commandIDs, commandID)
	}
	sort.Strings(commandIDs)
	for _, commandID := range commandIDs {
		override := normalized.Overrides[commandID]
		if override.Scope != settingservice.ShortcutScopeGlobal || override.Binding == nil {
			continue
		}
		if !isGlobalCommandAllowed(commandID) {
			state.Commands[commandID] = ShortcutCommandRuntimeState{Status: ShortcutStatusUnsupported}
			return nil, state, fmt.Errorf("command %q cannot use global shortcut scope", commandID)
		}
		accelerator, err := resolveAccelerator(*override.Binding, s.environment.OS)
		if err != nil {
			state.Commands[commandID] = ShortcutCommandRuntimeState{Status: ShortcutStatusUnsupported}
			return nil, state, fmt.Errorf("invalid global shortcut for %q: %w", commandID, err)
		}
		if conflictCommand, exists := mapping[accelerator]; exists {
			state.Commands[commandID] = ShortcutCommandRuntimeState{Status: ShortcutStatusConflict, Accelerator: accelerator}
			state.Commands[conflictCommand] = ShortcutCommandRuntimeState{Status: ShortcutStatusConflict, Accelerator: accelerator}
			return nil, state, fmt.Errorf("global shortcut %q is assigned to both %q and %q", accelerator, conflictCommand, commandID)
		}
		mapping[accelerator] = commandID
		status := ShortcutStatusActive
		if !isSupportedOS(s.environment.OS) {
			status = ShortcutStatusUnsupported
		} else if s.environment.OS == "linux" && s.environment.Wayland {
			status = ShortcutStatusPortalPending
		}
		state.Commands[commandID] = ShortcutCommandRuntimeState{Status: status, Accelerator: accelerator}
	}

	if !isSupportedOS(s.environment.OS) {
		mapping = make(map[string]string)
	}
	if len(state.Commands) > 0 && s.environment.OS == "linux" && s.environment.Wayland {
		state.Warnings = append(state.Warnings, WarningWaylandPortalControlsBinding)
	}
	if len(state.Commands) > 0 && s.environment.OS == "darwin" {
		state.Warnings = append(state.Warnings, WarningMacNonANSICompatibility)
	}
	return mapping, state, nil
}

func (s *ShortcutService) rollbackAfterUnregisterFailure(oldConfig *settingservice.ShortcutConfig, oldMapping map[string]string, additions, removed []string, restorePersistence bool) error {
	s.mapping.Store(cloneMapping(oldMapping))
	var rollbackErr error
	if s.registrar != nil {
		for _, accelerator := range removed {
			if s.registrar.IsRegistered(accelerator) {
				continue
			}
			if err := s.registrar.Register(accelerator, s.callbackFor(accelerator)); err != nil {
				rollbackErr = errors.Join(rollbackErr, fmt.Errorf("restore old global shortcut %q: %w", accelerator, err))
			}
		}
		rollbackErr = errors.Join(rollbackErr, s.unregisterOwned(additions))
	}
	if restorePersistence {
		if err := s.store.SaveShortcutConfig(oldConfig); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("restore persisted shortcut config: %w", err))
		}
	}
	return rollbackErr
}

func (s *ShortcutService) unregisterOwned(accelerators []string) error {
	if s.registrar == nil {
		return nil
	}
	var rollbackErr error
	for _, accelerator := range slices.Backward(accelerators) {

		if !s.registrar.IsRegistered(accelerator) {
			continue
		}
		if err := s.registrar.Unregister(accelerator); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("roll back global shortcut %q: %w", accelerator, err))
		}
	}
	return rollbackErr
}

func (s *ShortcutService) successResultLocked() ShortcutApplyResult {
	state := cloneRuntimeState(s.runtimeState)
	return ShortcutApplyResult{
		Applied:      true,
		Config:       settingservice.NormalizeShortcutConfig(s.effectiveConfig),
		RuntimeState: state,
		Warnings:     append([]string(nil), state.Warnings...),
	}
}

func (s *ShortcutService) failureResult(code string, applyErr error, state *ShortcutRuntimeState) *ShortcutApplyResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failureResultLocked(code, applyErr, state)
}

func (s *ShortcutService) failureResultLocked(code string, applyErr error, overrideState *ShortcutRuntimeState) *ShortcutApplyResult {
	state := s.runtimeState
	if overrideState != nil {
		state = *overrideState
	}
	clonedState := cloneRuntimeState(state)
	result := &ShortcutApplyResult{
		Applied:      false,
		Config:       settingservice.NormalizeShortcutConfig(s.effectiveConfig),
		RuntimeState: clonedState,
		Warnings:     append([]string(nil), clonedState.Warnings...),
		ErrorCode:    code,
	}
	if applyErr != nil {
		result.ErrorMessage = applyErr.Error()
	}
	return result
}

func emptyRuntimeState() ShortcutRuntimeState {
	return ShortcutRuntimeState{Commands: make(map[string]ShortcutCommandRuntimeState)}
}

func cloneRuntimeState(state ShortcutRuntimeState) ShortcutRuntimeState {
	clone := emptyRuntimeState()
	maps.Copy(clone.Commands, state.Commands)
	clone.Warnings = append([]string(nil), state.Warnings...)
	return clone
}

func cloneMapping(mapping map[string]string) map[string]string {
	clone := make(map[string]string, len(mapping))
	maps.Copy(clone, mapping)
	return clone
}

func mappingSetChanges(oldMapping, newMapping map[string]string) (additions, removals []string) {
	for accelerator := range newMapping {
		if _, exists := oldMapping[accelerator]; !exists {
			additions = append(additions, accelerator)
		}
	}
	for accelerator := range oldMapping {
		if _, exists := newMapping[accelerator]; !exists {
			removals = append(removals, accelerator)
		}
	}
	sort.Strings(additions)
	sort.Strings(removals)
	return additions, removals
}

func sortedMappingKeys(mapping map[string]string) []string {
	keys := make([]string, 0, len(mapping))
	for accelerator := range mapping {
		keys = append(keys, accelerator)
	}
	sort.Strings(keys)
	return keys
}

func markAcceleratorStatus(state ShortcutRuntimeState, mapping map[string]string, accelerator string, status ShortcutRuntimeStatus) ShortcutRuntimeState {
	state = cloneRuntimeState(state)
	if commandID := mapping[accelerator]; commandID != "" {
		state.Commands[commandID] = ShortcutCommandRuntimeState{Status: status, Accelerator: accelerator}
	}
	return state
}

func isGlobalCommandAllowed(commandID string) bool {
	return commandID == CommandShowMainWindow || commandID == CommandToggleProxy
}

func isSupportedOS(osName string) bool {
	return osName == "windows" || osName == "darwin" || osName == "linux"
}
