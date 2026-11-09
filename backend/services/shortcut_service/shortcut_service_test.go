package shortcutservice

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
)

type fakeRegistrar struct {
	mu                       sync.Mutex
	registered               map[string]func()
	registerFailures         map[string]error
	unregisterFailures       map[string]error
	unregisterFailureRemoves bool
	registerCalls            []string
	unregisterCalls          []string
}

func newFakeRegistrar() *fakeRegistrar {
	return &fakeRegistrar{
		registered:         make(map[string]func()),
		registerFailures:   make(map[string]error),
		unregisterFailures: make(map[string]error),
	}
}

func (f *fakeRegistrar) Register(accelerator string, callback func()) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registerCalls = append(f.registerCalls, accelerator)
	if err := f.registerFailures[accelerator]; err != nil {
		delete(f.registerFailures, accelerator)
		return err
	}
	if _, exists := f.registered[accelerator]; exists {
		return errors.New("already registered")
	}
	f.registered[accelerator] = callback
	return nil
}

func (f *fakeRegistrar) Unregister(accelerator string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unregisterCalls = append(f.unregisterCalls, accelerator)
	if err := f.unregisterFailures[accelerator]; err != nil {
		delete(f.unregisterFailures, accelerator)
		if f.unregisterFailureRemoves {
			delete(f.registered, accelerator)
		}
		return err
	}
	if _, exists := f.registered[accelerator]; !exists {
		return errors.New("not registered")
	}
	delete(f.registered, accelerator)
	return nil
}

func (f *fakeRegistrar) IsRegistered(accelerator string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, exists := f.registered[accelerator]
	return exists
}

func (f *fakeRegistrar) trigger(accelerator string) bool {
	f.mu.Lock()
	callback := f.registered[accelerator]
	f.mu.Unlock()
	if callback == nil {
		return false
	}
	callback()
	return true
}

func (f *fakeRegistrar) registeredKeys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	keys := make([]string, 0, len(f.registered))
	for accelerator := range f.registered {
		keys = append(keys, accelerator)
	}
	return keys
}

type fakeConfigStore struct {
	mu        sync.Mutex
	config    *settingservice.ShortcutConfig
	getErr    error
	saveCalls int
	failSave  map[int]error
}

func newFakeConfigStore(config *settingservice.ShortcutConfig) *fakeConfigStore {
	return &fakeConfigStore{config: settingservice.NormalizeShortcutConfig(config), failSave: make(map[int]error)}
}

func (f *fakeConfigStore) GetShortcutConfig() (*settingservice.ShortcutConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return nil, f.getErr
	}
	return settingservice.NormalizeShortcutConfig(f.config), nil
}

func (f *fakeConfigStore) SaveShortcutConfig(config *settingservice.ShortcutConfig) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saveCalls++
	if err := f.failSave[f.saveCalls]; err != nil {
		return err
	}
	f.config = settingservice.NormalizeShortcutConfig(config)
	return nil
}

func globalConfig(commandID string, modifiers []settingservice.ShortcutModifier, key string) *settingservice.ShortcutConfig {
	return &settingservice.ShortcutConfig{Overrides: map[string]settingservice.ShortcutOverride{
		commandID: {
			Scope: settingservice.ShortcutScopeGlobal,
			Binding: &settingservice.ShortcutBinding{
				Modifiers: modifiers,
				Key:       key,
			},
		},
	}}
}

