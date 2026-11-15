//go:build darwin && !cgo

package processattribution

import (
	"context"
	"testing"
)

func TestDarwinNoCGOProviderIsUnsupported(t *testing.T) {
	result := NewPlatformProvider().Lookup(context.Background(), EndpointTuple{})
	if result.Status != StatusUnsupported || result.Reason != "darwin_requires_cgo" {
		t.Fatalf("no-cgo provider result = %+v", result)
	}
}
