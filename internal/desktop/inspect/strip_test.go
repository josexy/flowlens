package inspect

import (
	"testing"

	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
)

func TestDetailTabStripBorderSpansPane(t *testing.T) {
	f := newFixture(t, httpEntry(), &proxyservice.TrafficBodyView{})
	f.tester.SetSize(1400, 700)
	f.settle()
	strip, ok := f.tester.Find("tab-strip")
	if !ok {
		t.Fatal("no tab strip")
	}
	body, _ := f.tester.Find("Body")
	if body.X+body.W > strip.X+strip.W/2 {
		t.Skipf("tabs %v fill the strip %v", body, strip)
	}
	theme := f.settings.Theme().Config.Light
	image := f.tester.Image()
	y := int(strip.Y + strip.H - 1)
	for _, x := range []int{int(strip.X) + 1, int(body.X+body.W) + 20, int(strip.X+strip.W) - 2} {
		got := image.RGBAAt(x, y)
		if got.R != theme.Border.R || got.G != theme.Border.G || got.B != theme.Border.B {
			t.Fatalf("x=%d on the strip's bottom is %v, want border %v (strip %v)", x, got, theme.Border, strip)
		}
	}
}
