package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goSource = `package appservice

const (
	APP_NAME    = "FlowLens"
	APP_VERSION = "1.0.0"
)
`

func project(t *testing.T, config string) string {
	t.Helper()
	root := t.TempDir()
	write := func(name, text string) {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(configFile, config)
	write(appVersionGo, goSource)
	write(changelogFile, "# Changelog\n\n## 1.0.0\n\nFirst.\n")
	return root
}

const plainConfig = `{
  "name": "FlowLens",
  "version": "1.0.0",
  "i18n": {
    "version": "untouched"
  }
}
`

const signedConfig = `{
  "version": "1.0.0",
  "updates": { "publicKey": "abc", "github": "josexy/flowlens" }
}
`

func read(t *testing.T, root, name string) string {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(text)
}

func TestSetUpdatesEveryFile(t *testing.T) {
	root := project(t, plainConfig)
	if err := run(io.Discard, root, "v1.2.3", false); err != nil {
		t.Fatal(err)
	}
	config := read(t, root, configFile)
	if !strings.Contains(config, `"version": "1.2.3"`) || !strings.Contains(config, `"version": "untouched"`) {
		t.Fatal(config)
	}
	if !strings.Contains(read(t, root, appVersionGo), `APP_VERSION = "1.2.3"`) {
		t.Fatal(read(t, root, appVersionGo))
	}
	if err := run(io.Discard, root, "1.2.3", true); err != nil {
		t.Fatal(err)
	}
}

func TestCheckFindsInconsistentMetadataWithoutWriting(t *testing.T) {
	root := project(t, plainConfig)
	before := read(t, root, configFile)
	err := run(io.Discard, root, "1.2.3", true)
	if err == nil || !strings.Contains(err.Error(), configFile) || !strings.Contains(err.Error(), appVersionGo) {
		t.Fatal(err)
	}
	if read(t, root, configFile) != before {
		t.Fatal("check must not write")
	}
	if err := os.WriteFile(filepath.Join(root, appVersionGo), []byte(strings.ReplaceAll(goSource, "1.0.0", "1.0.1")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(io.Discard, root, "1.0.0", true); err == nil || !strings.Contains(err.Error(), "1.0.1") {
		t.Fatal(err)
	}
}

func TestSetWritesNothingWhenASourceIsBroken(t *testing.T) {
	root := project(t, plainConfig)
	if err := os.WriteFile(filepath.Join(root, appVersionGo), []byte("package appservice\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(io.Discard, root, "1.2.3", false); err == nil {
		t.Fatal("broken source")
	}
	if !strings.Contains(read(t, root, configFile), `"version": "1.0.0"`) {
		t.Fatal(read(t, root, configFile))
	}
}

func TestVersionFormat(t *testing.T) {
	root := project(t, plainConfig)
	for _, bad := range []string{"", "1.2", "1.2.x", "latest", "v1.2.3.4"} {
		if err := run(io.Discard, root, bad, false); err == nil {
			t.Errorf("%q", bad)
		}
	}
	if err := run(io.Discard, root, "1.2.3-beta.1", false); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseNeedsTheSigningKey(t *testing.T) {
	root := project(t, signedConfig)
	err := checkRelease(io.Discard, root, "1.0.0", "")
	if err == nil || !strings.Contains(err.Error(), signingKeyEnv) {
		t.Fatal(err)
	}
	if err := checkRelease(io.Discard, root, "1.0.0", "  \n"); err == nil {
		t.Fatal("blank key")
	}
	if err := checkRelease(io.Discard, root, "1.0.0", "secret"); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseNeedsThePublicKeyAndRepository(t *testing.T) {
	err := checkRelease(io.Discard, project(t, plainConfig), "1.0.0", "secret")
	if err == nil || !strings.Contains(err.Error(), "updates needs") {
		t.Fatal(err)
	}
}

func TestReleaseNeedsNotesAndMatchingMetadata(t *testing.T) {
	root := project(t, strings.ReplaceAll(signedConfig, "1.0.0", "1.1.0"))
	err := checkRelease(io.Discard, root, "1.1.0", "secret")
	if err == nil || !strings.Contains(err.Error(), appVersionGo) || !strings.Contains(err.Error(), changelogFile) {
		t.Fatal(err)
	}
}

func TestChangelogHeadingMustMatchExactly(t *testing.T) {
	root := project(t, signedConfig)
	if err := os.WriteFile(filepath.Join(root, changelogFile), []byte("## 1.0.0-beta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkRelease(io.Discard, root, "1.0.0", "secret"); err == nil {
		t.Fatal("a different version's section")
	}
}
