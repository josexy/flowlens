package rewriteservice

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

type compiledRule struct {
	rule    Rule
	matcher *urlMatcher
	body    *bodyRegex
}

// Snapshot is immutable after publication. Match returns detached rule values.
type Snapshot struct {
	state State
	rules []compiledRule
}
type Match struct {
	Rule     Rule
	captures []string
	body     *bodyRegex
}

func cloneRule(r Rule) Rule {
	r.Action.Headers = slices.Clone(r.Action.Headers)
	r.Action.Query = slices.Clone(r.Action.Query)
	return r
}
func cloneState(s State) State {
	s.Rules = slices.Clone(s.Rules)
	for i := range s.Rules {
		s.Rules[i] = cloneRule(s.Rules[i])
	}
	return s
}
func (s *Snapshot) Match(method, rawURL string) []Match {
	if s == nil || !s.state.Enabled || method == "CONNECT" {
		return nil
	}
	var matches []Match
	for _, c := range s.rules {
		if !c.rule.Enabled || (c.rule.Method != "ALL" && c.rule.Method != method) {
			continue
		}
		captures := c.matcher.FindStringSubmatch(rawURL)
		if captures != nil {
			matches = append(matches, Match{cloneRule(c.rule), captures[1:], c.body})
		}
	}
	return matches
}
func (m Match) TargetURL() (string, error) {
	target, err := expandTarget(m.Rule.Action.TargetURL, m.captures)
	if err != nil {
		return "", err
	}
	if err := validateTarget(target); err != nil {
		return "", err
	}
	return target, nil
}

// urlMatcher compares literals at their actual input position. A wildcard may
// cross authority/path boundaries, so regex-wide case folding would also fold
// path characters accidentally.
type urlMatcher struct{ literals []string }

func (m *urlMatcher) NumSubexp() int { return len(m.literals) - 1 }
func compilePattern(pattern string) (*urlMatcher, error) {
	if pattern == "" || len(pattern) > 16384 || !utf8.ValidString(pattern) {
		return nil, errors.New("URL pattern must contain 1–16384 UTF-8 bytes")
	}
	m := &urlMatcher{}
	var literal strings.Builder
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '\\':
			if i+1 >= len(pattern) || pattern[i+1] != '*' {
				return nil, errors.New("URL patterns only support an escaped literal star")
			}
			i++
			literal.WriteByte('*')
		case '*':
			m.literals = append(m.literals, literal.String())
			literal.Reset()
		default:
			literal.WriteByte(pattern[i])
		}
	}
	m.literals = append(m.literals, literal.String())
	return m, nil
}
func (m *urlMatcher) FindStringSubmatch(raw string) []string {
	foldEnd := 0
	if scheme := strings.Index(raw, "://"); scheme >= 0 {
		foldEnd = len(raw)
		if offset := strings.IndexAny(raw[scheme+3:], "/?#"); offset >= 0 {
			foldEnd = scheme + 3 + offset
		}
	}
	equalAt := func(literal string, pos int) bool {
		if pos < 0 || pos+len(literal) > len(raw) {
			return false
		}
		folded := max(0, min(len(literal), foldEnd-pos))
		return strings.EqualFold(literal[:folded], raw[pos:pos+folded]) && literal[folded:] == raw[pos+folded:pos+len(literal)]
	}
	if !equalAt(m.literals[0], 0) {
		return nil
	}
	cursor := len(m.literals[0])
	result := []string{raw}
	if len(m.literals) == 1 {
		if cursor == len(raw) {
			return result
		}
		return nil
	}
	for i := 1; i < len(m.literals); i++ {
		literal := m.literals[i]
		start := -1
		if i == len(m.literals)-1 {
			candidate := len(raw) - len(literal)
			if candidate >= cursor && equalAt(literal, candidate) {
				start = candidate
			}
		} else {
			// Only the short scheme/authority prefix needs folded comparisons;
			// path/query literal runs use the standard linear substring search.
			for cursorCandidate := cursor; cursorCandidate < foldEnd; cursorCandidate++ {
				if equalAt(literal, cursorCandidate) {
					start = cursorCandidate
					break
				}
			}
			if start < 0 {
				from := max(cursor, foldEnd)
				if from <= len(raw) {
					if index := strings.Index(raw[from:], literal); index >= 0 {
						start = from + index
					}
				}
			}
		}
		if start < 0 {
			return nil
		}
		result = append(result, raw[cursor:start])
		cursor = start + len(literal)
	}
	return result
}

