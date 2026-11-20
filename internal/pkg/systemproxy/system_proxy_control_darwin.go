package systemproxy

import (
	"bytes"
	"errors"
	"fmt"
)

type darwinProxySnapshot struct {
	ServiceID string
	Config    []byte
}

type darwinPlatformDriver struct {
	api                           *darwinProxyAPI // optional injection; production initializes lazily
	serviceID                     string
	originalConfig, managedConfig []byte
}

func newPlatformDriver() platformDriver               { return &darwinPlatformDriver{} }
func (*darwinPlatformDriver) Supported() bool         { return true }
func (*darwinPlatformDriver) Supports(mode Mode) bool { return mode == ModeHTTP || mode == ModeSOCKS5 }

func (d *darwinPlatformDriver) native() (*darwinProxyAPI, error) {
	if d.api != nil {
		return d.api, nil
	}
	return loadDarwinProxy()
}

func (d *darwinPlatformDriver) Snapshot() (any, error) {
	a, err := d.native()
	if err != nil {
		return nil, err
	}
	id := a.primaryService()
	if id == "" {
		return nil, errors.New("macOS primary network service is not available")
	}
	config, err := a.readConfiguration(id)
	if err != nil {
		return nil, err
	}
	defer a.release(config)
	data, err := a.serialize(config)
	if err != nil {
		return nil, err
	}
	d.serviceID, d.originalConfig, d.managedConfig = id, bytes.Clone(data), nil
	return darwinProxySnapshot{ServiceID: id, Config: data}, nil
}

func (d *darwinPlatformDriver) expected() []byte {
	if len(d.managedConfig) != 0 {
		return d.managedConfig
	}
	return d.originalConfig
}

func (d *darwinPlatformDriver) Apply(endpoint Endpoint) error {
	if err := endpoint.validate(); err != nil {
		return err
	}
	a, err := d.native()
	if err != nil {
		return err
	}
	data, err := a.writeConfiguration(d.serviceID, d.expected(), func(current uintptr) (uintptr, error) {
		return a.managedDictionary(current, endpoint)
	})
	if err != nil {
		return err
	}
	d.managedConfig = data
	return nil
}

func (d *darwinPlatformDriver) Matches(Endpoint) (bool, error) {
	if len(d.managedConfig) == 0 {
		return false, errors.New("macOS managed proxy state is not available")
	}
	a, err := d.native()
	if err != nil {
		return false, err
	}
	expected, err := a.dictionary(d.managedConfig)
	if err != nil {
		return false, err
	}
	defer a.release(expected)
	current, err := a.readConfiguration(d.serviceID)
	if errors.Is(err, errDarwinServiceMissing) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer a.release(current)
	return a.equal(current, expected) != 0, nil
}

func (d *darwinPlatformDriver) Restore(value any) error {
	snapshot, ok := value.(darwinProxySnapshot)
	if !ok || snapshot.ServiceID == "" || snapshot.ServiceID != d.serviceID || len(snapshot.Config) == 0 {
		return errors.New("invalid macOS system proxy snapshot")
	}
	a, err := d.native()
	if err != nil {
		return err
	}
	_, err = a.writeConfiguration(snapshot.ServiceID, d.expected(), func(uintptr) (uintptr, error) { return a.dictionary(snapshot.Config) })
	if err != nil {
		return err
	}
	d.originalConfig, d.managedConfig = nil, nil
	return nil
}

func (a *darwinProxyAPI) managedDictionary(current uintptr, endpoint Endpoint) (uintptr, error) {
	config := a.dictCopy(0, 0, current)
	if config == 0 {
		return 0, errors.New("copy macOS proxy dictionary")
	}
	success := false
	defer func() {
		if !success {
			a.release(config)
		}
	}()
	host := a.string(endpoint.Host)
	if host == 0 {
		return 0, errors.New("create macOS proxy host")
	}
	defer a.release(host)
	httpEnabled, socksEnabled := int32(1), int32(0)
	if endpoint.Mode == ModeSOCKS5 {
		httpEnabled, socksEnabled = 0, 1
	}
	values := []struct {
		key   string
		value int32
	}{
		{"HTTPEnable", httpEnabled}, {"HTTPSEnable", httpEnabled}, {"SOCKSEnable", socksEnabled},
		{"ProxyAutoConfigEnable", 0}, {"ProxyAutoDiscoveryEnable", 0},
	}
	kinds := []string{"HTTP", "HTTPS"}
	if socksEnabled != 0 {
		kinds = []string{"SOCKS"}
	}
	for _, kind := range kinds {
		a.dictSet(config, a.keys["kSCPropNetProxies"+kind+"Proxy"], host)
		values = append(values, struct {
			key   string
			value int32
		}{kind + "Port", int32(endpoint.Port)})
	}
	for _, pair := range values {
		number := a.numberCreate(0, cfInt32, &pair.value)
		if number == 0 {
			return 0, errors.New("create macOS proxy number")
		}
		a.dictSet(config, a.keys["kSCPropNetProxies"+pair.key], number)
		a.release(number)
	}
	success = true
	return config, nil
}

