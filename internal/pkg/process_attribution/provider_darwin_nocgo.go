//go:build darwin && !cgo

package processattribution

func init() {
	platformProviderFactory = func() Provider {
		return &unavailableProvider{reason: "darwin_requires_cgo"}
	}
}