func expandTarget(template string, captures []string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(template); {
		if template[i] != '$' {
			b.WriteByte(template[i])
			i++
			continue
		}
		if i+1 < len(template) && template[i+1] == '$' {
			b.WriteByte('$')
			i += 2
			continue
		}
		if i+1 >= len(template) || template[i+1] != '{' {
			return "", errors.New("target captures must use ${1}; use $$ for a literal dollar")
		}
		end := strings.IndexByte(template[i+2:], '}')
		if end < 0 {
			return "", errors.New("unclosed target capture")
		}
		end += i + 2
		n, err := strconv.Atoi(template[i+2 : end])
		if err != nil || n < 1 || n > len(captures) {
			return "", errors.New("target capture is outside URL pattern capture range")
		}
		b.WriteString(captures[n-1])
		i = end + 1
	}
	return b.String(), nil
}

// Validate fixed components strictly, while checking capture-dependent scheme,
// host and port components only after expansion. A complete component stand-in
// is necessary: inserting a number inside a port or IPv6 literal is not valid.
func validateTargetTemplate(template string, count int) error {
	const marker = "\x01"
	if strings.ContainsAny(template, "\x00\x01\r\n\t ") {
		return errors.New("target URL contains control characters or whitespace")
	}
	captures := make([]string, count)
	for i := range captures {
		captures[i] = marker
	}
	masked, err := expandTarget(template, captures)
	if err != nil {
		return err
	}
	if strings.Contains(masked, "#") {
		return errors.New("target URL must not contain a fragment")
	}
	scheme, rest, ok := strings.Cut(masked, "://")
	if !ok {
		if !strings.HasPrefix(masked, marker) {
			return errors.New("target must be an absolute HTTP/HTTPS URL")
		}
		return validateTarget("https://rewrite.invalid" + strings.ReplaceAll(strings.TrimPrefix(masked, marker), marker, "00"))
	}
	if strings.Contains(scheme, marker) {
		scheme = "https"
	}
	end := strings.IndexAny(rest, "/?#")
	if end < 0 {
		end = len(rest)
	}
	authority, path := rest[:end], rest[end:]
	if strings.Contains(authority, "@") {
		return errors.New("target URL must not contain credentials")
	}
	host, port := authority, ""
	hasPort := false
	if strings.HasPrefix(authority, "[") {
		close := strings.IndexByte(authority, ']')
		if close < 0 {
			return errors.New("invalid target IPv6 authority")
		}
		host = authority[:close+1]
		if strings.Contains(host, marker) {
			host = "[::1]"
		}
		if close+1 < len(authority) {
			if authority[close+1] != ':' {
				return errors.New("invalid target authority")
			}
			port = authority[close+2:]
			hasPort = true
		}
	} else if colon := strings.LastIndexByte(authority, ':'); colon >= 0 {
		host = authority[:colon]
		port = authority[colon+1:]
		hasPort = true
	}
	if strings.Contains(host, marker) {
		host = "rewrite.invalid"
	}
	if strings.Contains(port, marker) {
		port = "443"
	}
	authority = host
	if hasPort {
		authority += ":" + port
	}
	return validateTarget(scheme + "://" + authority + strings.ReplaceAll(path, marker, "00"))
}

