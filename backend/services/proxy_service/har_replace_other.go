//go:build !windows

package proxyservice

import "os"

func atomicReplaceHARFile(source, destination string) error {
	return os.Rename(source, destination)
}
