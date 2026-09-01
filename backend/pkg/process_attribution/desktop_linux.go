//go:build linux

package processattribution

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/adrg/xdg"
)

const linuxDesktopFileMaxBytes = 1 << 20

type linuxDesktopProcess struct {
	executablePath string
	processName    string
	appHint        string
}

type linuxDesktopIdentity struct {
	displayName string
	appID       string
	icon        string
	confidence  string
}

type linuxDesktopEntry struct {
	id       string
	name     string
	exec     string
	tryExec  string
	icon     string
	flatpak  string
	snap     string
	priority int
}

type linuxDesktopResolver struct {
	once     sync.Once
	dataDirs []string
	entries  []linuxDesktopEntry
	loadErr  error
}

func linuxDesktopDataDirs() []string {
	candidates := []string{xdg.DataHome}
	if xdg.DataHome != "" {
		candidates = append(candidates, filepath.Join(xdg.DataHome, "flatpak", "exports", "share"))
	}
	candidates = append(candidates, xdg.DataDirs...)
	candidates = append(candidates, "/var/lib/flatpak/exports/share", "/var/lib/snapd/desktop")
	seen := make(map[string]struct{}, len(candidates))
	result := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		candidate = filepath.Clean(candidate)
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		result = append(result, candidate)
	}
	return result
}

func newLinuxDesktopResolver(dataDirs []string) (*linuxDesktopResolver, error) {
	resolver := newLazyLinuxDesktopResolver(dataDirs)
	resolver.ensureLoaded()
	return resolver, resolver.loadErr
}

func newLazyLinuxDesktopResolver(dataDirs []string) *linuxDesktopResolver {
	return &linuxDesktopResolver{dataDirs: append([]string(nil), dataDirs...)}
}

func (r *linuxDesktopResolver) ensureLoaded() {
	if r == nil {
		return
	}
	r.once.Do(func() {
		r.entries, r.loadErr = loadLinuxDesktopEntries(r.dataDirs)
	})
}

func loadLinuxDesktopEntries(dataDirs []string) ([]linuxDesktopEntry, error) {
	entries := make([]linuxDesktopEntry, 0)
	seenIDs := make(map[string]struct{})
	var firstErr error
	for priority, dataDir := range dataDirs {
		applicationsRoot := filepath.Join(dataDir, "applications")
		err := filepath.WalkDir(applicationsRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				if !errors.Is(walkErr, os.ErrNotExist) && firstErr == nil {
					firstErr = walkErr
				}
				return nil
			}
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".desktop") {
				return nil
			}
			relative, err := filepath.Rel(applicationsRoot, path)
			if err != nil {
				return nil
			}
			desktopID := strings.TrimSuffix(strings.ReplaceAll(filepath.ToSlash(relative), "/", "-"), ".desktop")
			if _, ok := seenIDs[desktopID]; ok {
				return nil
			}
			parsed, ok, err := parseLinuxDesktopEntry(path, desktopID, priority)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return nil
			}
			if !ok {
				return nil
			}
			seenIDs[desktopID] = struct{}{}
			entries = append(entries, parsed)
			return nil
		})
		if err != nil && !errors.Is(err, os.ErrNotExist) && firstErr == nil {
			firstErr = err
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].priority != entries[j].priority {
			return entries[i].priority < entries[j].priority
		}
		return entries[i].id < entries[j].id
	})
	return entries, firstErr
}

func parseLinuxDesktopEntry(path, desktopID string, priority int) (linuxDesktopEntry, bool, error) {
	sections, err := parseLinuxINIFile(path, linuxDesktopFileMaxBytes)
	if err != nil {
		return linuxDesktopEntry{}, false, err
	}
	section := sections["Desktop Entry"]
	if len(section) == 0 || (section["Type"] != "" && section["Type"] != "Application") || parseLinuxBool(section["Hidden"]) {
		return linuxDesktopEntry{}, false, nil
	}
	name := section["Name"]
	if name == "" {
		name = desktopID
	}
	return linuxDesktopEntry{
		id:       desktopID,
		name:     name,
		exec:     section["Exec"],
		tryExec:  section["TryExec"],
		icon:     section["Icon"],
		flatpak:  section["X-Flatpak"],
		snap:     section["X-SnapInstanceName"],
		priority: priority,
	}, true, nil
}

func parseLinuxINIFile(path string, maxBytes int64) (map[string]map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxBytes {
		return nil, errors.New("desktop metadata file is not a bounded regular file")
	}
	sections := make(map[string]map[string]string)
	sectionName := ""
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), int(maxBytes))
	firstLine := true
	for scanner.Scan() {
		line := scanner.Text()
		if firstLine {
			line = strings.TrimPrefix(line, "\ufeff")
			firstLine = false
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			sectionName = strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			if sections[sectionName] == nil {
				sections[sectionName] = make(map[string]string)
			}
			continue
		}
		separator := strings.IndexByte(line, '=')
		if separator <= 0 || sectionName == "" {
			continue
		}
		key := strings.TrimSpace(line[:separator])
		if strings.Contains(key, "[") {
			continue
		}
		sections[sectionName][key] = unescapeLinuxDesktopValue(strings.TrimSpace(line[separator+1:]))
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return sections, nil
}

