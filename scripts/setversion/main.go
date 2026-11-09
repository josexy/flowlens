// Command setversion sets or checks the release version in project metadata,
// and checks that a release can be signed and published.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	configFile    = "mygo.json"
	appVersionGo  = "backend/services/app_service/app_service.go"
	changelogFile = "CHANGELOG.md"
	signingKeyEnv = "MYGO_UPDATER_PRIVATE_KEY"
)

var versionPattern = regexp.MustCompile(`^v?(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?)$`)

// source is one file holding the version.
type source struct {
	path, description string
	pattern           *regexp.Regexp
}

var sources = []source{
	{configFile, "application version", regexp.MustCompile(`(?m)^(  "version"\s*:\s*")([^"]*)(")`)},
	{appVersionGo, "APP_VERSION", regexp.MustCompile(`(?m)^(\s*APP_VERSION\s*=\s*")([^"]*)(")`)},
}

func main() {
	version := flag.String("version", "", "the version, such as 1.2.3 or v1.2.3")
	check := flag.Bool("check", false, "check the metadata without changing it")
	release := flag.Bool("release", false, "check that this version can be released, without changing anything")
	flag.Parse()
	var err error
	if *release {
		err = checkRelease(os.Stdout, ".", *version, os.Getenv(signingKeyEnv))
	} else {
		err = run(os.Stdout, ".", *version, *check)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "setversion:", err)
		os.Exit(1)
	}
}

func parseVersion(input string) (string, error) {
	match := versionPattern.FindStringSubmatch(strings.TrimSpace(input))
	if match == nil {
		return "", fmt.Errorf("version must look like 1.2.3 or v1.2.3-beta.1, got %q", input)
	}
	return match[1], nil
}

// run sets every source to the version, or with check reports those that
// differ. It writes nothing unless every source can be updated.
func run(out io.Writer, root, input string, check bool) error {
	version, err := parseVersion(input)
	if err != nil {
		return err
	}
	updated := make(map[string][]byte, len(sources))
	var failures []error
	for _, item := range sources {
		text, err := os.ReadFile(filepath.Join(root, item.path))
		if err != nil {
			return err
		}
		matches := item.pattern.FindAllSubmatch(text, -1)
		if len(matches) != 1 {
			return fmt.Errorf("%s: expected one %s, found %d", item.path, item.description, len(matches))
		}
		if current := string(matches[0][2]); check && current != version {
			failures = append(failures, fmt.Errorf("%s: %s is %q, expected %q", item.path, item.description, current, version))
		}
		updated[item.path] = item.pattern.ReplaceAll(text, []byte("${1}"+version+"${3}"))
	}
	if err := errors.Join(failures...); err != nil {
		return err
	}
	if check {
		fmt.Fprintf(out, "Version metadata matches %s.\n", version)
		return nil
	}
	for _, item := range sources {
		if err := os.WriteFile(filepath.Join(root, item.path), updated[item.path], 0o644); err != nil {
			return err
		}
		fmt.Fprintf(out, "Updated %s: %s\n", item.path, item.description)
	}
	fmt.Fprintf(out, "Version metadata updated to %s.\n", version)
	return nil
}

// checkRelease fails before anything is built or uploaded when installed
// applications could not verify the release.
func checkRelease(out io.Writer, root, input, signingKey string) error {
	version, err := parseVersion(input)
	if err != nil {
		return err
	}
	var failures []error
	if err := run(io.Discard, root, input, true); err != nil {
		failures = append(failures, err)
	}
	if strings.TrimSpace(signingKey) == "" {
		failures = append(failures, fmt.Errorf("%s is not set: updates could not be signed", signingKeyEnv))
	}
	var config struct {
		Updates *struct {
			PublicKey string `json:"publicKey"`
			GitHub    string `json:"github"`
		} `json:"updates"`
	}
	text, err := os.ReadFile(filepath.Join(root, configFile))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(text, &config); err != nil {
		return fmt.Errorf("%s: %w", configFile, err)
	}
	if config.Updates == nil || config.Updates.PublicKey == "" || config.Updates.GitHub == "" {
		failures = append(failures, fmt.Errorf("%s: updates needs publicKey and github", configFile))
	}
	changelog, err := os.ReadFile(filepath.Join(root, changelogFile))
	if err != nil {
		return err
	}
	heading := regexp.MustCompile(`(?m)^## ` + regexp.QuoteMeta(version) + `\s*$`)
	if !heading.Match(changelog) {
		failures = append(failures, fmt.Errorf("%s: no section for %s, whose text becomes the release notes", changelogFile, version))
	}
	if err := errors.Join(failures...); err != nil {
		return err
	}
	fmt.Fprintf(out, "Release %s can be signed and published.\n", version)
	return nil
}
