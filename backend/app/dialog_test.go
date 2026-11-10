package app

import (
	"slices"
	"testing"

	"github.com/josexy/flowlens/desktop/settings"
	"github.com/josexy/flowlens/locales"
)

func TestOpenDialogOptionsCarryMessageAndFilterName(t *testing.T) {
	options := openDialogOptions(settings.FilePicker{Title: "Key", Message: "Pick the key", FilterName: locales.SettingsPrivateKeyFileFilter(), Patterns: []string{"key", "pem"}}, false)
	if options.Title != "Key" || options.Message != "Pick the key" || options.Multiple {
		t.Fatalf("%+v", options)
	}
	if len(options.Filters) != 2 || options.Filters[0].Name != locales.SettingsPrivateKeyFileFilter() || !slices.Equal(options.Filters[0].Extensions, []string{"key", "pem"}) || options.Filters[1].Name != locales.SettingsAllFilesFilter() {
		t.Fatalf("%+v", options.Filters)
	}
	if plain := openDialogOptions(settings.FilePicker{Title: "Python"}, true); plain.Filters != nil || !plain.Multiple {
		t.Fatalf("%+v", plain)
	}
}
