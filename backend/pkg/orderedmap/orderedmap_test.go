package orderedmap

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"testing"
)

// ========================
// Construction
// ========================

func TestNew_Empty(t *testing.T) {
	om := New[string, int]()
	if om.Len() != 0 {
		t.Fatalf("expected Len=0, got %d", om.Len())
	}
	if om.Front() != nil {
		t.Fatal("expected Front=nil on empty map")
	}
	if om.Back() != nil {
		t.Fatal("expected Back=nil on empty map")
	}
}

func TestNewWithCapacity(t *testing.T) {
	om := NewWithCapacity[int, string](64)
	if om.Len() != 0 {
		t.Fatalf("expected Len=0, got %d", om.Len())
	}
}

// ========================
// Set
// ========================

func TestSet_Insert(t *testing.T) {
	om := New[string, int]()
	inserted := om.Set("a", 1)
	if !inserted {
		t.Fatal("Set should return true for new key")
	}
	if om.Len() != 1 {
		t.Fatalf("expected Len=1, got %d", om.Len())
	}
}

func TestSet_Update_KeepsPosition(t *testing.T) {
	om := New[string, int]()
	om.Set("a", 1)
	om.Set("b", 2)
	om.Set("c", 3)

	inserted := om.Set("b", 99)
	if inserted {
		t.Fatal("Set should return false for existing key")
	}

	// Value updated
	v, ok := om.Get("b")
	if !ok || v != 99 {
		t.Fatalf("expected b=99, got %v (ok=%v)", v, ok)
	}

	// Order unchanged: a, b, c
	keys := om.Keys()
	want := []string{"a", "b", "c"}
	if !slices.Equal(keys, want) {
		t.Fatalf("expected keys %v, got %v", want, keys)
	}
}

func TestSet_InsertionOrder(t *testing.T) {
	om := New[int, string]()
	for i := range 5 {
		om.Set(i, fmt.Sprintf("v%d", i))
	}
	keys := om.Keys()
	for i, k := range keys {
		if k != i {
			t.Fatalf("key[%d]: expected %d, got %d", i, i, k)
		}
	}
}

// ========================
// SetFront
// ========================

func TestSetFront_InsertAtHead(t *testing.T) {
	om := New[string, int]()
	om.Set("a", 1)
	om.Set("b", 2)

	inserted := om.SetFront("c", 3)
	if !inserted {
		t.Fatal("SetFront should return true for new key")
	}

	keys := om.Keys()
	want := []string{"c", "a", "b"}
	if !slices.Equal(keys, want) {
		t.Fatalf("expected keys %v, got %v", want, keys)
	}
}

func TestSetFront_UpdateMovesToHead(t *testing.T) {
	om := New[string, int]()
	om.Set("a", 1)
	om.Set("b", 2)
	om.Set("c", 3)

	inserted := om.SetFront("b", 99)
	if inserted {
		t.Fatal("SetFront should return false for existing key")
	}

	v, _ := om.Get("b")
	if v != 99 {
		t.Fatalf("expected b=99, got %d", v)
	}

	keys := om.Keys()
	want := []string{"b", "a", "c"}
	if !slices.Equal(keys, want) {
		t.Fatalf("expected keys %v, got %v", want, keys)
	}
}

// ========================
// Get
// ========================

func TestGet_ExistingKey(t *testing.T) {
	om := New[string, int]()
	om.Set("x", 42)

	v, ok := om.Get("x")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if v != 42 {
		t.Fatalf("expected 42, got %d", v)
	}
}

func TestGet_MissingKey(t *testing.T) {
	om := New[string, int]()
	v, ok := om.Get("missing")
	if ok {
		t.Fatal("expected ok=false for missing key")
	}
	if v != 0 {
		t.Fatalf("expected zero value, got %d", v)
	}
}

// ========================
// GetOrSet
// ========================

func TestGetOrSet_NewKey(t *testing.T) {
	om := New[string, int]()
	actual, loaded := om.GetOrSet("k", 7)
	if loaded {
		t.Fatal("expected loaded=false for new key")
	}
	if actual != 7 {
		t.Fatalf("expected 7, got %d", actual)
	}
	if om.Len() != 1 {
		t.Fatalf("expected Len=1, got %d", om.Len())
	}
}

func TestGetOrSet_ExistingKey(t *testing.T) {
	om := New[string, int]()
	om.Set("k", 5)

	actual, loaded := om.GetOrSet("k", 99)
	if !loaded {
		t.Fatal("expected loaded=true for existing key")
	}
	if actual != 5 {
		t.Fatalf("expected 5, got %d", actual)
	}
}

