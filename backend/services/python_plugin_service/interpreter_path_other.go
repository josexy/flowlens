//go:build !windows

package pythonpluginservice

import (
	"errors"
	"fmt"
	"os"
)

func validateInterpreterFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect Python interpreter: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("Python interpreter path must reference a regular file")
	}
	if info.Mode().Perm()&0o111 == 0 {
		return errors.New("Python interpreter path is not executable")
	}
	return nil
}
