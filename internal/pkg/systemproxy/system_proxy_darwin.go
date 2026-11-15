//go:build darwin && cgo

package systemproxy

/*
#cgo darwin LDFLAGS: -framework SystemConfiguration -framework CoreFoundation
#include <CoreFoundation/CoreFoundation.h>
#include <SystemConfiguration/SystemConfiguration.h>
#include <stdlib.h>

typedef struct darwin_proxy_settings {
	int http_enabled;
	char* http_host;
	int http_port;
	int https_enabled;
	char* https_host;
	int https_port;
	int socks_enabled;
	char* socks_host;
	int socks_port;
} darwin_proxy_settings;

static int proxy_setting_enabled(CFDictionaryRef settings, CFStringRef key) {
	if (settings == NULL || key == NULL) {
		return 0;
	}

	CFTypeRef value = CFDictionaryGetValue(settings, key);
	if (value == NULL) {
		return 0;
	}

	CFTypeID type_id = CFGetTypeID(value);
	if (type_id == CFBooleanGetTypeID()) {
		return CFBooleanGetValue((CFBooleanRef)value) != 0;
	}
	if (type_id == CFNumberGetTypeID()) {
		int enabled = 0;
		if (CFNumberGetValue((CFNumberRef)value, kCFNumberIntType, &enabled)) {
			return enabled != 0;
		}
	}
	return 0;
}

static int proxy_setting_port(CFDictionaryRef settings, CFStringRef key) {
	if (settings == NULL || key == NULL) {
		return 0;
	}

	CFTypeRef value = CFDictionaryGetValue(settings, key);
	if (value == NULL || CFGetTypeID(value) != CFNumberGetTypeID()) {
		return 0;
	}

	int port = 0;
	if (!CFNumberGetValue((CFNumberRef)value, kCFNumberIntType, &port)) {
		return 0;
	}
	return port;
}

static char* proxy_setting_string(CFDictionaryRef settings, CFStringRef key) {
	if (settings == NULL || key == NULL) {
		return NULL;
	}

	CFTypeRef value = CFDictionaryGetValue(settings, key);
	if (value == NULL || CFGetTypeID(value) != CFStringGetTypeID()) {
		return NULL;
	}

	CFStringRef str = (CFStringRef)value;
	CFIndex length = CFStringGetLength(str);
	CFIndex size = CFStringGetMaximumSizeForEncoding(length, kCFStringEncodingUTF8) + 1;
	char* buffer = (char*)malloc((size_t)size);
	if (buffer == NULL) {
		return NULL;
	}

	if (!CFStringGetCString(str, buffer, size, kCFStringEncodingUTF8)) {
		free(buffer);
		return NULL;
	}
	return buffer;
}

static darwin_proxy_settings copy_darwin_proxy_settings(void) {
	darwin_proxy_settings result = {0};
	CFDictionaryRef settings = SCDynamicStoreCopyProxies(NULL);
	if (settings == NULL) {
		return result;
	}

	result.http_enabled = proxy_setting_enabled(settings, kSCPropNetProxiesHTTPEnable);
	result.http_host = proxy_setting_string(settings, kSCPropNetProxiesHTTPProxy);
	result.http_port = proxy_setting_port(settings, kSCPropNetProxiesHTTPPort);
	result.https_enabled = proxy_setting_enabled(settings, kSCPropNetProxiesHTTPSEnable);
	result.https_host = proxy_setting_string(settings, kSCPropNetProxiesHTTPSProxy);
	result.https_port = proxy_setting_port(settings, kSCPropNetProxiesHTTPSPort);
	result.socks_enabled = proxy_setting_enabled(settings, kSCPropNetProxiesSOCKSEnable);
	result.socks_host = proxy_setting_string(settings, kSCPropNetProxiesSOCKSProxy);
	result.socks_port = proxy_setting_port(settings, kSCPropNetProxiesSOCKSPort);

	CFRelease(settings);
	return result;
}

static void free_darwin_proxy_settings(darwin_proxy_settings* settings) {
	if (settings == NULL) {
		return;
	}
	if (settings->http_host != NULL) {
		free(settings->http_host);
		settings->http_host = NULL;
	}
	if (settings->https_host != NULL) {
		free(settings->https_host);
		settings->https_host = NULL;
	}
	if (settings->socks_host != NULL) {
		free(settings->socks_host);
		settings->socks_host = NULL;
	}
}
*/
import "C"

func fromPlatform() string {
	settings := C.copy_darwin_proxy_settings()
	defer C.free_darwin_proxy_settings(&settings)

	return proxyURLFromSystemProxyCandidates([]systemProxyCandidate{
		{
			Enabled:       settings.http_enabled != 0,
			Host:          cStringValue(settings.http_host),
			Port:          int(settings.http_port),
			DefaultScheme: "http",
		},
		{
			Enabled:       settings.https_enabled != 0,
			Host:          cStringValue(settings.https_host),
			Port:          int(settings.https_port),
			DefaultScheme: "http",
		},
		{
			Enabled:       settings.socks_enabled != 0,
			Host:          cStringValue(settings.socks_host),
			Port:          int(settings.socks_port),
			DefaultScheme: "socks5",
		},
	})
}

func cStringValue(value *C.char) string {
	if value == nil {
		return ""
	}
	return C.GoString(value)
}
