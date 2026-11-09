//go:build darwin && !cgo && mygo_noinspector

package main

// macOS process attribution and the managed system proxy need cgo. Without it
// they compile to stubs that report themselves unavailable, so a production
// build fails here instead of shipping without them. Build with CGO_ENABLED=1.
var _ = macOSReleaseBuildsRequireCgoEnabled