func validateTarget(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" || strings.ContainsAny(u.Host, " \t\r\n") {
		return errors.New("target must be an absolute HTTP/HTTPS URL without credentials or fragment")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return errors.New("invalid target port")
		}
	}
	return nil
}
func compileRule(r Rule) (compiledRule, error) {
	c := compiledRule{rule: cloneRule(r)}
	if r.Action.Version != ConfigVersion {
		return c, fmt.Errorf("unsupported action version %d", r.Action.Version)
	}
	if strings.TrimSpace(r.Name) == "" || len(r.Name) > 256 {
		return c, errors.New("rule name must contain 1–256 bytes")
	}
	switch r.Method {
	case "ALL", "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "TRACE":
	default:
		return c, errors.New("unsupported HTTP method")
	}
	var err error
	c.matcher, err = compilePattern(r.URLPattern)
	if err != nil {
		return c, err
	}
	switch r.Action.Type {
	case ActionRedirect:
		if r.Action.HostPolicy != "target" && r.Action.HostPolicy != "preserve" {
			return c, errors.New("invalid Host policy")
		}
		if err := validateTargetTemplate(r.Action.TargetURL, c.matcher.NumSubexp()); err != nil {
			return c, err
		}

	case ActionRequest, ActionResponse:
		for _, op := range r.Action.Headers {
			if err := validateOperation(op, true, r.Action.Type == ActionRequest); err != nil {
				return c, err
			}
		}
		for _, op := range r.Action.Query {
			if err := validateOperation(op, false, true); err != nil {
				return c, err
			}
		}
		if r.Action.Type == ActionResponse && len(r.Action.Query) != 0 {
			return c, errors.New("response actions cannot modify query")
		}
		switch r.Action.Body.Mode {
		case "none":
		case "replace":
			if len(r.Action.Body.Text) > MaxBodyBytes || !utf8.ValidString(r.Action.Body.Text) {
				return c, errors.New("replacement body must be UTF-8 and at most 8 MiB")
			}
		case "regex":
			if len(r.Action.Body.Pattern) > 16384 || len(r.Action.Body.Replacement) > MaxBodyBytes || !utf8.ValidString(r.Action.Body.Replacement) {
				return c, errors.New("body regular expression or replacement is too large or replacement is not UTF-8")
			}
			c.body, err = compileBodyRegex(r.Action.Body.Pattern)
			if err != nil {
				return c, fmt.Errorf("body regular expression: %w", err)
			}
		default:
			return c, errors.New("invalid body mode")
		}
	default:
		return c, errors.New("unsupported rewrite action")
	}
	return c, nil
}
func validateOperation(op FieldOperation, header, request bool) error {
	if op.Operation != "add" && op.Operation != "set" && op.Operation != "delete" {
		return errors.New("invalid field operation")
	}
	if op.Name == "" || !utf8.ValidString(op.Name+op.Value) {
		return errors.New("field name is required and fields must be UTF-8")
	}
	if !header {
		return nil
	}
	for _, b := range []byte(op.Name) {
		valid := b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(b))
		if !valid {
			return errors.New("invalid header name")
		}
	}
	for _, b := range []byte(op.Value) {
		if b == 127 || b < 32 && b != '\t' {
			return errors.New("invalid header value")
		}
	}
	switch strings.ToLower(op.Name) {
	case "host":
		if !request || op.Operation != "set" || strings.TrimSpace(op.Value) == "" || strings.ContainsAny(op.Value, "/?#@ \t") {
			return errors.New("host only supports setting a nonempty request authority")
		}
		if err := validateTarget("http://" + op.Value + "/"); err != nil {
			return errors.New("invalid Host authority")
		}
	case "content-length", "transfer-encoding", "content-encoding", "connection", "proxy-connection", "keep-alive", "upgrade", "te", "trailer", "proxy-authenticate", "proxy-authorization":
		return fmt.Errorf("header %s is managed by the transport", op.Name)
	}
	return nil
}