func TestApplyRejectsDisallowedInvalidAndDuplicateGlobals(t *testing.T) {
	tests := []struct {
		name   string
		config *settingservice.ShortcutConfig
		status ShortcutRuntimeStatus
	}{
		{
			name:   "unknown global command",
			config: globalConfig("unknown.command", []settingservice.ShortcutModifier{settingservice.ShortcutModifierControl}, "a"),
			status: ShortcutStatusUnsupported,
		},
		{
			name:   "modifier is required",
			config: globalConfig(CommandShowMainWindow, nil, "a"),
			status: ShortcutStatusUnsupported,
		},
		{
			name:   "non portable key",
			config: globalConfig(CommandShowMainWindow, []settingservice.ShortcutModifier{settingservice.ShortcutModifierControl}, ";"),
			status: ShortcutStatusUnsupported,
		},
		{
			name: "platform collapsed duplicate",
			config: &settingservice.ShortcutConfig{Overrides: map[string]settingservice.ShortcutOverride{
				CommandShowMainWindow: {
					Scope:   settingservice.ShortcutScopeGlobal,
					Binding: &settingservice.ShortcutBinding{Modifiers: []settingservice.ShortcutModifier{settingservice.ShortcutModifierPrimary}, Key: "a"},
				},
				CommandToggleProxy: {
					Scope:   settingservice.ShortcutScopeGlobal,
					Binding: &settingservice.ShortcutBinding{Modifiers: []settingservice.ShortcutModifier{settingservice.ShortcutModifierControl}, Key: "A"},
				},
			}},
			status: ShortcutStatusConflict,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registrar := newFakeRegistrar()
			store := newFakeConfigStore(nil)
			service := NewForEnvironment(registrar, store, nil, Environment{OS: "windows"})
			result, err := service.ApplyShortcutConfig(test.config)
			if err != nil {
				t.Fatalf("expected reportable failure, got Go error: %v", err)
			}
			wantCode := ApplyErrorUnsupported
			if test.status == ShortcutStatusConflict {
				wantCode = ApplyErrorRegistration
			}
			if result.Applied || result.ErrorCode != wantCode {
				t.Fatalf("unexpected result: %+v", result)
			}
			foundStatus := false
			for _, commandState := range result.RuntimeState.Commands {
				foundStatus = foundStatus || commandState.Status == test.status
			}
			if !foundStatus {
				t.Fatalf("expected status %q, got %+v", test.status, result.RuntimeState.Commands)
			}
			if store.saveCalls != 0 || len(registrar.registeredKeys()) != 0 {
				t.Fatalf("rejected apply changed state: saves=%d registered=%v", store.saveCalls, registrar.registeredKeys())
			}
		})
	}
}

func TestApplyAddsAndSwapsCommandWithoutReregisteringAccelerator(t *testing.T) {
	registrar := newFakeRegistrar()
	store := newFakeConfigStore(nil)
	var invoked []string
	service := NewForEnvironment(registrar, store, func(commandID string) {
		invoked = append(invoked, commandID)
	}, Environment{OS: "windows"})

	first, err := service.ApplyShortcutConfig(globalConfig(CommandShowMainWindow, []settingservice.ShortcutModifier{settingservice.ShortcutModifierPrimary}, "a"))
	if err != nil || !first.Applied {
		t.Fatalf("first apply: result=%+v err=%v", first, err)
	}
	if !registrar.trigger("Ctrl+A") || !reflect.DeepEqual(invoked, []string{CommandShowMainWindow}) {
		t.Fatalf("first callback invoked %v", invoked)
	}

	second, err := service.ApplyShortcutConfig(globalConfig(CommandToggleProxy, []settingservice.ShortcutModifier{settingservice.ShortcutModifierControl}, "a"))
	if err != nil || !second.Applied {
		t.Fatalf("swap apply: result=%+v err=%v", second, err)
	}
	if len(registrar.registerCalls) != 1 || len(registrar.unregisterCalls) != 0 {
		t.Fatalf("accelerator swap touched OS registration: register=%v unregister=%v", registrar.registerCalls, registrar.unregisterCalls)
	}
	registrar.trigger("Ctrl+A")
	if !reflect.DeepEqual(invoked, []string{CommandShowMainWindow, CommandToggleProxy}) {
		t.Fatalf("callback did not consult swapped mapping: %v", invoked)
	}
}

