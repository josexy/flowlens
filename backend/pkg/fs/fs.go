package fs

import (
	"errors"
	stdfs "io/fs"
	"os"
	"path/filepath"
	"runtime"
)

const (
	HBIN_SUFFIX = ".hbin"
	HIDX_SUFFIX = ".hidx"

	baseStorageDirName = "FlowLens"

	PrivateDirMode  stdfs.FileMode = 0o700
	PrivateFileMode stdfs.FileMode = 0o600
)

func PathExists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	return !errors.Is(err, stdfs.ErrNotExist)
}

func CreateDirIfNotExists(path string) error {
	return EnsurePrivateDir(path)
}

// EnsurePrivateDir creates a FlowLens-owned storage directory and tightens an
// existing directory on Unix. Windows does not implement Unix permission bits,
// so the chmod step is intentionally skipped there.
func EnsurePrivateDir(path string) error {
	if err := os.MkdirAll(path, PrivateDirMode); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("private directory path is not a directory")
	}
	return os.Chmod(path, PrivateDirMode)
}

// EnsurePrivateFile tightens an existing FlowLens-owned file on Unix.
func EnsurePrivateFile(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("private file path is not a regular file")
	}
	return os.Chmod(path, PrivateFileMode)
}

func DeleteFile(path string) error {
	return os.Remove(path)
}

func DirSize(path string) int64 {
	var total int64
	_ = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

func GetBaseStorageDir() (string, error) {
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	userConfigDir = filepath.Join(userConfigDir, baseStorageDirName)
	if err = CreateDirIfNotExists(userConfigDir); err != nil {
		return "", err
	}
	return userConfigDir, nil
}

func GetHBinFileName(key string) string {
	return key + HBIN_SUFFIX
}

func GetHIdxFileName(key string) string {
	return key + HIDX_SUFFIX
}