// ========================
// Delete
// ========================

func TestDelete_ExistingKey(t *testing.T) {
	om := New[string, int]()
	om.Set("a", 1)
	om.Set("b", 2)
	om.Set("c", 3)

	v, ok := om.Delete("b")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if v != 2 {
		t.Fatalf("expected 2, got %d", v)
	}
	if om.Len() != 2 {
		t.Fatalf("expected Len=2, got %d", om.Len())
	}

	keys := om.Keys()
	want := []string{"a", "c"}
	if !slices.Equal(keys, want) {
		t.Fatalf("expected keys %v, got %v", want, keys)
	}
}

func TestDelete_MissingKey(t *testing.T) {
	om := New[string, int]()
	v, ok := om.Delete("nope")
	if ok {
		t.Fatal("expected ok=false")
	}
	if v != 0 {
		t.Fatalf("expected zero value, got %d", v)
	}
}

func TestDelete_AllElements(t *testing.T) {
	om := New[int, int]()
	for i := range 5 {
		om.Set(i, i*10)
	}
	for i := range 5 {
		om.Delete(i)
	}
	if om.Len() != 0 {
		t.Fatalf("expected Len=0, got %d", om.Len())
	}
	if om.Front() != nil || om.Back() != nil {
		t.Fatal("expected Front and Back to be nil after deleting all")
	}
}

// ========================
// Contains
// ========================

func TestContains(t *testing.T) {
	om := New[string, int]()
	om.Set("a", 1)

	if !om.Contains("a") {
		t.Fatal("expected Contains=true for existing key")
	}
	if om.Contains("b") {
		t.Fatal("expected Contains=false for missing key")
	}

	om.Delete("a")
	if om.Contains("a") {
		t.Fatal("expected Contains=false after deletion")
	}
}

// ========================
// Front / Back / Next
// ========================

func TestFront_Back(t *testing.T) {
	om := New[string, int]()
	om.Set("first", 1)
	om.Set("second", 2)
	om.Set("third", 3)

	front := om.Front()
	if front == nil || front.Key() != "first" {
		t.Fatalf("expected front key=first, got %v", front)
	}
	back := om.Back()
	if back == nil || back.Key() != "third" {
		t.Fatalf("expected back key=third, got %v", back)
	}
}

func TestNext_Traversal(t *testing.T) {
	om := New[int, int]()
	for i := range 4 {
		om.Set(i, i)
	}

	var keys []int
	for e := om.Front(); e != nil; e = om.Next(e) {
		keys = append(keys, e.Key())
	}
	want := []int{0, 1, 2, 3}
	if !slices.Equal(keys, want) {
		t.Fatalf("expected %v, got %v", want, keys)
	}
}

// ========================
// MoveToFront / MoveToBack
// ========================

func TestMoveToFront(t *testing.T) {
	om := New[string, int]()
	om.Set("a", 1)
	om.Set("b", 2)
	om.Set("c", 3)

	moved := om.MoveToFront("c")
	if !moved {
		t.Fatal("expected MoveToFront=true")
	}

	keys := om.Keys()
	want := []string{"c", "a", "b"}
	if !slices.Equal(keys, want) {
		t.Fatalf("expected %v, got %v", want, keys)
	}
}

func TestMoveToFront_Missing(t *testing.T) {
	om := New[string, int]()
	if om.MoveToFront("x") {
		t.Fatal("expected MoveToFront=false for missing key")
	}
}

func TestMoveToBack(t *testing.T) {
	om := New[string, int]()
	om.Set("a", 1)
	om.Set("b", 2)
	om.Set("c", 3)

	moved := om.MoveToBack("a")
	if !moved {
		t.Fatal("expected MoveToBack=true")
	}

	keys := om.Keys()
	want := []string{"b", "c", "a"}
	if !slices.Equal(keys, want) {
		t.Fatalf("expected %v, got %v", want, keys)
	}
}

func TestMoveToBack_Missing(t *testing.T) {
	om := New[string, int]()
	if om.MoveToBack("x") {
		t.Fatal("expected MoveToBack=false for missing key")
	}
}

// ========================
// Clear
// ========================

