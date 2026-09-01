//go:build darwin && !cgo

package systemproxy

func fromPlatform() string {
	return ""
}

type unsupportedPlatformDriver struct{}

func newPlatformDriver() platformDriver {
	return unsupportedPlatformDriver{}
}

func (unsupportedPlatformDriver) Supported() bool                { return false }
func (unsupportedPlatformDriver) Supports(Mode) bool             { return false }
func (unsupportedPlatformDriver) Snapshot() (any, error)         { return nil, ErrUnsupported }
func (unsupportedPlatformDriver) Apply(Endpoint) error           { return ErrUnsupported }
func (unsupportedPlatformDriver) Matches(Endpoint) (bool, error) { return false, ErrUnsupported }
func (unsupportedPlatformDriver) Restore(any) error              { return ErrUnsupported }