// The preferences lock spans reread, full-dictionary comparison, serialization,
// commit, apply and any compensation. Early Matches never authorizes a write.
// A failed apply can follow a successful commit. Re-synchronize and restore the
// old dictionary only if disk still equals our candidate. Never overwrite an
// external edit, including auxiliary/PAC/bypass keys. Every returned CF object,
// authorization and preferences lock has exactly one cleanup owner here.
func (a *darwinProxyAPI) writeConfiguration(id string, expectedData []byte, replacement func(uintptr) (uintptr, error)) ([]byte, error) {
	if id == "" {
		return nil, errors.New("macOS primary network service is not available")
	}
	expected, err := a.dictionary(expectedData)
	if err != nil {
		return nil, err
	}
	defer a.release(expected)
	var auth uintptr
	status := a.authorize(0, 0, 0, &auth)
	if auth != 0 {
		defer a.freeAuthorization(auth, 0)
	}
	if status != 0 || auth == 0 {
		return nil, fmt.Errorf("authorize macOS system proxy: %d", status)
	}
	name := a.string("FlowLens")
	if name == 0 {
		return nil, errors.New("create preferences name")
	}
	defer a.release(name)
	prefs := a.prefsAuthorized(0, name, 0, auth)
	if prefs == 0 {
		return nil, errors.New("create authorized macOS preferences")
	}
	defer a.release(prefs)
	if a.lock(prefs, 1) == 0 {
		return nil, errors.New("lock macOS preferences")
	}
	defer a.unlock(prefs)
	protocol, err := a.copyProtocol(prefs, id, false)
	if errors.Is(err, errDarwinServiceMissing) {
		return nil, fmt.Errorf("%w: %w", ErrChangedExternally, err)
	}
	if err != nil {
		return nil, err
	}
	// Keep the protocol alive until the transaction exits, including recovery.
	defer func() {
		if protocol != 0 {
			a.release(protocol)
		}
	}()
	current, err := a.copyConfiguration(protocol)
	if err != nil {
		return nil, err
	}
	defer a.release(current)
	if a.equal(current, expected) == 0 {
		return nil, ErrChangedExternally
	}
	candidate, err := replacement(current)
	if err != nil {
		return nil, err
	}
	defer a.release(candidate)
	data, err := a.serialize(candidate)
	if err != nil {
		return nil, err
	}
	if protocol == 0 {
		protocol, err = a.copyProtocol(prefs, id, true)
		if err != nil {
			return nil, err
		}
	}
	if a.setConfiguration(protocol, candidate) == 0 {
		return nil, errors.New("set macOS proxy configuration")
	}
	if a.commit(prefs) == 0 {
		return nil, a.recoverConfiguration(prefs, id, current, candidate, errors.New("commit macOS proxy configuration"))
	}
	if a.apply(prefs) == 0 {
		return nil, a.recoverConfiguration(prefs, id, current, candidate, errors.New("apply macOS proxy configuration"))
	}
	return data, nil
}

func (a *darwinProxyAPI) recoverConfiguration(prefs uintptr, id string, original, candidate uintptr, cause error) error {
	a.synchronize(prefs)
	protocol, err := a.copyProtocol(prefs, id, false)
	if err != nil {
		return errors.Join(cause, errProxyOwnershipLost, err)
	}
	if protocol != 0 {
		defer a.release(protocol)
	}
	current, err := a.copyConfiguration(protocol)
	if err != nil {
		return errors.Join(cause, errProxyOwnershipLost, err)
	}
	defer a.release(current)
	if a.equal(current, original) != 0 {
		return cause
	}
	if a.equal(current, candidate) == 0 {
		return errors.Join(cause, ErrChangedExternally)
	}
	if protocol == 0 || a.setConfiguration(protocol, original) == 0 || a.commit(prefs) == 0 || a.apply(prefs) == 0 {
		return errors.Join(cause, errProxyOwnershipLost, errors.New("recover macOS proxy configuration"))
	}
	return cause
}