func unescapeLinuxDesktopValue(value string) string {
	var result strings.Builder
	result.Grow(len(value))
	for index := 0; index < len(value); index++ {
		if value[index] != '\\' || index+1 >= len(value) {
			result.WriteByte(value[index])
			continue
		}
		index++
		switch value[index] {
		case 's':
			result.WriteByte(' ')
		case 'n':
			result.WriteByte('\n')
		case 't':
			result.WriteByte('\t')
		case 'r':
			result.WriteByte('\r')
		case '\\':
			result.WriteByte('\\')
		default:
			result.WriteByte(value[index])
		}
	}
	return result.String()
}

func parseLinuxBool(value string) bool {
	parsed, _ := strconv.ParseBool(strings.TrimSpace(value))
	return parsed
}

func (r *linuxDesktopResolver) resolve(process linuxDesktopProcess) linuxDesktopIdentity {
	fallback := linuxDesktopIdentity{displayName: process.processName, appID: process.appHint, confidence: "none"}
	if r == nil {
		return fallback
	}
	r.ensureLoaded()
	if process.appHint != "" {
		for _, entry := range r.entries {
			if entry.matchesAppID(process.appHint) {
				return entry.identity(process.appHint, "exact")
			}
		}
	}
	canonicalPath := canonicalLinuxExecutable(process.executablePath)
	if canonicalPath != "" {
		for _, entry := range r.entries {
			for _, executable := range entry.executables() {
				if filepath.IsAbs(executable) && canonicalLinuxExecutable(executable) == canonicalPath {
					return entry.identity("", "exact")
				}
			}
		}
	}

	processBase := filepath.Base(process.executablePath)
	if processBase == "." || processBase == string(filepath.Separator) || processBase == "" {
		processBase = process.processName
	}
	var basenameMatch *linuxDesktopEntry
	for index := range r.entries {
		entry := &r.entries[index]
		matched := false
		for _, executable := range entry.executables() {
			if filepath.Base(executable) == processBase {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if basenameMatch != nil && basenameMatch.id != entry.id {
			return fallback
		}
		basenameMatch = entry
	}
	if basenameMatch != nil {
		return basenameMatch.identity("", "heuristic")
	}
	return fallback
}

func (entry linuxDesktopEntry) matchesAppID(appID string) bool {
	appID = normalizeLinuxDesktopID(appID)
	for _, candidate := range []string{entry.id, entry.flatpak, entry.snap} {
		if normalizeLinuxDesktopID(candidate) == appID && appID != "" {
			return true
		}
	}
	return false
}

func normalizeLinuxDesktopID(value string) string {
	return strings.TrimSuffix(strings.TrimSpace(value), ".desktop")
}

func (entry linuxDesktopEntry) identity(preferredAppID, confidence string) linuxDesktopIdentity {
	appID := preferredAppID
	if appID == "" {
		for _, candidate := range []string{entry.flatpak, entry.snap, entry.id} {
			if candidate != "" {
				appID = normalizeLinuxDesktopID(candidate)
				break
			}
		}
	}
	return linuxDesktopIdentity{
		displayName: entry.name,
		appID:       appID,
		icon:        entry.icon,
		confidence:  confidence,
	}
}

func (entry linuxDesktopEntry) executables() []string {
	candidates := make([]string, 0, 2)
	for _, command := range []string{entry.tryExec, entry.exec} {
		if executable := linuxDesktopExecutable(command); executable != "" {
			candidates = append(candidates, executable)
		}
	}
	return candidates
}

func linuxDesktopExecutable(command string) string {
	arguments := splitLinuxDesktopExec(command)
	for index := 0; index < len(arguments); index++ {
		argument := removeLinuxDesktopFieldCodes(arguments[index])
		if argument == "" {
			continue
		}
		if index == 0 && filepath.Base(argument) == "env" {
			continue
		}
		if strings.Contains(argument, "=") && !strings.ContainsAny(argument, `/\\`) {
			continue
		}
		return argument
	}
	return ""
}

func splitLinuxDesktopExec(command string) []string {
	var arguments []string
	var current strings.Builder
	var quote byte
	escaped := false
	flush := func() {
		if current.Len() == 0 {
			return
		}
		arguments = append(arguments, current.String())
		current.Reset()
	}
	for index := 0; index < len(command); index++ {
		character := command[index]
		if escaped {
			current.WriteByte(character)
			escaped = false
			continue
		}
		if character == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if character == quote {
				quote = 0
			} else {
				current.WriteByte(character)
			}
			continue
		}
		if character == '"' || character == '\'' {
			quote = character
			continue
		}
		if character == ' ' || character == '\t' || character == '\n' {
			flush()
			continue
		}
		current.WriteByte(character)
	}
	if escaped {
		current.WriteByte('\\')
	}
	flush()
	return arguments
}

func removeLinuxDesktopFieldCodes(value string) string {
	var result strings.Builder
	result.Grow(len(value))
	for index := 0; index < len(value); index++ {
		if value[index] != '%' || index+1 >= len(value) {
			result.WriteByte(value[index])
			continue
		}
		index++
		if value[index] == '%' {
			result.WriteByte('%')
			continue
		}
		switch value[index] {
		case 'f', 'F', 'u', 'U', 'i', 'c', 'k':
			continue
		default:
			continue
		}
	}
	return result.String()
}

func canonicalLinuxExecutable(path string) string {
	if path == "" {
		return ""
	}
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return path
}
