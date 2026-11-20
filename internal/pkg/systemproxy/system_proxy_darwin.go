package systemproxy

func fromPlatform() string {
	api, err := loadDarwinProxy()
	if err != nil {
		return ""
	}
	settings := api.copyProxies(0)
	if settings == 0 {
		return ""
	}
	defer api.release(settings)
	return api.proxyURL(settings)
}

func (a *darwinProxyAPI) proxyURL(settings uintptr) string {
	if settings == 0 || a.typeID(settings) != a.dictType() {
		return ""
	}
	candidates := make([]systemProxyCandidate, 0, 3)
	for _, kind := range []string{"HTTP", "HTTPS", "SOCKS"} {
		prefix := "kSCPropNetProxies" + kind
		enabled := a.enabled(a.dictGet(settings, a.keys[prefix+"Enable"]))
		host := a.text(a.dictGet(settings, a.keys[prefix+"Proxy"]))
		port := a.number(a.dictGet(settings, a.keys[prefix+"Port"]))
		scheme := "http"
		if kind == "SOCKS" {
			scheme = "socks5"
		}
		candidates = append(candidates, systemProxyCandidate{Enabled: enabled, Host: host, Port: int(port), DefaultScheme: scheme})
	}
	return proxyURLFromSystemProxyCandidates(candidates)
}

func (a *darwinProxyAPI) number(value uintptr) int32 {
	var result int32
	if value != 0 && a.typeID(value) == a.numberType() && a.numberValue(value, cfInt32, &result) != 0 {
		return result
	}
	return 0
}

func (a *darwinProxyAPI) enabled(value uintptr) bool {
	if value == 0 {
		return false
	}
	if a.typeID(value) == a.booleanType() {
		return a.boolValue(value) != 0
	}
	return a.number(value) != 0
}