func TestRegistrationFailureRollsBackEarlierAdditions(t *testing.T) {
	registrar := newFakeRegistrar()
	registrar.registerFailures["Ctrl+B"] = errors.New("owned by another application")
	store := newFakeConfigStore(nil)
	service := NewForEnvironment(registrar, store, nil, Environment{OS: "windows"})
	config := &settingservice.ShortcutConfig{Overrides: map[string]settingservice.ShortcutOverride{
		CommandShowMainWindow: {
			Scope:   settingservice.ShortcutScopeGlobal,
			Binding: &settingservice.ShortcutBinding{Modifiers: []settingservice.ShortcutModifier{settingservice.ShortcutModifierControl}, Key: "a"},
		},
		CommandToggleProxy: {
			Scope:   settingservice.ShortcutScopeGlobal,
			Binding: &settingservice.ShortcutBinding{Modifiers: []settingservice.ShortcutModifier{settingservice.ShortcutModifierControl}, Key: "b"},
		},
	}}

	result, err := service.ApplyShortcutConfig(config)
	if err != nil || result.Applied || result.ErrorCode != ApplyErrorRegistration {
		t.Fatalf("unexpected result=%+v err=%v", result, err)
	}
	if len(registrar.registeredKeys()) != 0 || store.saveCalls != 0 {
		t.Fatalf("registration rollback incomplete: registered=%v saves=%d", registrar.registeredKeys(), store.saveCalls)
	}
	if state, exists := result.RuntimeState.Commands[CommandShowMainWindow]; exists && state.Status == ShortcutStatusActive {
		t.Fatalf("rolled-back earlier addition was reported active: %+v", result.RuntimeState)
	}
	if state := result.RuntimeState.Commands[CommandToggleProxy]; state.Status != ShortcutStatusConflict {
		t.Fatalf("failed addition status = %+v, want conflict", state)
	}
}

func TestRegistrationRollbackCleanupFailureIsReported(t *testing.T) {
	registrar := newFakeRegistrar()
	registrar.registerFailures["Ctrl+B"] = errors.New("owned by another application")
	registrar.unregisterFailures["Ctrl+A"] = errors.New("cleanup failed")
	store := newFakeConfigStore(nil)
	service := NewForEnvironment(registrar, store, nil, Environment{OS: "windows"})
	config := &settingservice.ShortcutConfig{Overrides: map[string]settingservice.ShortcutOverride{
		CommandShowMainWindow: {
			Scope:   settingservice.ShortcutScopeGlobal,
			Binding: &settingservice.ShortcutBinding{Modifiers: []settingservice.ShortcutModifier{settingservice.ShortcutModifierControl}, Key: "a"},
		},
		CommandToggleProxy: {
			Scope:   settingservice.ShortcutScopeGlobal,
			Binding: &settingservice.ShortcutBinding{Modifiers: []settingservice.ShortcutModifier{settingservice.ShortcutModifierControl}, Key: "b"},
		},
	}}

	result, err := service.ApplyShortcutConfig(config)
	if err != nil || result.Applied || result.ErrorCode != ApplyErrorRollbackIncomplete || !strings.Contains(result.ErrorMessage, "cleanup failed") {
		t.Fatalf("cleanup failure was not reported: result=%+v err=%v", result, err)
	}
}

func TestPersistenceFailureUnregistersNewAdditions(t *testing.T) {
	registrar := newFakeRegistrar()
	store := newFakeConfigStore(nil)
	store.failSave[1] = errors.New("disk full")
	service := NewForEnvironment(registrar, store, nil, Environment{OS: "windows"})

	result, err := service.ApplyShortcutConfig(globalConfig(CommandShowMainWindow, []settingservice.ShortcutModifier{settingservice.ShortcutModifierControl}, "a"))
	if err != nil || result.Applied || result.ErrorCode != ApplyErrorPersistence {
		t.Fatalf("unexpected result=%+v err=%v", result, err)
	}
	if len(registrar.registeredKeys()) != 0 {
		t.Fatalf("persistence rollback left registration: %v", registrar.registeredKeys())
	}
}

