//go:build !windows

package bodycache

import "os"

// openSharedReadOnly opens path for reading. On Unix an open handle never
// prevents the writer from renaming or removing the file.
func openSharedReadOnly(path string) (*os.File, error) {
	return os.Open(path)
}
