package pythonpluginservice

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

func matchPlugins(plugins []*Plugin, method, candidateURL string) ([]*Plugin, error) {
	method = strings.ToUpper(strings.TrimSpace(method))
	candidateURL = normalizeMatchURL(candidateURL)
	matched := make([]*Plugin, 0)
	for _, plugin := range plugins {
		if plugin == nil || !plugin.Enabled || plugin.ValidationStatus != ValidationStatusValid || strings.TrimSpace(plugin.ActiveRevision) == "" {
			continue
		}
		pluginMatched := false
		for _, rule := range plugin.Rules {
			if rule == nil || !rule.Enabled || rule.Method != "*" && rule.Method != method {
				continue
			}
			matcher, err := compileURLWildcard(rule.URLPattern)
			if err != nil {
				return nil, fmt.Errorf("compile URL rule %q for plugin %q: %w", rule.ID, plugin.ID, err)
			}
			if matcher.MatchString(candidateURL) {
				pluginMatched = true
				break
			}
		}
		if pluginMatched {
			matched = append(matched, plugin)
		}
	}
	return matched, nil
}

func compileURLWildcard(pattern string) (*regexp.Regexp, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, fmt.Errorf("URL wildcard cannot be empty")
	}
	if !utf8.ValidString(pattern) {
		return nil, fmt.Errorf("URL wildcard must be valid UTF-8")
	}
	pattern = normalizeMatchPattern(pattern)
	var expression strings.Builder
	expression.WriteByte('^')
	for _, value := range pattern {
		switch value {
		case '*':
			expression.WriteString(".*")
		case '?':
			expression.WriteByte('.')
		default:
			expression.WriteString(regexp.QuoteMeta(string(value)))
		}
	}
	expression.WriteByte('$')
	matcher, err := regexp.Compile(expression.String())
	if err != nil {
		return nil, fmt.Errorf("compile URL wildcard: %w", err)
	}
	return matcher, nil
}

func normalizeMatchURL(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return strings.TrimSpace(value)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	return parsed.String()
}

func normalizeMatchPattern(pattern string) string {
	schemeSeparator := strings.Index(pattern, "://")
	if schemeSeparator < 0 {
		return pattern
	}
	authorityEnd := strings.IndexAny(pattern[schemeSeparator+3:], "/?#")
	if authorityEnd < 0 {
		return strings.ToLower(pattern)
	}
	authorityEnd += schemeSeparator + 3
	return strings.ToLower(pattern[:authorityEnd]) + pattern[authorityEnd:]
}