func TestUnregisterFailureRestoresConfigMappingAndRegistrations(t *testing.T) {
	registrar := newFakeRegistrar()
	registrar.unregisterFailureRemoves = true
	store := newFakeConfigStore(nil)
	var invoked []string
	service := NewForEnvironment(registrar, store, func(commandID string) { invoked = append(invoked, commandID) }, Environment{OS: "windows"})
	oldConfig := globalConfig(CommandShowMainWindow, []settingservice.ShortcutModifier{settingservice.ShortcutModifierControl}, "a")
	if result, _ := service.ApplyShortcutConfig(oldConfig); !result.Applied {
		t.Fatalf("initial apply failed: %+v", result)
	}
	registrar.unregisterFailures["Ctrl+A"] = errors.New("native unregister failed")

	result, err := service.ApplyShortcutConfig(globalConfig(CommandToggleProxy, []settingservice.ShortcutModifier{settingservice.ShortcutModifierControl}, "b"))
	if err != nil || result.Applied || result.ErrorCode != ApplyErrorUnregister {
		t.Fatalf("unexpected result=%+v err=%v", result, err)
	}
	if !registrar.IsRegistered("Ctrl+A") || registrar.IsRegistered("Ctrl+B") {
		t.Fatalf("registrations not restored: %v", registrar.registeredKeys())
	}
	if !reflect.DeepEqual(store.config, settingservice.NormalizeShortcutConfig(oldConfig)) {
		t.Fatalf("persisted config not restored: %+v", store.config)
	}
	registrar.trigger("Ctrl+A")
	if !reflect.DeepEqual(invoked, []string{CommandShowMainWindow}) {
		t.Fatalf("old mapping not restored: %v", invoked)
	}
}

func TestUnregisterRollbackPersistenceFailureIsReported(t *testing.T) {
	registrar := newFakeRegistrar()
	registrar.unregisterFailureRemoves = true
	store := newFakeConfigStore(nil)
	service := NewForEnvironment(registrar, store, nil, Environment{OS: "windows"})
	if result, _ := service.ApplyShortcutConfig(globalConfig(CommandShowMainWindow, []settingservice.ShortcutModifier{settingservice.ShortcutModifierControl}, "a")); !result.Applied {
		t.Fatalf("initial apply failed: %+v", result)
	}
	registrar.unregisterFailures["Ctrl+A"] = errors.New("native unregister failed")
	store.failSave[3] = errors.New("rollback disk failure")

	result, err := service.ApplyShortcutConfig(globalConfig(CommandToggleProxy, []settingservice.ShortcutModifier{settingservice.ShortcutModifierControl}, "b"))
	if err != nil || result.Applied || result.ErrorCode != ApplyErrorRollbackIncomplete || !strings.Contains(result.ErrorMessage, "restore persisted") {
		t.Fatalf("rollback failure was not surfaced: result=%+v err=%v", result, err)
	}
}

func TestUnregisterRollbackReregistrationFailureIsReported(t *testing.T) {
	registrar := newFakeRegistrar()
	registrar.unregisterFailureRemoves = true
	store := newFakeConfigStore(nil)
	service := NewForEnvironment(registrar, store, nil, Environment{OS: "windows"})
	if result, _ := service.ApplyShortcutConfig(globalConfig(CommandShowMainWindow, []settingservice.ShortcutModifier{settingservice.ShortcutModifierControl}, "a")); !result.Applied {
		t.Fatalf("initial apply failed: %+v", result)
	}
	registrar.unregisterFailures["Ctrl+A"] = errors.New("native unregister failed")
	registrar.registerFailures["Ctrl+A"] = errors.New("restore conflict")

	result, err := service.ApplyShortcutConfig(globalConfig(CommandToggleProxy, []settingservice.ShortcutModifier{settingservice.ShortcutModifierControl}, "b"))
	if err != nil || result.Applied || result.ErrorCode != ApplyErrorRollbackIncomplete || !strings.Contains(result.ErrorMessage, "restore conflict") {
		t.Fatalf("restore registration failure was not reported: result=%+v err=%v", result, err)
	}
}

func TestWaylandAndMacRuntimeWarnings(t *testing.T) {
	for _, test := range []struct {
		name        string
		environment Environment
		status      ShortcutRuntimeStatus
		warning     string
	}{
		{name: "wayland", environment: Environment{OS: "linux", Wayland: true}, status: ShortcutStatusPortalPending, warning: WarningWaylandPortalControlsBinding},
		{name: "mac", environment: Environment{OS: "darwin"}, status: ShortcutStatusActive, warning: WarningMacNonANSICompatibility},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := NewForEnvironment(newFakeRegistrar(), newFakeConfigStore(nil), nil, test.environment)
			result, err := service.ApplyShortcutConfig(globalConfig(CommandShowMainWindow, []settingservice.ShortcutModifier{settingservice.ShortcutModifierPrimary}, "F20"))
			if err != nil || !result.Applied {
				t.Fatalf("apply: result=%+v err=%v", result, err)
			}
			if result.RuntimeState.Commands[CommandShowMainWindow].Status != test.status || !reflect.DeepEqual(result.Warnings, []string{test.warning}) {
				t.Fatalf("unexpected runtime state: %+v", result)
			}
		})
	}
}

