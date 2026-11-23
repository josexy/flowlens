package serviceruntime_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/josexy/flowlens/internal/serviceruntime"
)

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
	l := serviceruntime.NewLifecycle(context.Background())
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
	l := serviceruntime.NewLifecycle(context.Background())
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
	l := serviceruntime.NewLifecycle(context.Background())
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
