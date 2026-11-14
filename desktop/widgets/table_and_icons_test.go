package widgets

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func TestKeyValueTableKeepsOrderDuplicatesAndCase(t *testing.T) {
	rows := []KeyValue{{"Set-Cookie", "a=1"}, {"set-cookie", "b=2"}, {"X-Empty", ""}, {"Set-Cookie", "c=3"}}
	config := nui.DefaultApp()
	tester := ui.NewTester(func(c *ui.Context) {
		theme := nui.App(c, config)
		KeyValueTable(c, theme, rows)
	}, 600, 300)
	var got []string
	for _, text := range tester.Texts() {
		if text != "" {
			got = append(got, text)
		}
	}
	want := []string{"Set-Cookie", "a=1", "set-cookie", "b=2", "X-Empty", "Set-Cookie", "c=3"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

type fakeLoads struct {
	calls    []string
	finishes map[string]func(int, bool)
}

func newFakeLoads() *fakeLoads { return &fakeLoads{finishes: map[string]func(int, bool){}} }

func (f *fakeLoads) load(key string, finish func(int, bool)) {
	f.calls = append(f.calls, key)
	f.finishes[key] = finish
}

func TestMissingIconIsRetriedAfterADelayAndThenAvailable(t *testing.T) {
	loads := newFakeLoads()
	cache := NewIconCache(256, loads.load)
	now := time.Unix(1000, 0)
	cache.now = func() time.Time { return now }

	if _, ok := cache.Get("a"); ok {
		t.Fatal("unexpected hit")
	}
	loads.finishes["a"](0, false)
	cache.Get("a")
	if len(loads.calls) != 1 {
		t.Fatalf("retried immediately: %v", loads.calls)
	}
	now = now.Add(iconRetryDelay)
	cache.Get("a")
	if len(loads.calls) != 2 {
		t.Fatalf("not retried: %v", loads.calls)
	}
	loads.finishes["a"](7, true)
	if got, ok := cache.Get("a"); !ok || got != 7 {
		t.Fatalf("got %d, %v", got, ok)
	}
}

func TestConcurrentGetsMergeIntoOneLoad(t *testing.T) {
	loads := newFakeLoads()
	cache := NewIconCache(256, loads.load)
	cache.Get("a")
	cache.Get("a")
	cache.Get("a")
	if len(loads.calls) != 1 {
		t.Fatalf("loads: %v", loads.calls)
	}
}

func TestLeastRecentlyUsedIconIsEvicted(t *testing.T) {
	loads := newFakeLoads()
	cache := NewIconCache(2, loads.load)
	for _, key := range []string{"a", "b"} {
		cache.Get(key)
		loads.finishes[key](1, true)
	}
	cache.Get("a") // b is now the oldest
	cache.Get("c")
	loads.finishes["c"](1, true)
	if cache.Len() != 2 {
		t.Fatalf("len %d", cache.Len())
	}
	if _, ok := cache.Get("a"); !ok {
		t.Fatal("recently used icon evicted")
	}
	if cache.index("b") >= 0 {
		t.Fatal("oldest icon kept")
	}
}

func TestCacheNeverExceedsItsSize(t *testing.T) {
	loads := newFakeLoads()
	cache := NewIconCache(processIconCacheSize, loads.load)
	for i := range processIconCacheSize + 10 {
		key := fmt.Sprint(i)
		cache.Get(key)
		loads.finishes[key](i, true)
	}
	if cache.Len() != processIconCacheSize {
		t.Fatalf("len %d", cache.Len())
	}
}

func TestClearDiscardsRunningLoads(t *testing.T) {
	loads := newFakeLoads()
	cache := NewIconCache(256, loads.load)
	cache.Get("a")
	finish := loads.finishes["a"]
	cache.Clear()
	finish(1, true)
	if cache.Len() != 0 {
		t.Fatal("stale load stored after Clear")
	}
	cache.Get("a")
	if len(loads.calls) != 2 {
		t.Fatalf("loads: %v", loads.calls)
	}
}

func TestDeleteForcesReload(t *testing.T) {
	loads := newFakeLoads()
	cache := NewIconCache(256, loads.load)
	cache.Get("a")
	loads.finishes["a"](1, true)
	cache.Delete("a")
	if _, ok := cache.Get("a"); ok {
		t.Fatal("deleted icon returned")
	}
	if len(loads.calls) != 2 {
		t.Fatalf("loads: %v", loads.calls)
	}
}

func TestInvalidProcessIconKeysAreNotLoaded(t *testing.T) {
	icons := &ProcessIcons{cache: NewIconCache(1, func(string, func(*ui.Bitmap, bool)) { t.Fatal("loaded an invalid key") })}
	if icons.Bitmap("not-a-key") != nil {
		t.Fatal("unexpected bitmap")
	}
}