func TestUnsupportedPlatformPersistsPortableConfigWithoutRegistering(t *testing.T) {
	registrar := newFakeRegistrar()
	store := newFakeConfigStore(nil)
	service := NewForEnvironment(registrar, store, nil, Environment{OS: "freebsd"})
	result, err := service.ApplyShortcutConfig(globalConfig(CommandShowMainWindow, []settingservice.ShortcutModifier{settingservice.ShortcutModifierControl}, "a"))
	if err != nil || !result.Applied || result.RuntimeState.Commands[CommandShowMainWindow].Status != ShortcutStatusUnsupported {
		t.Fatalf("unexpected result=%+v err=%v", result, err)
	}
	if len(registrar.registeredKeys()) != 0 || store.saveCalls != 1 {
		t.Fatalf("unsupported platform side effects: registered=%v saves=%d", registrar.registeredKeys(), store.saveCalls)
	}
}

func TestStartRegistersWithoutRepersistingAndNotifies(t *testing.T) {
	config := globalConfig(CommandShowMainWindow, []settingservice.ShortcutModifier{settingservice.ShortcutModifierControl}, "Home")
	registrar := newFakeRegistrar()
	store := newFakeConfigStore(config)
	var notifications []ShortcutApplyResult
	service := NewForEnvironmentWithNotifier(registrar, store, nil, func(result ShortcutApplyResult) { notifications = append(notifications, result) }, Environment{OS: "windows"})

	result, err := service.Start()
	if err != nil || !result.Applied || store.saveCalls != 0 || !registrar.IsRegistered("Ctrl+Home") {
		t.Fatalf("start: result=%+v err=%v saves=%d registered=%v", result, err, store.saveCalls, registrar.registeredKeys())
	}
	if len(notifications) != 1 || !notifications[0].Applied {
		t.Fatalf("successful start notification missing: %+v", notifications)
	}
}

func TestFailedApplyDoesNotNotify(t *testing.T) {
	notifications := 0
	service := NewForEnvironmentWithNotifier(newFakeRegistrar(), newFakeConfigStore(nil), nil, func(ShortcutApplyResult) { notifications++ }, Environment{OS: "windows"})
	result, _ := service.ApplyShortcutConfig(globalConfig("not.allowed", []settingservice.ShortcutModifier{settingservice.ShortcutModifierControl}, "a"))
	if result.Applied || notifications != 0 {
		t.Fatalf("failed apply notified: result=%+v notifications=%d", result, notifications)
	}
}

func TestStartConflictStoresAndNotifiesFailureRuntime(t *testing.T) {
	config := globalConfig(CommandShowMainWindow, []settingservice.ShortcutModifier{settingservice.ShortcutModifierControl}, "a")
	registrar := newFakeRegistrar()
	registrar.registerFailures["Ctrl+A"] = errors.New("system conflict")
	store := newFakeConfigStore(config)
	var notifications []ShortcutApplyResult
	service := NewForEnvironmentWithNotifier(registrar, store, nil, func(result ShortcutApplyResult) {
		notifications = append(notifications, result)
	}, Environment{OS: "windows"})

	result, err := service.Start()
	if err != nil || result.Applied || result.RuntimeState.Commands[CommandShowMainWindow].Status != ShortcutStatusConflict {
		t.Fatalf("unexpected startup result=%+v err=%v", result, err)
	}
	if store.saveCalls != 0 || service.GetShortcutRuntimeState().Commands[CommandShowMainWindow].Status != ShortcutStatusConflict {
		t.Fatalf("startup conflict was not retained without persistence: saves=%d state=%+v", store.saveCalls, service.GetShortcutRuntimeState())
	}
	if len(notifications) != 1 || notifications[0].RuntimeState.Commands[CommandShowMainWindow].Status != ShortcutStatusConflict {
		t.Fatalf("startup conflict notification missing: %+v", notifications)
	}
}

