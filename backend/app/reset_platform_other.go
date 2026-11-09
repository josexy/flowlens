//go:build !windows

package app

import (
	"os"
	"os/exec"
)

func configureResetStartupCommand(*exec.Cmd)  {}
func isResetSharingViolation(error) bool      { return false }
func isResetStartupReplaceBlocked(error) bool { return false }

func openResetStartupFile(path string) (*os.File, error) { return os.Open(path) }
