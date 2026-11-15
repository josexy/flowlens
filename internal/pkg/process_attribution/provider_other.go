//go:build !windows && !darwin && !linux

package processattribution

func init() {
	platformProviderFactory = func() Provider {
		return &unavailableProvider{reason: "platform_unsupported"}
	}
}
