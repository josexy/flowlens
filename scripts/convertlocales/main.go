// Command convertlocales flattens the previous interface's nested locale files
// into the ARB catalogs MyGo generates message functions from.
//
//	go run ./scripts/convertlocales <source-dir> <catalog-dir>
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

var placeholderPattern = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// renamed replaces messages that named the previous framework.
var renamed = map[string]string{"status.environment_wails": "status.environment_mygo"}

var replacedValues = map[string]string{"status.environment_mygo": "MyGo"}

func main() {
	if len(os.Args) != 3 {
		log.Fatal("usage: convertlocales <source-dir> <catalog-dir>")
	}
	source, target := os.Args[1], os.Args[2]
	english, err := convert(filepath.Join(source, "en.json"), filepath.Join(target, "en.json"), "en", true)
	if err != nil {
		log.Fatal(err)
	}
	chinese, err := convert(filepath.Join(source, "zh.json"), filepath.Join(target, "zh-Hans.json"), "zh-Hans", false)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("en: %d messages, zh-Hans: %d messages\n", english, chinese)
}

func convert(path, target, locale string, describe bool) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var nested map[string]any
	if err := json.Unmarshal(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")), &nested); err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}
	flat := map[string]string{}
	if err := flatten(nested, "", flat); err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}

	catalog := map[string]any{"@@locale": locale}
	origin := map[string]string{}
	for dotted, message := range flat {
		key := catalogKey(dotted)
		if other, taken := origin[key]; taken {
			return 0, fmt.Errorf("%s: %q and %q both become %q", path, other, dotted, key)
		}
		origin[key] = dotted
		catalog[key] = message
		if describe {
			if placeholders := placeholderMetadata(message); placeholders != nil {
				catalog["@"+key] = map[string]any{"placeholders": placeholders}
			}
		}
	}

	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(catalog); err != nil {
		return 0, err
	}
	if err := os.WriteFile(target, out.Bytes(), 0o644); err != nil {
		return 0, err
	}
	return len(flat), nil
}

func flatten(node map[string]any, prefix string, into map[string]string) error {
	for key, value := range node {
		dotted := key
		if prefix != "" {
			dotted = prefix + "." + key
		}
		switch typed := value.(type) {
		case map[string]any:
			if err := flatten(typed, dotted, into); err != nil {
				return err
			}
		case string:
			if name, ok := renamed[dotted]; ok {
				dotted = name
				if replacement, ok := replacedValues[dotted]; ok {
					typed = replacement
				}
			}
			into[dotted] = typed
		default:
			return fmt.Errorf("%s: unexpected value %T", dotted, value)
		}
	}
	return nil
}

// catalogKey turns a dotted, snake-case key into a camel-case catalog key.
func catalogKey(dotted string) string {
	var builder strings.Builder
	upper := false
	for _, r := range dotted {
		if r == '.' || r == '_' || r == ' ' || r == '-' {
			upper = builder.Len() > 0
			continue
		}
		if upper {
			r = unicode.ToUpper(r)
			upper = false
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func placeholderMetadata(message string) map[string]any {
	matches := placeholderPattern.FindAllStringSubmatch(message, -1)
	if len(matches) == 0 {
		return nil
	}
	placeholders := map[string]any{}
	for _, match := range matches {
		placeholders[match[1]] = map[string]any{"type": "String"}
	}
	return placeholders
}
