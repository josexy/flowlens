//go:build darwin && cgo

package systemproxy

/*
#cgo darwin LDFLAGS: -framework SystemConfiguration -framework CoreFoundation -framework Security
#include <CoreFoundation/CoreFoundation.h>
#include <SystemConfiguration/SystemConfiguration.h>
#include <Security/Authorization.h>
#include <stdlib.h>
#include <string.h>

typedef struct flowlens_proxy_snapshot_result {
	char* service_id;
	unsigned char* config;
	CFIndex config_len;
} flowlens_proxy_snapshot_result;

typedef struct flowlens_proxy_apply_result {
	int status;
	unsigned char* config;
	CFIndex config_len;
} flowlens_proxy_apply_result;

static char* flowlens_copy_cf_string(CFStringRef value) {
	if (value == NULL) {
		return NULL;
	}
	CFIndex length = CFStringGetLength(value);
	CFIndex size = CFStringGetMaximumSizeForEncoding(length, kCFStringEncodingUTF8) + 1;
	char* buffer = (char*)malloc((size_t)size);
	if (buffer == NULL) {
		return NULL;
	}
	if (!CFStringGetCString(value, buffer, size, kCFStringEncodingUTF8)) {
		free(buffer);
		return NULL;
	}
	return buffer;
}

static CFStringRef flowlens_copy_primary_service_id(void) {
	const CFStringRef keys[] = {
		CFSTR("State:/Network/Global/IPv4"),
		CFSTR("State:/Network/Global/IPv6")
	};
	for (int i = 0; i < 2; i++) {
		CFDictionaryRef state = (CFDictionaryRef)SCDynamicStoreCopyValue(NULL, keys[i]);
		if (state == NULL || CFGetTypeID(state) != CFDictionaryGetTypeID()) {
			if (state != NULL) CFRelease(state);
			continue;
		}
		CFStringRef service_id = (CFStringRef)CFDictionaryGetValue(state, kSCDynamicStorePropNetPrimaryService);
		if (service_id != NULL && CFGetTypeID(service_id) == CFStringGetTypeID()) {
			CFRetain(service_id);
			CFRelease(state);
			return service_id;
		}
		CFRelease(state);
	}
	return NULL;
}

static SCNetworkServiceRef flowlens_copy_service(SCPreferencesRef prefs, CFStringRef service_id) {
	CFArrayRef services = SCNetworkServiceCopyAll(prefs);
	if (services == NULL) {
		return NULL;
	}
	SCNetworkServiceRef result = NULL;
	CFIndex count = CFArrayGetCount(services);
	for (CFIndex i = 0; i < count; i++) {
		SCNetworkServiceRef service = (SCNetworkServiceRef)CFArrayGetValueAtIndex(services, i);
		CFStringRef candidate = SCNetworkServiceGetServiceID(service);
		if (candidate != NULL && CFEqual(candidate, service_id)) {
			result = service;
			CFRetain(result);
			break;
		}
	}
	CFRelease(services);
	return result;
}

static SCNetworkProtocolRef flowlens_copy_proxy_protocol(SCPreferencesRef prefs, CFStringRef service_id, int create) {
	SCNetworkServiceRef service = flowlens_copy_service(prefs, service_id);
	if (service == NULL) {
		return NULL;
	}
	SCNetworkProtocolRef protocol = SCNetworkServiceCopyProtocol(service, kSCNetworkProtocolTypeProxies);
	if (protocol == NULL && create) {
		if (SCNetworkServiceAddProtocolType(service, kSCNetworkProtocolTypeProxies)) {
			protocol = SCNetworkServiceCopyProtocol(service, kSCNetworkProtocolTypeProxies);
		}
	}
	CFRelease(service);
	return protocol;
}

static flowlens_proxy_snapshot_result flowlens_snapshot_service_proxy(CFStringRef service_id) {
	flowlens_proxy_snapshot_result result = {0};
	if (service_id == NULL) {
		return result;
	}
	SCPreferencesRef prefs = SCPreferencesCreate(NULL, CFSTR("FlowLens"), NULL);
	if (prefs == NULL) {
		return result;
	}
	SCNetworkProtocolRef protocol = flowlens_copy_proxy_protocol(prefs, service_id, 0);
	CFDictionaryRef config = NULL;
	if (protocol != NULL) {
		config = SCNetworkProtocolGetConfiguration(protocol);
	}
	if (config != NULL) {
		CFRetain(config);
	} else {
		config = CFDictionaryCreate(NULL, NULL, NULL, 0, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	}

	CFErrorRef error = NULL;
	CFDataRef data = CFPropertyListCreateData(NULL, config, kCFPropertyListBinaryFormat_v1_0, 0, &error);
	if (data != NULL) {
		result.config_len = CFDataGetLength(data);
		if (result.config_len > 0) {
			result.config = (unsigned char*)malloc((size_t)result.config_len);
			if (result.config != NULL) {
				memcpy(result.config, CFDataGetBytePtr(data), (size_t)result.config_len);
			}
		}
		result.service_id = flowlens_copy_cf_string(service_id);
		CFRelease(data);
	}
	if (error != NULL) CFRelease(error);
	CFRelease(config);
	if (protocol != NULL) CFRelease(protocol);
	CFRelease(prefs);
	return result;
}

static flowlens_proxy_snapshot_result flowlens_snapshot_proxy(void) {
	flowlens_proxy_snapshot_result result = {0};
	CFStringRef service_id = flowlens_copy_primary_service_id();
	if (service_id != NULL) {
		result = flowlens_snapshot_service_proxy(service_id);
		CFRelease(service_id);
	}
	return result;
}

static int flowlens_proxy_config_equals(
	const char* service_id_text,
	const unsigned char* config_bytes,
	CFIndex config_len
) {
	CFStringRef service_id = CFStringCreateWithCString(NULL, service_id_text, kCFStringEncodingUTF8);
	CFDataRef data = CFDataCreate(NULL, config_bytes, config_len);
	if (service_id == NULL || data == NULL) {
		if (service_id != NULL) CFRelease(service_id);
		if (data != NULL) CFRelease(data);
		return -1;
	}
	CFErrorRef error = NULL;
	CFPropertyListRef expected = CFPropertyListCreateWithData(NULL, data, kCFPropertyListImmutable, NULL, &error);
	CFRelease(data);
	if (expected == NULL || CFGetTypeID(expected) != CFDictionaryGetTypeID()) {
		if (expected != NULL) CFRelease(expected);
		if (error != NULL) CFRelease(error);
		CFRelease(service_id);
		return -1;
	}
	if (error != NULL) CFRelease(error);
	SCPreferencesRef prefs = SCPreferencesCreate(NULL, CFSTR("FlowLens"), NULL);
	SCNetworkProtocolRef protocol = prefs != NULL
		? flowlens_copy_proxy_protocol(prefs, service_id, 0)
		: NULL;
	CFDictionaryRef current = protocol != NULL
		? SCNetworkProtocolGetConfiguration(protocol)
		: NULL;
	CFDictionaryRef empty = NULL;
	if (current == NULL) {
		empty = CFDictionaryCreate(NULL, NULL, NULL, 0, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
		current = empty;
	}
	int result = current != NULL && CFEqual(current, expected) ? 1 : 0;
	if (empty != NULL) CFRelease(empty);
	if (protocol != NULL) CFRelease(protocol);
	if (prefs != NULL) CFRelease(prefs);
	CFRelease(expected);
	CFRelease(service_id);
	return result;
}

static void flowlens_free_proxy_snapshot(flowlens_proxy_snapshot_result* snapshot) {
	if (snapshot == NULL) return;
	if (snapshot->service_id != NULL) free(snapshot->service_id);
	if (snapshot->config != NULL) free(snapshot->config);
	snapshot->service_id = NULL;
	snapshot->config = NULL;
	snapshot->config_len = 0;
}

static void flowlens_free_proxy_apply_result(flowlens_proxy_apply_result* result) {
	if (result == NULL) return;
	if (result->config != NULL) free(result->config);
	result->status = 0;
	result->config = NULL;
	result->config_len = 0;
}

static CFDictionaryRef flowlens_copy_dictionary_from_data(const unsigned char* bytes, CFIndex length) {
	if (bytes == NULL || length <= 0) return NULL;
	CFDataRef data = CFDataCreate(NULL, bytes, length);
	if (data == NULL) return NULL;
	CFErrorRef error = NULL;
	CFPropertyListRef property = CFPropertyListCreateWithData(NULL, data, kCFPropertyListImmutable, NULL, &error);
	CFRelease(data);
	if (error != NULL) CFRelease(error);
	if (property == NULL || CFGetTypeID(property) != CFDictionaryGetTypeID()) {
		if (property != NULL) CFRelease(property);
		return NULL;
	}
	return (CFDictionaryRef)property;
}

static int flowlens_dictionary_equals_data(CFDictionaryRef current, const unsigned char* bytes, CFIndex length) {
	CFDictionaryRef expected = flowlens_copy_dictionary_from_data(bytes, length);
	if (expected == NULL) return -1;
	int result = current != NULL && CFEqual(current, expected) ? 1 : 0;
	CFRelease(expected);
	return result;
}

static int flowlens_copy_dictionary_data(
	CFDictionaryRef config,
	unsigned char** bytes,
	CFIndex* length
) {
	*bytes = NULL;
	*length = 0;
	CFErrorRef error = NULL;
	CFDataRef data = CFPropertyListCreateData(NULL, config, kCFPropertyListBinaryFormat_v1_0, 0, &error);
	if (error != NULL) CFRelease(error);
	if (data == NULL) return 0;
	*length = CFDataGetLength(data);
	if (*length > 0) {
		*bytes = (unsigned char*)malloc((size_t)*length);
		if (*bytes != NULL) {
			memcpy(*bytes, CFDataGetBytePtr(data), (size_t)*length);
		}
	}
	CFRelease(data);
	return *bytes != NULL && *length > 0;
}

static void flowlens_set_number(CFMutableDictionaryRef config, CFStringRef key, int value) {
	CFNumberRef number = CFNumberCreate(NULL, kCFNumberIntType, &value);
	if (number != NULL) {
		CFDictionarySetValue(config, key, number);
		CFRelease(number);
	}
}

static SCPreferencesRef flowlens_create_authorized_preferences(AuthorizationRef* auth) {
	*auth = NULL;
	OSStatus status = AuthorizationCreate(NULL, kAuthorizationEmptyEnvironment, kAuthorizationFlagDefaults, auth);
	if (status != errAuthorizationSuccess || *auth == NULL) return NULL;
	SCPreferencesRef prefs = SCPreferencesCreateWithAuthorization(NULL, CFSTR("FlowLens"), NULL, *auth);
	if (prefs == NULL) {
		AuthorizationFree(*auth, kAuthorizationFlagDefaults);
		*auth = NULL;
	}
	return prefs;
}

static flowlens_proxy_apply_result flowlens_apply_proxy(
	const char* service_id_text,
	const char* host_text,
	int port,
	int socks,
	const unsigned char* expected_bytes,
	CFIndex expected_len
) {
	flowlens_proxy_apply_result result = {0};
	CFStringRef service_id = CFStringCreateWithCString(NULL, service_id_text, kCFStringEncodingUTF8);
	CFStringRef host = CFStringCreateWithCString(NULL, host_text, kCFStringEncodingUTF8);
	if (service_id == NULL || host == NULL) {
		if (service_id != NULL) CFRelease(service_id);
		if (host != NULL) CFRelease(host);
		return result;
	}
	AuthorizationRef auth = NULL;
	SCPreferencesRef prefs = flowlens_create_authorized_preferences(&auth);
	if (prefs == NULL) {
		CFRelease(host);
		CFRelease(service_id);
		return result;
	}
	if (!SCPreferencesLock(prefs, TRUE)) {
		CFRelease(prefs);
		AuthorizationFree(auth, kAuthorizationFlagDefaults);
		CFRelease(host);
		CFRelease(service_id);
		return result;
	}
	SCNetworkProtocolRef protocol = flowlens_copy_proxy_protocol(prefs, service_id, 1);
	if (protocol == NULL) {
		SCPreferencesUnlock(prefs);
		CFRelease(prefs);
		AuthorizationFree(auth, kAuthorizationFlagDefaults);
		CFRelease(host);
		CFRelease(service_id);
		return result;
	}
	CFDictionaryRef current = SCNetworkProtocolGetConfiguration(protocol);
	CFDictionaryRef empty = NULL;
	if (current == NULL) {
		empty = CFDictionaryCreate(NULL, NULL, NULL, 0, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
		current = empty;
	}
	int matches = flowlens_dictionary_equals_data(current, expected_bytes, expected_len);
	if (matches != 1) {
		result.status = matches == 0 ? -2 : 0;
		if (empty != NULL) CFRelease(empty);
		CFRelease(protocol);
		SCPreferencesUnlock(prefs);
		CFRelease(prefs);
		AuthorizationFree(auth, kAuthorizationFlagDefaults);
		CFRelease(host);
		CFRelease(service_id);
		return result;
	}
	CFMutableDictionaryRef config = current != NULL
		? CFDictionaryCreateMutableCopy(NULL, 0, current)
		: CFDictionaryCreateMutable(NULL, 0, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	if (empty != NULL) CFRelease(empty);
	if (config == NULL) {
		CFRelease(protocol);
		SCPreferencesUnlock(prefs);
		CFRelease(prefs);
		AuthorizationFree(auth, kAuthorizationFlagDefaults);
		CFRelease(host);
		CFRelease(service_id);
		return result;
	}

	flowlens_set_number(config, kSCPropNetProxiesHTTPEnable, socks ? 0 : 1);
	flowlens_set_number(config, kSCPropNetProxiesHTTPSEnable, socks ? 0 : 1);
	flowlens_set_number(config, kSCPropNetProxiesSOCKSEnable, socks ? 1 : 0);
	flowlens_set_number(config, kSCPropNetProxiesProxyAutoConfigEnable, 0);
	flowlens_set_number(config, kSCPropNetProxiesProxyAutoDiscoveryEnable, 0);
	if (socks) {
		CFDictionarySetValue(config, kSCPropNetProxiesSOCKSProxy, host);
		flowlens_set_number(config, kSCPropNetProxiesSOCKSPort, port);
	} else {
		CFDictionarySetValue(config, kSCPropNetProxiesHTTPProxy, host);
		CFDictionarySetValue(config, kSCPropNetProxiesHTTPSProxy, host);
		flowlens_set_number(config, kSCPropNetProxiesHTTPPort, port);
		flowlens_set_number(config, kSCPropNetProxiesHTTPSPort, port);
	}

	int serialized = flowlens_copy_dictionary_data(config, &result.config, &result.config_len);
	int ok = serialized
		&& SCNetworkProtocolSetConfiguration(protocol, config)
		&& SCPreferencesCommitChanges(prefs)
		&& SCPreferencesApplyChanges(prefs);
	if (!ok && result.config != NULL) {
		free(result.config);
		result.config = NULL;
		result.config_len = 0;
	}
	result.status = ok ? 1 : 0;
	CFRelease(config);
	CFRelease(protocol);
	SCPreferencesUnlock(prefs);
	CFRelease(prefs);
	AuthorizationFree(auth, kAuthorizationFlagDefaults);
	CFRelease(host);
	CFRelease(service_id);
	return result;
}

static int flowlens_restore_proxy(
	const char* service_id_text,
	const unsigned char* config_bytes,
	CFIndex config_len,
	const unsigned char* expected_bytes,
	CFIndex expected_len
) {
	CFStringRef service_id = CFStringCreateWithCString(NULL, service_id_text, kCFStringEncodingUTF8);
	CFDictionaryRef property = flowlens_copy_dictionary_from_data(config_bytes, config_len);
	if (service_id == NULL || property == NULL) {
		if (service_id != NULL) CFRelease(service_id);
		if (property != NULL) CFRelease(property);
		return 0;
	}
	AuthorizationRef auth = NULL;
	SCPreferencesRef prefs = flowlens_create_authorized_preferences(&auth);
	if (prefs == NULL) {
		CFRelease(property);
		CFRelease(service_id);
		return 0;
	}
	if (!SCPreferencesLock(prefs, TRUE)) {
		CFRelease(prefs);
		AuthorizationFree(auth, kAuthorizationFlagDefaults);
		CFRelease(property);
		CFRelease(service_id);
		return 0;
	}
	SCNetworkProtocolRef protocol = flowlens_copy_proxy_protocol(prefs, service_id, 1);
	CFDictionaryRef current = protocol != NULL ? SCNetworkProtocolGetConfiguration(protocol) : NULL;
	CFDictionaryRef empty = NULL;
	if (current == NULL) {
		empty = CFDictionaryCreate(NULL, NULL, NULL, 0, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
		current = empty;
	}
	int matches = flowlens_dictionary_equals_data(current, expected_bytes, expected_len);
	int ok = 0;
	if (protocol != NULL && matches == 1) {
		ok = SCNetworkProtocolSetConfiguration(protocol, property)
			&& SCPreferencesCommitChanges(prefs)
			&& SCPreferencesApplyChanges(prefs);
	}
	if (empty != NULL) CFRelease(empty);
	if (protocol != NULL) CFRelease(protocol);
	SCPreferencesUnlock(prefs);
	CFRelease(prefs);
	AuthorizationFree(auth, kAuthorizationFlagDefaults);
	CFRelease(property);
	CFRelease(service_id);
	if (matches == 0) return -2;
	return ok;
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

type darwinProxySnapshot struct {
	ServiceID string
	Config    []byte
}

type darwinPlatformDriver struct {
	serviceID      string
	originalConfig []byte
	managedConfig  []byte
}

func newPlatformDriver() platformDriver {
	return &darwinPlatformDriver{}
}

func (*darwinPlatformDriver) Supported() bool {
	return true
}

func (*darwinPlatformDriver) Supports(mode Mode) bool {
	return mode == ModeHTTP || mode == ModeSOCKS5
}

func (d *darwinPlatformDriver) Snapshot() (any, error) {
	result := C.flowlens_snapshot_proxy()
	defer C.flowlens_free_proxy_snapshot(&result)
	if result.service_id == nil || result.config == nil || result.config_len <= 0 {
		return nil, errors.New("failed to snapshot macOS system proxy")
	}
	snapshot := darwinProxySnapshot{
		ServiceID: C.GoString(result.service_id),
		Config:    C.GoBytes(unsafe.Pointer(result.config), C.int(result.config_len)),
	}
	d.serviceID = snapshot.ServiceID
	d.originalConfig = append(d.originalConfig[:0], snapshot.Config...)
	d.managedConfig = nil
	return snapshot, nil
}

func (d *darwinPlatformDriver) Apply(endpoint Endpoint) error {
	if d.serviceID == "" {
		return errors.New("macOS primary network service is not available")
	}
	serviceID := C.CString(d.serviceID)
	host := C.CString(endpoint.Host)
	defer C.free(unsafe.Pointer(serviceID))
	defer C.free(unsafe.Pointer(host))
	socks := C.int(0)
	if endpoint.Mode == ModeSOCKS5 {
		socks = 1
	}
	expectedConfig := d.managedConfig
	if len(expectedConfig) == 0 {
		expectedConfig = d.originalConfig
	}
	if len(expectedConfig) == 0 {
		return errors.New("macOS expected proxy state is not available")
	}
	result := C.flowlens_apply_proxy(
		serviceID,
		host,
		C.int(endpoint.Port),
		socks,
		(*C.uchar)(unsafe.Pointer(&expectedConfig[0])),
		C.CFIndex(len(expectedConfig)),
	)
	defer C.flowlens_free_proxy_apply_result(&result)
	if result.status == -2 {
		return ErrChangedExternally
	}
	if result.status != 1 || result.config == nil || result.config_len <= 0 {
		return errors.New("failed to apply macOS system proxy")
	}
	d.managedConfig = C.GoBytes(unsafe.Pointer(result.config), C.int(result.config_len))
	return nil
}

func (d *darwinPlatformDriver) Matches(Endpoint) (bool, error) {
	if len(d.managedConfig) == 0 {
		return false, errors.New("macOS managed proxy state is not available")
	}
	serviceID := C.CString(d.serviceID)
	defer C.free(unsafe.Pointer(serviceID))
	result := C.flowlens_proxy_config_equals(
		serviceID,
		(*C.uchar)(unsafe.Pointer(&d.managedConfig[0])),
		C.CFIndex(len(d.managedConfig)),
	)
	if result < 0 {
		return false, errors.New("failed to inspect macOS system proxy")
	}
	return result != 0, nil
}

func (d *darwinPlatformDriver) Restore(value any) error {
	snapshot, ok := value.(darwinProxySnapshot)
	if !ok || snapshot.ServiceID == "" || len(snapshot.Config) == 0 {
		return errors.New("invalid macOS system proxy snapshot")
	}
	serviceID := C.CString(snapshot.ServiceID)
	defer C.free(unsafe.Pointer(serviceID))
	expectedConfig := d.managedConfig
	if len(expectedConfig) == 0 {
		expectedConfig = d.originalConfig
	}
	if len(expectedConfig) == 0 {
		return errors.New("macOS expected proxy state is not available")
	}
	result := C.flowlens_restore_proxy(
		serviceID,
		(*C.uchar)(unsafe.Pointer(&snapshot.Config[0])),
		C.CFIndex(len(snapshot.Config)),
		(*C.uchar)(unsafe.Pointer(&expectedConfig[0])),
		C.CFIndex(len(expectedConfig)),
	)
	if result == -2 {
		return ErrChangedExternally
	}
	if result == 0 {
		return errors.New("failed to restore macOS system proxy")
	}
	d.serviceID = snapshot.ServiceID
	d.originalConfig = nil
	d.managedConfig = nil
	return nil
}
