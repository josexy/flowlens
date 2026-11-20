package memstatsservice

import (
	"errors"
	"testing"
)

func TestDarwinMachBindings(t *testing.T) {
	api, err := loadDarwinMach()
	if err != nil {
		t.Fatal(err)
	}
	if sample := api.read(); sample.Measure != MeasurePhysFootprint || sample.Bytes <= 0 {
		t.Fatalf("native physical footprint = %+v", sample)
	}
}

func TestDarwinMachMissingSymbols(t *testing.T) {
	failure := errors.New("missing native API")
	if _, err := bindDarwinMach(func(string, int) (uintptr, error) { return 0, failure }, nil); err == nil {
		t.Fatal("missing library accepted")
	}
	for _, missing := range []string{"task_info", "mach_task_self_"} {
		_, err := bindDarwinMach(func(string, int) (uintptr, error) { return 1, nil }, func(_ uintptr, name string) (uintptr, error) {
			if name == missing {
				return 0, failure
			}
			return 1, nil
		})
		if err == nil {
			t.Fatalf("missing %s accepted", missing)
		}
	}
}
