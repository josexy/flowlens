// Package locale maps the saved language setting to the languages the
// interface is shown in.
package locale

import "github.com/egoist/mygo"

// Values of the saved language setting.
const (
	Chinese = "zh"
	English = "en"
)

// Language tags of the message catalogs.
const (
	ChineseTag = "zh-Hans"
	EnglishTag = "en"
)

// Normalize returns the saved language if it is valid, and Chinese otherwise.
func Normalize(language string) string {
	if language == English {
		return English
	}
	return Chinese
}

// Tag returns the catalog tag of a saved language.
func Tag(language string) string {
	if Normalize(language) == English {
		return EnglishTag
	}
	return ChineseTag
}

// Apply shows the interface in a saved language.
func Apply(language string) {
	mygo.App.SetLocale(Tag(language))
}