func TestShutdownReleasesOwnedRegistrationsAndClearsCallbackMap(t *testing.T) {
	registrar := newFakeRegistrar()
	store := newFakeConfigStore(nil)
	invocations := 0
	service := NewForEnvironment(registrar, store, func(string) { invocations++ }, Environment{OS: "windows"})
	if result, _ := service.ApplyShortcutConfig(globalConfig(CommandShowMainWindow, []settingservice.ShortcutModifier{settingservice.ShortcutModifierControl}, "a")); !result.Applied {
		t.Fatalf("apply failed: %+v", result)
	}
	callback := registrar.registered["Ctrl+A"]
	if err := service.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if len(registrar.registeredKeys()) != 0 || len(service.GetShortcutRuntimeState().Commands) != 0 {
		t.Fatalf("shutdown state not cleared: registered=%v state=%+v", registrar.registeredKeys(), service.GetShortcutRuntimeState())
	}
	callback()
	if invocations != 0 {
		t.Fatalf("stale callback remained active: %d", invocations)
	}
}

func TestShutdownClearsMappingEvenWhenNativeUnregisterFails(t *testing.T) {
	registrar := newFakeRegistrar()
	store := newFakeConfigStore(nil)
	invocations := 0
	service := NewForEnvironment(registrar, store, func(string) { invocations++ }, Environment{OS: "windows"})
	if result, _ := service.ApplyShortcutConfig(globalConfig(CommandShowMainWindow, []settingservice.ShortcutModifier{settingservice.ShortcutModifierControl}, "a")); !result.Applied {
		t.Fatalf("apply failed: %+v", result)
	}
	callback := registrar.registered["Ctrl+A"]
	registrar.unregisterFailures["Ctrl+A"] = errors.New("native shutdown failure")
	if err := service.Shutdown(); err == nil || !strings.Contains(err.Error(), "native shutdown failure") {
		t.Fatalf("shutdown failure not reported: %v", err)
	}
	callback()
	if invocations != 0 || len(service.GetShortcutRuntimeState().Commands) != 0 {
		t.Fatalf("shutdown failure left executable mapping: invocations=%d state=%+v", invocations, service.GetShortcutRuntimeState())
	}
}

func TestF21IsRejected(t *testing.T) {
	service := NewForEnvironment(newFakeRegistrar(), newFakeConfigStore(nil), nil, Environment{OS: "windows"})
	result, err := service.ApplyShortcutConfig(globalConfig(CommandShowMainWindow, []settingservice.ShortcutModifier{settingservice.ShortcutModifierControl}, "F21"))
	if err != nil || result.Applied || result.ErrorCode != ApplyErrorUnsupported {
		t.Fatalf("F21 should be reportably rejected: result=%+v err=%v", result, err)
	}
}

func TestSpaceAndCollapsedModifiersResolveDeterministically(t *testing.T) {
	tests := []struct {
		name        string
		binding     settingservice.ShortcutBinding
		osName      string
		accelerator string
	}{
		{
			name:        "literal keyboard event space",
			binding:     settingservice.ShortcutBinding{Modifiers: []settingservice.ShortcutModifier{settingservice.ShortcutModifierControl}, Key: " "},
			osName:      "windows",
			accelerator: "Ctrl+Space",
		},
		{
			name:        "windows primary and control collapse",
			binding:     settingservice.ShortcutBinding{Modifiers: []settingservice.ShortcutModifier{settingservice.ShortcutModifierPrimary, settingservice.ShortcutModifierControl}, Key: "a"},
			osName:      "windows",
			accelerator: "Ctrl+A",
		},
		{
			name:        "mac primary and super collapse with total order",
			binding:     settingservice.ShortcutBinding{Modifiers: []settingservice.ShortcutModifier{settingservice.ShortcutModifierControl, settingservice.ShortcutModifierSuper, settingservice.ShortcutModifierPrimary}, Key: "a"},
			osName:      "darwin",
			accelerator: "Cmd+Ctrl+A",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for range 20 {
				got, err := resolveAccelerator(test.binding, test.osName)
				if err != nil || got != test.accelerator {
					t.Fatalf("resolveAccelerator() = %q, %v; want %q", got, err, test.accelerator)
				}
			}
		})
	}
}
