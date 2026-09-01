package systemproxy

import (
	"net"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/http/httpproxy"
)

// Get returns the best available system upstream proxy URL.
func Get() string {
	if proxy := FromEnvironment(); proxy != "" {
		return proxy
	}
	return fromPlatform()
}

func FromEnvironment() string {
	proxyConfig := httpproxy.FromEnvironment()
	for _, candidate := range []string{proxyConfig.HTTPProxy, proxyConfig.HTTPSProxy} {
		if proxy := normalizeURL(candidate, "http"); proxy != "" {
			return proxy
		}
	}
	return ""
}

func proxyURLFromWindowsProxyServer(proxyServer string) string {
	proxyServer = strings.TrimSpace(proxyServer)
	if proxyServer == "" {
		return ""
	}

	entries := strings.Split(proxyServer, ";")
	keyedProxies := make(map[string]string, len(entries))
	hasKeyedProxy := false
	for _, entry := range entries {
		key, value, ok := strings.Cut(strings.TrimSpace(entry), "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		hasKeyedProxy = true
		keyedProxies[key] = value
	}

	if !hasKeyedProxy {
		return normalizeURL(proxyServer, "http")
	}

	candidates := []struct {
		key           string
		defaultScheme string
	}{
		{key: "http", defaultScheme: "http"},
		{key: "https", defaultScheme: "http"},
		{key: "socks", defaultScheme: "socks5"},
	}
	for _, candidate := range candidates {
		if proxy := normalizeURL(keyedProxies[candidate.key], candidate.defaultScheme); proxy != "" {
			return proxy
		}
	}
	return ""
}

type systemProxyCandidate struct {
	Enabled       bool
	Host          string
	Port          int
	DefaultScheme string
}

func proxyURLFromSystemProxyCandidates(candidates []systemProxyCandidate) string {
	for _, candidate := range candidates {
		if !candidate.Enabled {
			continue
		}
		if proxy := proxyURLFromHostPort(candidate.Host, candidate.Port, candidate.DefaultScheme); proxy != "" {
			return proxy
		}
	}
	return ""
}

func proxyURLFromHostPort(host string, port int, defaultScheme string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if port <= 0 {
		return normalizeURL(host, defaultScheme)
	}

	if strings.Contains(host, "://") {
		parsedURL, err := url.Parse(host)
		if err != nil || parsedURL.Host == "" || parsedURL.Port() != "" {
			return normalizeURL(host, defaultScheme)
		}
		parsedURL.Host = net.JoinHostPort(parsedURL.Hostname(), strconv.Itoa(port))
		return normalizeURL(parsedURL.String(), defaultScheme)
	}

	if !hostIncludesPort(host) {
		host = net.JoinHostPort(host, strconv.Itoa(port))
	}
	return normalizeURL(host, defaultScheme)
}

func hostIncludesPort(host string) bool {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return true
	}

	if strings.HasPrefix(host, "[") {
		return false
	}

	hostPart, portPart, ok := strings.Cut(host, ":")
	return ok && hostPart != "" && portPart != "" && !strings.Contains(portPart, ":")
}

func normalizeURL(rawProxy string, defaultScheme string) string {
	rawProxy = strings.TrimSpace(rawProxy)
	if rawProxy == "" {
		return ""
	}
	if !strings.Contains(rawProxy, "://") {
		rawProxy = strings.TrimSpace(defaultScheme) + "://" + rawProxy
	}
	parsedURL, err := url.Parse(rawProxy)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return ""
	}
	return parsedURL.String()
}