func TestClear(t *testing.T) {
	om := New[string, int]()
	om.Set("a", 1)
	om.Set("b", 2)
	om.Clear()

	if om.Len() != 0 {
		t.Fatalf("expected Len=0 after Clear, got %d", om.Len())
	}
	if om.Front() != nil || om.Back() != nil {
		t.Fatal("expected Front/Back=nil after Clear")
	}
	if om.Contains("a") || om.Contains("b") {
		t.Fatal("expected no keys after Clear")
	}

	// Reuse after Clear
	om.Set("c", 3)
	if om.Len() != 1 {
		t.Fatalf("expected Len=1 after re-insert, got %d", om.Len())
	}
}

// ========================
// Keys / Values
// ========================

func TestKeys_Values_Order(t *testing.T) {
	om := New[string, int]()
	om.Set("x", 10)
	om.Set("y", 20)
	om.Set("z", 30)

	if !slices.Equal(om.Keys(), []string{"x", "y", "z"}) {
		t.Fatalf("unexpected keys: %v", om.Keys())
	}
	if !slices.Equal(om.Values(), []int{10, 20, 30}) {
		t.Fatalf("unexpected values: %v", om.Values())
	}
}

func TestKeys_Empty(t *testing.T) {
	om := New[string, int]()
	if len(om.Keys()) != 0 {
		t.Fatal("expected empty keys slice")
	}
}

// ========================
// All / Backward iterators
// ========================

func TestAll_ForwardIteration(t *testing.T) {
	om := New[int, int]()
	for i := range 5 {
		om.Set(i, i*2)
	}

	var keys, vals []int
	for k, v := range om.All() {
		keys = append(keys, k)
		vals = append(vals, v)
	}

	if !slices.Equal(keys, []int{0, 1, 2, 3, 4}) {
		t.Fatalf("unexpected keys: %v", keys)
	}
	if !slices.Equal(vals, []int{0, 2, 4, 6, 8}) {
		t.Fatalf("unexpected vals: %v", vals)
	}
}

func TestAll_EarlyBreak(t *testing.T) {
	om := New[int, int]()
	for i := range 5 {
		om.Set(i, i)
	}

	var count int
	for range om.All() {
		count++
		if count == 2 {
			break
		}
	}
	if count != 2 {
		t.Fatalf("expected 2 iterations, got %d", count)
	}
}

func TestBackward_ReverseOrder(t *testing.T) {
	om := New[int, int]()
	for i := range 4 {
		om.Set(i, i)
	}

	var keys []int
	for k := range om.Backward() {
		keys = append(keys, k)
	}
	if !slices.Equal(keys, []int{3, 2, 1, 0}) {
		t.Fatalf("expected reverse order, got %v", keys)
	}
}

func TestBackward_EarlyBreak(t *testing.T) {
	om := New[int, int]()
	for i := range 5 {
		om.Set(i, i)
	}

	var count int
	for range om.Backward() {
		count++
		if count == 3 {
			break
		}
	}
	if count != 3 {
		t.Fatalf("expected 3 iterations, got %d", count)
	}
}

// ========================
// ForEach
// ========================

func TestForEach_AllElements(t *testing.T) {
	om := New[string, int]()
	om.Set("a", 1)
	om.Set("b", 2)
	om.Set("c", 3)

	var result []string
	om.ForEach(func(k string, v int) bool {
		result = append(result, fmt.Sprintf("%s=%d", k, v))
		return true
	})
	want := []string{"a=1", "b=2", "c=3"}
	if !slices.Equal(result, want) {
		t.Fatalf("expected %v, got %v", want, result)
	}
}

func TestForEach_EarlyStop(t *testing.T) {
	om := New[int, int]()
	for i := range 5 {
		om.Set(i, i)
	}

	var count int
	om.ForEach(func(k, v int) bool {
		count++
		return count < 3
	})
	if count != 3 {
		t.Fatalf("expected 3 calls, got %d", count)
	}
}

// ========================
// Filter
// ========================

func TestFilter(t *testing.T) {
	om := New[int, int]()
	for i := range 6 {
		om.Set(i, i)
	}

	even := om.Filter(func(k, v int) bool { return v%2 == 0 })

	if even.Len() != 3 {
		t.Fatalf("expected 3 elements, got %d", even.Len())
	}
	if !slices.Equal(even.Keys(), []int{0, 2, 4}) {
		t.Fatalf("unexpected keys: %v", even.Keys())
	}
}

func TestFilter_NoMatch(t *testing.T) {
	om := New[int, int]()
	om.Set(1, 1)
	result := om.Filter(func(k, v int) bool { return false })
	if result.Len() != 0 {
		t.Fatalf("expected empty result, got %d", result.Len())
	}
}

