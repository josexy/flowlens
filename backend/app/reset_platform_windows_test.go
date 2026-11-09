package app

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestResetStartupLaunchDoesNotSuppressFirstGUIShow(t *testing.T) {
	command := exec.Command("flowlens.exe")
	configureResetStartupCommand(command)
	if command.SysProcAttr.HideWindow || command.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
		t.Fatal("progress launch must suppress the console without hiding the first GUI window")
	}
}

func TestResetStartupSnapshotRetriesBriefReaderConflict(t *testing.T) {
	directory := t.TempDir()
	initial := resetStartupAction{Sequence: 1, Action: "ready"}
	if err := writeResetStartupJSON(directory, "state.json", initial); err != nil {
		t.Fatal(err)
	}
	reader, err := openResetStartupFile(filepath.Join(directory, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = reader.Close()
	}()
	updated := resetStartupAction{Sequence: 2, Action: "retry"}
	if err := writeResetStartupJSON(directory, "state.json", updated); err != nil {
		t.Fatalf("IPC reader prevented atomic replacement: %v", err)
	}
	var got resetStartupAction
	if err := readResetStartupJSON(directory, "state.json", &got); err != nil || got != updated {
		t.Fatalf("replacement snapshot: %+v %v", got, err)
	}
}
