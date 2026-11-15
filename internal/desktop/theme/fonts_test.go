package theme

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestRegisterFontsMakesBundledFamiliesAvailable(t *testing.T) {
	if err := RegisterFonts(); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{DefaultFontFamily: false, DefaultCodeFamily: false}
	for _, family := range ui.FontFamilies() {
		if _, ok := want[family.Name]; ok && family.Registered {
			want[family.Name] = true
		}
	}
	for family, found := range want {
		if !found {
			t.Errorf("family %q not registered", family)
		}
	}
}