// ========================
// JSON serialization
// ========================

func TestMarshalJSON_OrderPreserved(t *testing.T) {
	om := New[string, int]()
	om.Set("one", 1)
	om.Set("two", 2)
	om.Set("three", 3)

	data, err := json.Marshal(om)
	if err != nil {
		t.Fatalf("MarshalJSON error: %v", err)
	}

	want := `{"one":1,"two":2,"three":3}`
	if string(data) != want {
		t.Fatalf("expected %s, got %s", want, string(data))
	}
}

func TestMarshalJSON_Empty(t *testing.T) {
	om := New[string, int]()
	data, err := json.Marshal(om)
	if err != nil {
		t.Fatalf("MarshalJSON error: %v", err)
	}
	if string(data) != "{}" {
		t.Fatalf("expected {}, got %s", string(data))
	}
}

func TestUnmarshalJSON_OrderPreserved(t *testing.T) {
	// Use explicit JSON with known order
	raw := []byte(`{"alpha":10,"beta":20,"gamma":30}`)

	om := New[string, int]()
	if err := json.Unmarshal(raw, om); err != nil {
		t.Fatalf("UnmarshalJSON error: %v", err)
	}

	if om.Len() != 3 {
		t.Fatalf("expected 3, got %d", om.Len())
	}
	if !slices.Equal(om.Keys(), []string{"alpha", "beta", "gamma"}) {
		t.Fatalf("unexpected keys: %v", om.Keys())
	}
	if !slices.Equal(om.Values(), []int{10, 20, 30}) {
		t.Fatalf("unexpected values: %v", om.Values())
	}
}

func TestMarshalUnmarshalRoundtrip(t *testing.T) {
	om := New[string, string]()
	om.Set("key1", "val1")
	om.Set("key2", "val2")

	data, err := json.Marshal(om)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	om2 := New[string, string]()
	if err := json.Unmarshal(data, om2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !slices.Equal(om.Keys(), om2.Keys()) {
		t.Fatalf("keys mismatch: %v vs %v", om.Keys(), om2.Keys())
	}
	if !slices.Equal(om.Values(), om2.Values()) {
		t.Fatalf("values mismatch: %v vs %v", om.Values(), om2.Values())
	}
}

// ========================
// String
// ========================

func TestString(t *testing.T) {
	om := New[string, int]()
	om.Set("a", 1)
	om.Set("b", 2)

	s := om.String()
	want := "OrderedMap{a:1, b:2}"
	if s != want {
		t.Fatalf("expected %q, got %q", want, s)
	}
}

func TestString_Empty(t *testing.T) {
	om := New[string, int]()
	want := "OrderedMap{}"
	if om.String() != want {
		t.Fatalf("expected %q, got %q", want, om.String())
	}
}

// ========================
// Concurrent safety
// ========================

func TestConcurrentReadWrite(t *testing.T) {
	om := New[int, int]()
	const goroutines = 50
	const ops = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 3)

	// Writers
	for g := range goroutines {
		go func(g int) {
			defer wg.Done()
			for i := range ops {
				om.Set(g*ops+i, i)
			}
		}(g)
	}

	// Readers
	for range goroutines {
		go func() {
			defer wg.Done()
			for range ops {
				_ = om.Len()
				_ = om.Keys()
			}
		}()
	}

	// Deleters
	for g := range goroutines {
		go func(g int) {
			defer wg.Done()
			for i := range ops {
				om.Delete(g*ops + i)
			}
		}(g)
	}

	wg.Wait()
}

func TestConcurrentForEach(t *testing.T) {
	om := New[int, int]()
	for i := range 100 {
		om.Set(i, i)
	}

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			om.ForEach(func(k, v int) bool { return true })
		})
	}
	wg.Wait()
}

func TestTailValuesReturnsLatestInsertionWindow(t *testing.T) {
	om := New[int, string]()
	om.Set(1, "one")
	om.Set(2, "two")
	om.Set(3, "three")
	om.Set(4, "four")

	if got := om.TailValues(2); !slices.Equal(got, []string{"three", "four"}) {
		t.Fatalf("TailValues(2) = %v, want [three four]", got)
	}
	if got := om.TailValues(10); !slices.Equal(got, []string{"one", "two", "three", "four"}) {
		t.Fatalf("TailValues(10) = %v, want all values", got)
	}
	if got := om.TailValues(0); len(got) != 0 {
		t.Fatalf("TailValues(0) = %v, want empty", got)
	}
}
