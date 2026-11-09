package host_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/josexy/flowlens/backend/host"
	"github.com/josexy/flowlens/backend/host/hosttest"
)

func TestBusDeliversInSubscriptionOrder(t *testing.T) {
	bus := host.NewBus()
	var got []string
	bus.On("a", func(host.Event) { got = append(got, "first") })
	bus.On("a", func(host.Event) { got = append(got, "second") })
	bus.On("b", func(host.Event) { got = append(got, "other") })
	if !bus.Emit("a") {
		t.Fatal("Emit reported no listeners")
	}
	if want := []string{"first", "second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("delivery = %v, want %v", got, want)
	}
}

func TestBusKeepsOnePublishersOrder(t *testing.T) {
	bus := host.NewBus()
	var got []int
	bus.On("n", func(e host.Event) { got = append(got, e.Data.(int)) })
	for i := range 100 {
		bus.Emit("n", i)
	}
	for i, v := range got {
		if v != i {
			t.Fatalf("got[%d] = %d", i, v)
		}
	}
	if len(got) != 100 {
		t.Fatalf("delivered %d events", len(got))
	}
}

func TestBusUnsubscribe(t *testing.T) {
	bus := host.NewBus()
	calls := 0
	off := bus.On("a", func(host.Event) { calls++ })
	bus.Emit("a")
	off()
	off() // safe to repeat
	if bus.Emit("a") {
		t.Fatal("Emit reported a listener after unsubscribe")
	}
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestBusUnsubscribeDuringDelivery(t *testing.T) {
	bus := host.NewBus()
	var off func()
	first, second := 0, 0
	off = bus.On("a", func(host.Event) { first++; off() })
	bus.On("a", func(host.Event) { second++ })
	bus.Emit("a")
	bus.Emit("a")
	if first != 1 || second != 2 {
		t.Fatalf("first = %d, second = %d", first, second)
	}
}

func TestBusDataFolding(t *testing.T) {
	bus := host.NewBus()
	var got any
	bus.On("a", func(e host.Event) { got = e.Data })
	bus.Emit("a")
	if got != nil {
		t.Fatalf("no data = %v", got)
	}
	bus.Emit("a", "x")
	if got != "x" {
		t.Fatalf("one value = %v", got)
	}
	bus.Emit("a", "x", 2)
	if !reflect.DeepEqual(got, []any{"x", 2}) {
		t.Fatalf("several values = %v", got)
	}
}

func TestBusConcurrentUse(t *testing.T) {
	bus := host.NewBus()
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				off := bus.On("a", func(host.Event) {})
				bus.Emit("a", 1)
				off()
			}
		}()
	}
	wg.Wait()
}

func TestRecordingBus(t *testing.T) {
	bus := hosttest.NewRecordingBus()
	delivered := 0
	bus.On("a", func(host.Event) { delivered++ })
	bus.Emit("a", 1)
	bus.Emit("b", 2)
	bus.Emit("a", 3)
	if delivered != 2 || len(bus.Events()) != 3 {
		t.Fatalf("delivered = %d, events = %v", delivered, bus.Events())
	}
	named := bus.Named("a")
	if len(named) != 2 || named[0].Data != 1 || named[1].Data != 3 {
		t.Fatalf("named = %v", named)
	}
}

type recorder struct {
	name     string
	log      *[]string
	startErr error
	stopErr  error
	ctx      context.Context
}

func (r *recorder) ServiceStartup(ctx context.Context) error {
	r.ctx = ctx
	*r.log = append(*r.log, "start "+r.name)
	return r.startErr
}

func (r *recorder) ServiceShutdown() error {
	state := "live"
	if r.ctx != nil && r.ctx.Err() != nil {
		state = "canceled"
	}
	*r.log = append(*r.log, "stop "+r.name+" "+state)
	return r.stopErr
}

func TestLifecycleOrder(t *testing.T) {
	var log []string
	l := host.NewLifecycle(context.Background())
	l.Register(&recorder{name: "a", log: &log}, struct{}{}, &recorder{name: "b", log: &log})
	if err := l.Start(); err != nil {
		t.Fatal(err)
	}
	if err := l.Stop(); err != nil {
		t.Fatal(err)
	}
	want := []string{"start a", "start b", "stop b canceled", "stop a canceled"}
	if !reflect.DeepEqual(log, want) {
		t.Fatalf("log = %v, want %v", log, want)
	}
}

func TestLifecycleStartFailureStopsStartedServices(t *testing.T) {
	var log []string
	boom := errors.New("boom")
	l := host.NewLifecycle(context.Background())
	l.Register(
		&recorder{name: "a", log: &log},
		&recorder{name: "b", log: &log, startErr: boom},
		&recorder{name: "c", log: &log},
	)
	if err := l.Start(); !errors.Is(err, boom) {
		t.Fatalf("Start = %v", err)
	}
	want := []string{"start a", "start b", "stop a canceled"}
	if !reflect.DeepEqual(log, want) {
		t.Fatalf("log = %v, want %v", log, want)
	}
}

func TestLifecycleStopJoinsErrorsAndRunsOnce(t *testing.T) {
	var log []string
	first, second := errors.New("first"), errors.New("second")
	l := host.NewLifecycle(context.Background())
	l.Register(&recorder{name: "a", log: &log, stopErr: first}, &recorder{name: "b", log: &log, stopErr: second})
	if err := l.Start(); err != nil {
		t.Fatal(err)
	}
	err := l.Stop()
	if !errors.Is(err, first) || !errors.Is(err, second) {
		t.Fatalf("Stop = %v", err)
	}
	if err := l.Stop(); err != nil {
		t.Fatalf("second Stop = %v", err)
	}
	if len(log) != 4 {
		t.Fatalf("log = %v", log)
	}
}
