package logic

import (
	"regexp"
	"strconv"
	"strings"
)

// QueryItem is one raw query parameter.
type QueryItem struct {
	Key       string
	HasEquals bool
	Value     string
}

// ParsedHighlightedURL splits a URL for colored display.
type ParsedHighlightedURL struct {
	Scheme, Host, Path string
	QueryItems         []QueryItem
	HasQuery           bool
	Hash               string
}

// QueryParameterField is one decoded query parameter.
type QueryParameterField struct {
	Name, Value string
}

// ParsedURLQuery is a URL's query in raw and decoded form.
type ParsedURLQuery struct {
	RawQuery string
	Fields   []QueryParameterField
}

var schemeURLPattern = regexp.MustCompile(`^([a-zA-Z][a-zA-Z\d+\-.]*://)([^/?#]*)(.*)$`)

func parsePathQueryHash(input string) ParsedHighlightedURL {
	var parsed ParsedHighlightedURL
	before := input
	if index := strings.IndexByte(input, '#'); index >= 0 {
		before, parsed.Hash = input[:index], input[index:]
	}
	parsed.Path = before
	if index := strings.IndexByte(before, '?'); index >= 0 {
		parsed.Path = before[:index]
		parsed.HasQuery = true
		if query := before[index+1:]; query != "" {
			for item := range strings.SplitSeq(query, "&") {
				key, value, hasEquals := strings.Cut(item, "=")
				parsed.QueryItems = append(parsed.QueryItems, QueryItem{Key: key, HasEquals: hasEquals, Value: value})
			}
		}
	}
	return parsed
}

// ParseHighlightedURL splits a URL into scheme, host, path, query, and hash.
func ParseHighlightedURL(input string) ParsedHighlightedURL {
	value := strings.TrimSpace(input)
	if value == "" {
		return ParsedHighlightedURL{}
	}
	match := schemeURLPattern.FindStringSubmatch(value)
	if match == nil {
		return parsePathQueryHash(value)
	}
	parsed := parsePathQueryHash(match[3])
	parsed.Scheme, parsed.Host = match[1], match[2]
	return parsed
}

// ParseURLQuery returns a URL's raw query and its decoded parameters, keeping
// order and duplicates.
func ParseURLQuery(input string) ParsedURLQuery {
	items := ParseHighlightedURL(input).QueryItems
	raw := make([]string, len(items))
	for index, item := range items {
		raw[index] = item.Key
		if item.HasEquals {
			raw[index] += "=" + item.Value
		}
	}
	rawQuery := strings.Join(raw, "&")
	return ParsedURLQuery{RawQuery: rawQuery, Fields: DecodeQuery(rawQuery)}
}

// DecodeQuery decodes form-encoded text. Malformed escapes stay as written.
func DecodeQuery(query string) []QueryParameterField {
	var fields []QueryParameterField
	for part := range strings.SplitSeq(query, "&") {
		if part == "" {
			continue
		}
		name, value, _ := strings.Cut(part, "=")
		fields = append(fields, QueryParameterField{Name: decodeFormComponent(name), Value: decodeFormComponent(value)})
	}
	return fields
}

func decodeFormComponent(value string) string {
	value = strings.ReplaceAll(value, "+", " ")
	if !strings.Contains(value, "%") {
		return value
	}
	decoded := make([]byte, 0, len(value))
	for index := 0; index < len(value); index++ {
		if value[index] == '%' && index+2 < len(value) {
			if parsed, err := strconv.ParseUint(value[index+1:index+3], 16, 8); err == nil {
				decoded = append(decoded, byte(parsed))
				index += 2
				continue
			}
		}
		decoded = append(decoded, value[index])
	}
	return strings.ToValidUTF8(string(decoded), "�")
}
