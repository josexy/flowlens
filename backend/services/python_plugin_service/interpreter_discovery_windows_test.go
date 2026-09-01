//go:build windows

package pythonpluginservice

import (
	"reflect"
	"testing"
)

func TestParsePythonLauncherPaths(t *testing.T) {
	output := "Installed Pythons found by py Launcher for Windows\r\n" +
		" -3.13-64        C:\\Program Files\\Python313\\python.exe *\r\n" +
		" -3.12-64        D:\\Python\\Python312\\python.exe\r\n"
	want := []string{
		`C:\Program Files\Python313\python.exe`,
		`D:\Python\Python312\python.exe`,
	}
	if got := parsePythonLauncherPaths(output); !reflect.DeepEqual(got, want) {
		t.Fatalf("parsePythonLauncherPaths() = %#v, want %#v", got, want)
	}
}
