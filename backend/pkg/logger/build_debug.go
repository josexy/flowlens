//go:build !production

package logger

func detectDebugBuild() bool {
	return true
}
