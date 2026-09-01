package pythonpluginservice

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	stagingDirectoryName    = ".staging"
	trashDirectoryName      = ".trash"
	quarantineDirectoryName = ".quarantine"

	maxManagedFileBytes    = 32 * 1024 * 1024
	maxManagedPackageBytes = 64 * 1024 * 1024
)

type RevisionValidationRequest struct {
	ExecutionID string
	PluginID    string
	Revision    string
	Path        string
}

type RevisionValidator interface {
	ValidateRevision(ctx context.Context, request RevisionValidationRequest) error
}

type packageManager struct {
	repository   *repository
	packagesRoot string
	runtimeRoot  string
	validator    RevisionValidator

	mu         sync.Mutex
	references map[string]int
	stale      map[string]struct{}
}

type RevisionLease struct {
	Path    string
	once    sync.Once
	release func()
}

func (l *RevisionLease) Release() {
	if l == nil {
		return
	}
	l.once.Do(l.release)
}

func newPackageManager(repository *repository, packagesRoot, runtimeRoot string, validator RevisionValidator) (*packageManager, error) {
	if repository == nil || repository.db == nil {
		return nil, errors.New("python plugin repository is not available")
	}
	var err error
	packagesRoot, err = filepath.Abs(strings.TrimSpace(packagesRoot))
	if err != nil {
		return nil, fmt.Errorf("resolve python plugin package root: %w", err)
	}
	runtimeRoot, err = filepath.Abs(strings.TrimSpace(runtimeRoot))
	if err != nil {
		return nil, fmt.Errorf("resolve python plugin runtime root: %w", err)
	}
	for _, directory := range []string{
		packagesRoot,
		filepath.Join(packagesRoot, stagingDirectoryName),
		filepath.Join(packagesRoot, trashDirectoryName),
		filepath.Join(packagesRoot, quarantineDirectoryName),
		runtimeRoot,
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return nil, fmt.Errorf("create python plugin directory %q: %w", directory, err)
		}
	}
	return &packageManager{
		repository: repository, packagesRoot: packagesRoot, runtimeRoot: runtimeRoot,
		validator: validator, references: make(map[string]int), stale: make(map[string]struct{}),
	}, nil
}

func (m *packageManager) createPlugin(ctx context.Context, input CreatePluginInput) (*Plugin, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = uuid.NewString()
	}
	if err := validateUUID(id); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.ParamsJSON) == "" {
		input.ParamsJSON = `{}`
	}
	manifest, err := defaultManifest(id, input.Name, input.Description)
	if err != nil {
		return nil, err
	}
	manifestBytes, err := marshalManifest(manifest)
	if err != nil {
		return nil, err
	}
	finalPath := m.packagePath(id)
	if _, err := os.Lstat(finalPath); err == nil {
		return nil, fmt.Errorf("python plugin package %q already exists", id)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect python plugin package %q: %w", id, err)
	}
	stagePath, err := m.newStagingDirectory("create-" + id)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stagePath)
	for name, content := range map[string][]byte{
		manifestFileName: manifestBytes,
		mainFileName:     []byte(defaultMainSource),
		helpersFileName:  []byte(defaultHelpersSource),
	} {
		if err := atomicWriteManagedFile(filepath.Join(stagePath, name), content); err != nil {
			return nil, err
		}
	}
	if _, _, err := validateAndHashPackage(ctx, stagePath, id); err != nil {
		return nil, err
	}
	if err := os.Rename(stagePath, finalPath); err != nil {
		return nil, fmt.Errorf("commit python plugin package %q: %w", id, err)
	}
	input.ID = id
	input.Name = manifest.Name
	input.Description = manifest.Description
	plugin, err := m.repository.createPlugin(ctx, input)
	if err != nil {
		_ = os.RemoveAll(finalPath)
		return nil, err
	}
	return plugin, nil
}

func (m *packageManager) listFiles(ctx context.Context, pluginID string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateUUID(strings.TrimSpace(pluginID)); err != nil {
		return nil, err
	}
	files, _, err := collectManagedFiles(ctx, m.packagePath(strings.TrimSpace(pluginID)))
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(files))
	for _, file := range files {
		result = append(result, filepath.ToSlash(file.relativePath))
	}
	sort.Strings(result)
	return result, nil
}

func (m *packageManager) readFile(ctx context.Context, pluginID, relativePath string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	pluginID = strings.TrimSpace(pluginID)
	if err := validateUUID(pluginID); err != nil {
		return nil, err
	}
	path, _, err := resolveManagedFilePath(m.packagePath(pluginID), relativePath)
	if err != nil {
		return nil, err
	}
	if err := rejectSymlinkPath(m.packagePath(pluginID), path); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect python plugin file %q: %w", relativePath, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("python plugin path %q is not a regular file", relativePath)
	}
	if info.Size() > maxManagedFileBytes {
		return nil, fmt.Errorf("python plugin file %q exceeds %d bytes", relativePath, maxManagedFileBytes)
	}
	value, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read python plugin file %q: %w", relativePath, err)
	}
	return value, nil
}

func (m *packageManager) activateCurrent(ctx context.Context, pluginID string) (*Plugin, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pluginID = strings.TrimSpace(pluginID)
	plugin, err := m.repository.getPlugin(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	manifest, revision, revisionPath, err := m.snapshotCandidateLocked(ctx, pluginID, m.packagePath(pluginID))
	if err != nil {
		_ = m.repository.setPluginValidationFailure(ctx, pluginID, ValidationStatusInvalid, err.Error())
		return nil, err
	}
	if err := m.validateRevision(ctx, pluginID, revision, revisionPath); err != nil {
		_ = m.repository.setPluginValidationFailure(ctx, pluginID, ValidationStatusInvalid, err.Error())
		m.removeUnreferencedRevisionLocked(pluginID, revision, plugin)
		return nil, err
	}
	if err := m.repository.activatePluginPackage(ctx, pluginID, manifest, revision); err != nil {
		m.removeUnreferencedRevisionLocked(pluginID, revision, plugin)
		return nil, err
	}
	updated, err := m.repository.getPlugin(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	if err := m.pruneRevisionsLocked(pluginID, updated.ActiveRevision, updated.LastGoodRevision); err != nil {
		return nil, err
	}
	return updated, nil
}

func (m *packageManager) writeFile(ctx context.Context, pluginID, relativePath string, content []byte) (*Plugin, error) {
	if len(content) > maxManagedFileBytes {
		return nil, fmt.Errorf("python plugin file exceeds %d bytes", maxManagedFileBytes)
	}
	return m.mutatePackage(ctx, pluginID, func(stageRoot string) error {
		path, normalized, err := resolveManagedFilePath(stageRoot, relativePath)
		if err != nil {
			return err
		}
		if isIgnoredManagedPath(normalized) {
			return fmt.Errorf("python plugin file %q is reserved or temporary", relativePath)
		}
		if err := rejectSymlinkPath(stageRoot, path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return atomicWriteManagedFile(path, content)
	})
}

func (m *packageManager) renameFile(ctx context.Context, pluginID, oldRelativePath, newRelativePath string) (*Plugin, error) {
	oldSlash := normalizeSlashes(oldRelativePath)
	if oldSlash == manifestFileName || oldSlash == mainFileName {
		return nil, fmt.Errorf("python plugin file %q cannot be renamed", oldRelativePath)
	}
	return m.mutatePackage(ctx, pluginID, func(stageRoot string) error {
		oldPath, _, err := resolveManagedFilePath(stageRoot, oldRelativePath)
		if err != nil {
			return err
		}
		newPath, normalizedNew, err := resolveManagedFilePath(stageRoot, newRelativePath)
		if err != nil {
			return err
		}
		if isIgnoredManagedPath(normalizedNew) {
			return fmt.Errorf("python plugin file %q is reserved or temporary", newRelativePath)
		}
		if err := rejectSymlinkPath(stageRoot, oldPath); err != nil {
			return err
		}
		info, err := os.Lstat(oldPath)
		if err != nil {
			return fmt.Errorf("inspect python plugin file %q: %w", oldRelativePath, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("python plugin path %q is not a regular file", oldRelativePath)
		}
		if _, err := os.Lstat(newPath); err == nil {
			return fmt.Errorf("python plugin file %q already exists", newRelativePath)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect python plugin destination %q: %w", newRelativePath, err)
		}
		if err := os.MkdirAll(filepath.Dir(newPath), 0o700); err != nil {
			return fmt.Errorf("create python plugin destination directory: %w", err)
		}
		if err := os.Rename(oldPath, newPath); err != nil {
			return fmt.Errorf("rename python plugin file: %w", err)
		}
		removeEmptyParents(filepath.Dir(oldPath), stageRoot)
		return nil
	})
}

func (m *packageManager) deleteFile(ctx context.Context, pluginID, relativePath string) (*Plugin, error) {
	normalized := normalizeSlashes(relativePath)
	if normalized == manifestFileName || normalized == mainFileName {
		return nil, fmt.Errorf("python plugin file %q is required and cannot be deleted", relativePath)
	}
	return m.mutatePackage(ctx, pluginID, func(stageRoot string) error {
		path, _, err := resolveManagedFilePath(stageRoot, relativePath)
		if err != nil {
			return err
		}
		if err := rejectSymlinkPath(stageRoot, path); err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("inspect python plugin file %q: %w", relativePath, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("python plugin path %q is not a regular file", relativePath)
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("delete python plugin file %q: %w", relativePath, err)
		}
		removeEmptyParents(filepath.Dir(path), stageRoot)
		return nil
	})
}

func (m *packageManager) mutatePackage(ctx context.Context, pluginID string, mutate func(stageRoot string) error) (*Plugin, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pluginID = strings.TrimSpace(pluginID)
	plugin, err := m.repository.getPlugin(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	stagePath, err := m.newStagingDirectory("edit-" + pluginID)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stagePath)
	if err := copyManagedPackage(ctx, m.packagePath(pluginID), stagePath); err != nil {
		return nil, err
	}
	if err := mutate(stagePath); err != nil {
		return nil, err
	}
	manifest, revision, revisionPath, err := m.snapshotCandidateLocked(ctx, pluginID, stagePath)
	if err != nil {
		_ = m.repository.setPluginValidationFailure(ctx, pluginID, ValidationStatusInvalid, err.Error())
		return nil, err
	}
	if err := m.validateRevision(ctx, pluginID, revision, revisionPath); err != nil {
		_ = m.repository.setPluginValidationFailure(ctx, pluginID, ValidationStatusInvalid, err.Error())
		m.removeUnreferencedRevisionLocked(pluginID, revision, plugin)
		return nil, err
	}

	finalPath := m.packagePath(pluginID)
	backupPath := m.trashPath(pluginID)
	if err := os.RemoveAll(backupPath); err != nil {
		return nil, fmt.Errorf("clear stale python plugin backup: %w", err)
	}
	if err := os.Rename(finalPath, backupPath); err != nil {
		return nil, fmt.Errorf("stage current python plugin package: %w", err)
	}
	restore := func() {
		_ = os.RemoveAll(finalPath)
		_ = os.Rename(backupPath, finalPath)
	}
	if err := os.Rename(stagePath, finalPath); err != nil {
		restore()
		return nil, fmt.Errorf("commit python plugin package: %w", err)
	}
	if err := m.repository.activatePluginPackage(ctx, pluginID, manifest, revision); err != nil {
		restore()
		m.removeUnreferencedRevisionLocked(pluginID, revision, plugin)
		return nil, err
	}
	if err := os.RemoveAll(backupPath); err != nil {
		return nil, fmt.Errorf("remove committed python plugin backup: %w", err)
	}
	updated, err := m.repository.getPlugin(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	if err := m.pruneRevisionsLocked(pluginID, updated.ActiveRevision, updated.LastGoodRevision); err != nil {
		return nil, err
	}
	return updated, nil
}

func (m *packageManager) deletePlugin(ctx context.Context, pluginID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	pluginID = strings.TrimSpace(pluginID)
	plugin, err := m.repository.getPlugin(ctx, pluginID)
	if err != nil {
		return err
	}
	if err := m.repository.setPluginEnabled(ctx, pluginID, false); err != nil {
		return err
	}
	finalPath, backupPath := m.packagePath(pluginID), m.trashPath(pluginID)
	if err := os.RemoveAll(backupPath); err != nil {
		_ = m.repository.setPluginEnabled(ctx, pluginID, plugin.Enabled)
		return fmt.Errorf("clear stale python plugin trash: %w", err)
	}
	if err := os.Rename(finalPath, backupPath); err != nil {
		_ = m.repository.setPluginEnabled(ctx, pluginID, plugin.Enabled)
		return fmt.Errorf("move python plugin package to trash: %w", err)
	}
	if err := m.repository.deletePlugin(ctx, pluginID); err != nil {
		_ = os.Rename(backupPath, finalPath)
		_ = m.repository.setPluginEnabled(ctx, pluginID, plugin.Enabled)
		return err
	}
	if err := os.RemoveAll(backupPath); err != nil {
		return fmt.Errorf("remove deleted python plugin package: %w", err)
	}
	if err := m.pruneRevisionsLocked(pluginID); err != nil {
		return err
	}
	return nil
}

func (m *packageManager) snapshotCandidateLocked(ctx context.Context, pluginID, sourcePath string) (Manifest, string, string, error) {
	manifest, revision, err := validateAndHashPackage(ctx, sourcePath, pluginID)
	if err != nil {
		return Manifest{}, "", "", err
	}
	pluginRuntimeRoot := filepath.Join(m.runtimeRoot, pluginID)
	if err := os.MkdirAll(pluginRuntimeRoot, 0o700); err != nil {
		return Manifest{}, "", "", fmt.Errorf("create python plugin runtime directory: %w", err)
	}
	revisionPath := m.revisionPath(pluginID, revision)
	if info, err := os.Lstat(revisionPath); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return Manifest{}, "", "", fmt.Errorf("python plugin revision path %q is unsafe", revisionPath)
		}
		return manifest, revision, revisionPath, nil
	} else if !os.IsNotExist(err) {
		return Manifest{}, "", "", fmt.Errorf("inspect python plugin revision: %w", err)
	}
	stagePath := filepath.Join(pluginRuntimeRoot, "."+revision+"--"+uuid.NewString()+".tmp")
	if err := os.Mkdir(stagePath, 0o700); err != nil {
		return Manifest{}, "", "", fmt.Errorf("create python plugin revision staging directory: %w", err)
	}
	defer os.RemoveAll(stagePath)
	if err := copyManagedPackage(ctx, sourcePath, stagePath); err != nil {
		return Manifest{}, "", "", err
	}
	if err := os.Rename(stagePath, revisionPath); err != nil {
		if _, statErr := os.Stat(revisionPath); statErr == nil {
			return manifest, revision, revisionPath, nil
		}
		return Manifest{}, "", "", fmt.Errorf("commit python plugin revision: %w", err)
	}
	return manifest, revision, revisionPath, nil
}

func (m *packageManager) validateRevision(ctx context.Context, pluginID, revision, revisionPath string) error {
	if m.validator == nil {
		return errors.New("Python revision validator is unavailable")
	}
	if err := m.validator.ValidateRevision(ctx, RevisionValidationRequest{
		PluginID: pluginID, Revision: revision, Path: revisionPath,
	}); err != nil {
		return fmt.Errorf("validate Python plugin revision: %w", err)
	}
	return nil
}

func (m *packageManager) acquireRevision(pluginID, revision string) (*RevisionLease, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pluginID, revision = strings.TrimSpace(pluginID), strings.TrimSpace(revision)
	if err := validateUUID(pluginID); err != nil {
		return nil, err
	}
	if !isRevisionName(revision) {
		return nil, errors.New("invalid python plugin revision")
	}
	path := m.revisionPath(pluginID, revision)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect python plugin revision: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("python plugin revision is not a safe directory")
	}
	key := revisionKey(pluginID, revision)
	m.references[key]++
	return &RevisionLease{
		Path: path,
		release: func() {
			m.releaseRevision(pluginID, revision)
		},
	}, nil
}

func (m *packageManager) releaseRevision(pluginID, revision string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := revisionKey(pluginID, revision)
	if m.references[key] > 1 {
		m.references[key]--
		return
	}
	delete(m.references, key)
	if _, stale := m.stale[key]; stale {
		_ = os.RemoveAll(m.revisionPath(pluginID, revision))
		delete(m.stale, key)
		_ = removeDirectoryIfEmpty(filepath.Join(m.runtimeRoot, pluginID))
	}
}

func (m *packageManager) removeUnreferencedRevisionLocked(pluginID, revision string, current *Plugin) {
	if current != nil && (revision == current.ActiveRevision || revision == current.LastGoodRevision) {
		return
	}
	key := revisionKey(pluginID, revision)
	if m.references[key] > 0 {
		m.stale[key] = struct{}{}
		return
	}
	_ = os.RemoveAll(m.revisionPath(pluginID, revision))
}

func (m *packageManager) pruneRevisionsLocked(pluginID string, keepRevisions ...string) error {
	keep := make(map[string]struct{}, len(keepRevisions))
	for _, revision := range keepRevisions {
		if isRevisionName(revision) {
			keep[revision] = struct{}{}
		}
	}
	root := filepath.Join(m.runtimeRoot, pluginID)
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("list python plugin revisions: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(root, name)
		if _, retained := keep[name]; retained {
			continue
		}
		key := revisionKey(pluginID, name)
		if m.references[key] > 0 {
			m.stale[key] = struct{}{}
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove stale python plugin revision %q: %w", name, err)
		}
		delete(m.stale, key)
	}
	return removeDirectoryIfEmpty(root)
}

func (m *packageManager) reconcile(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, directory := range []string{m.packagesRoot, m.runtimeRoot, filepath.Join(m.packagesRoot, trashDirectoryName), filepath.Join(m.packagesRoot, quarantineDirectoryName)} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("create reconciliation directory %q: %w", directory, err)
		}
	}
	plugins, err := m.repository.listPlugins(ctx)
	if err != nil {
		return err
	}
	registered := make(map[string]*Plugin, len(plugins))
	for _, plugin := range plugins {
		registered[plugin.ID] = plugin
	}
	if err := m.restoreInterruptedTrashLocked(registered); err != nil {
		return err
	}
	stagingRoot := filepath.Join(m.packagesRoot, stagingDirectoryName)
	if err := os.RemoveAll(stagingRoot); err != nil {
		return fmt.Errorf("remove stale python plugin staging files: %w", err)
	}
	if err := os.MkdirAll(stagingRoot, 0o700); err != nil {
		return fmt.Errorf("recreate python plugin staging directory: %w", err)
	}

	entries, err := os.ReadDir(m.packagesRoot)
	if err != nil {
		return fmt.Errorf("list python plugin packages: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if isControlDirectory(name) {
			continue
		}
		if _, ok := registered[name]; ok && entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			continue
		}
		if err := m.quarantinePathLocked(filepath.Join(m.packagesRoot, name), name); err != nil {
			return err
		}
	}
	for id, plugin := range registered {
		packagePath := m.packagePath(id)
		info, statErr := os.Lstat(packagePath)
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			diagnostic := "managed plugin package is missing"
			if statErr != nil && !os.IsNotExist(statErr) {
				diagnostic = "managed plugin package is unavailable: " + statErr.Error()
			}
			if err := m.repository.disableMissingPlugin(ctx, id, diagnostic); err != nil {
				return err
			}
			continue
		}
		if _, _, err := validateAndHashPackage(ctx, packagePath, id); err != nil {
			if recordErr := m.repository.disableMissingPlugin(ctx, id, err.Error()); recordErr != nil {
				return recordErr
			}
		}
		if err := m.pruneRevisionsLocked(id, plugin.ActiveRevision, plugin.LastGoodRevision); err != nil {
			return err
		}
	}
	runtimeEntries, err := os.ReadDir(m.runtimeRoot)
	if err != nil {
		return fmt.Errorf("list python plugin runtime roots: %w", err)
	}
	for _, entry := range runtimeEntries {
		if entry.Name() == "_sdk" {
			continue
		}
		if _, ok := registered[entry.Name()]; ok {
			continue
		}
		if err := os.RemoveAll(filepath.Join(m.runtimeRoot, entry.Name())); err != nil {
			return fmt.Errorf("remove orphan python plugin runtime %q: %w", entry.Name(), err)
		}
	}
	return nil
}

func (m *packageManager) restoreInterruptedTrashLocked(registered map[string]*Plugin) error {
	trashRoot := filepath.Join(m.packagesRoot, trashDirectoryName)
	entries, err := os.ReadDir(trashRoot)
	if err != nil {
		return fmt.Errorf("list python plugin trash: %w", err)
	}
	for _, entry := range entries {
		path := filepath.Join(trashRoot, entry.Name())
		id, _, _ := strings.Cut(entry.Name(), "--")
		plugin := registered[id]
		finalPath := m.packagePath(id)
		_, finalErr := os.Lstat(finalPath)
		if plugin != nil && os.IsNotExist(finalErr) && entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			if err := os.Rename(path, finalPath); err != nil {
				return fmt.Errorf("restore interrupted python plugin package %q: %w", id, err)
			}
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove stale python plugin trash %q: %w", entry.Name(), err)
		}
	}
	return nil
}

func (m *packageManager) quarantinePathLocked(sourcePath, name string) error {
	quarantineRoot := filepath.Join(m.packagesRoot, quarantineDirectoryName)
	destination := filepath.Join(quarantineRoot, name+"--"+fmt.Sprint(time.Now().UnixNano()))
	if err := os.Rename(sourcePath, destination); err != nil {
		return fmt.Errorf("quarantine orphan python plugin package %q: %w", name, err)
	}
	return nil
}

func (m *packageManager) newStagingDirectory(prefix string) (string, error) {
	path, err := os.MkdirTemp(filepath.Join(m.packagesRoot, stagingDirectoryName), prefix+"--")
	if err != nil {
		return "", fmt.Errorf("create python plugin staging directory: %w", err)
	}
	return path, nil
}

func (m *packageManager) packagePath(pluginID string) string {
	return filepath.Join(m.packagesRoot, pluginID)
}

func (m *packageManager) revisionPath(pluginID, revision string) string {
	return filepath.Join(m.runtimeRoot, pluginID, revision)
}

func (m *packageManager) trashPath(pluginID string) string {
	return filepath.Join(m.packagesRoot, trashDirectoryName, pluginID+"--pending")
}

type managedFile struct {
	relativePath string
	absolutePath string
	size         int64
}

func collectManagedFiles(ctx context.Context, root string) ([]managedFile, int64, error) {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return nil, 0, fmt.Errorf("inspect python plugin package: %w", err)
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, 0, errors.New("python plugin package is not a safe directory")
	}
	files := make([]managedFile, 0)
	var total int64
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("python plugin package contains symlink %q", relative)
		}
		if entry.IsDir() {
			if isIgnoredManagedPath(relative) {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("python plugin package contains non-regular file %q", relative)
		}
		if isIgnoredManagedPath(relative) {
			return nil
		}
		if info.Size() > maxManagedFileBytes {
			return fmt.Errorf("python plugin file %q exceeds %d bytes", relative, maxManagedFileBytes)
		}
		total += info.Size()
		if total > maxManagedPackageBytes {
			return fmt.Errorf("python plugin package exceeds %d bytes", maxManagedPackageBytes)
		}
		files = append(files, managedFile{relativePath: relative, absolutePath: path, size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, 0, fmt.Errorf("walk python plugin package: %w", err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].relativePath < files[j].relativePath })
	return files, total, nil
}

func validateAndHashPackage(ctx context.Context, root, pluginID string) (Manifest, string, error) {
	files, _, err := collectManagedFiles(ctx, root)
	if err != nil {
		return Manifest{}, "", err
	}
	byName := make(map[string]managedFile, len(files))
	for _, file := range files {
		byName[file.relativePath] = file
	}
	manifestFile, ok := byName[manifestFileName]
	if !ok {
		return Manifest{}, "", errors.New("python plugin package is missing manifest.json")
	}
	if _, ok := byName[mainFileName]; !ok {
		return Manifest{}, "", errors.New("python plugin package is missing main.py")
	}
	manifestBytes, err := os.ReadFile(manifestFile.absolutePath)
	if err != nil {
		return Manifest{}, "", fmt.Errorf("read python plugin manifest: %w", err)
	}
	manifest, err := parseManifest(manifestBytes, pluginID)
	if err != nil {
		return Manifest{}, "", err
	}
	hash := sha256.New()
	var length [8]byte
	for _, file := range files {
		name := filepath.ToSlash(file.relativePath)
		binary.BigEndian.PutUint64(length[:], uint64(len(name)))
		_, _ = hash.Write(length[:])
		_, _ = io.WriteString(hash, name)
		binary.BigEndian.PutUint64(length[:], uint64(file.size))
		_, _ = hash.Write(length[:])
		value, err := os.Open(file.absolutePath)
		if err != nil {
			return Manifest{}, "", fmt.Errorf("open python plugin file %q: %w", name, err)
		}
		_, copyErr := io.Copy(hash, value)
		closeErr := value.Close()
		if copyErr != nil {
			return Manifest{}, "", fmt.Errorf("hash python plugin file %q: %w", name, copyErr)
		}
		if closeErr != nil {
			return Manifest{}, "", fmt.Errorf("close python plugin file %q: %w", name, closeErr)
		}
	}
	return manifest, hex.EncodeToString(hash.Sum(nil)), nil
}

func copyManagedPackage(ctx context.Context, sourceRoot, destinationRoot string) error {
	files, _, err := collectManagedFiles(ctx, sourceRoot)
	if err != nil {
		return err
	}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		destination := filepath.Join(destinationRoot, filepath.FromSlash(file.relativePath))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return fmt.Errorf("create python plugin package directory: %w", err)
		}
		input, err := os.Open(file.absolutePath)
		if err != nil {
			return fmt.Errorf("open python plugin source file %q: %w", file.relativePath, err)
		}
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			input.Close()
			return fmt.Errorf("create python plugin destination file %q: %w", file.relativePath, err)
		}
		_, copyErr := io.Copy(output, input)
		inputCloseErr := input.Close()
		outputCloseErr := output.Close()
		if copyErr != nil {
			return fmt.Errorf("copy python plugin file %q: %w", file.relativePath, copyErr)
		}
		if inputCloseErr != nil {
			return fmt.Errorf("close python plugin source file %q: %w", file.relativePath, inputCloseErr)
		}
		if outputCloseErr != nil {
			return fmt.Errorf("close python plugin destination file %q: %w", file.relativePath, outputCloseErr)
		}
	}
	return nil
}

func resolveManagedFilePath(root, relativePath string) (string, string, error) {
	normalized := normalizeSlashes(relativePath)
	if normalized == "" || normalized == "." || pathpkg.IsAbs(normalized) || filepath.VolumeName(filepath.FromSlash(normalized)) != "" {
		return "", "", fmt.Errorf("invalid python plugin file path %q", relativePath)
	}
	for segment := range strings.SplitSeq(normalized, "/") {
		if segment == "" || segment == "." || segment == ".." || strings.Contains(segment, ":") {
			return "", "", fmt.Errorf("invalid python plugin file path %q", relativePath)
		}
	}
	if isControlDirectory(strings.Split(normalized, "/")[0]) {
		return "", "", fmt.Errorf("python plugin file path %q targets a managed control directory", relativePath)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", "", fmt.Errorf("resolve python plugin root: %w", err)
	}
	result := filepath.Join(root, filepath.FromSlash(normalized))
	relative, err := filepath.Rel(root, result)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("python plugin file path %q escapes the package", relativePath)
	}
	return result, normalized, nil
}

func normalizeSlashes(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	return pathpkg.Clean(value)
}

func rejectSymlinkPath(root, target string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("python plugin path escapes the package")
	}
	current := root
	for segment := range strings.SplitSeq(relative, string(filepath.Separator)) {
		if segment == "." || segment == "" {
			continue
		}
		current = filepath.Join(current, segment)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			return statErr
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("python plugin path contains symlink %q", current)
		}
	}
	return nil
}

func atomicWriteManagedFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create python plugin file directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".flowlens-write-*.tmp")
	if err != nil {
		return fmt.Errorf("create python plugin temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return fmt.Errorf("write python plugin temporary file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync python plugin temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close python plugin temporary file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace python plugin file: %w", err)
	}
	return nil
}

func isIgnoredManagedPath(relativePath string) bool {
	relativePath = filepath.ToSlash(relativePath)
	for segment := range strings.SplitSeq(relativePath, "/") {
		lower := strings.ToLower(segment)
		if lower == "__pycache__" || lower == ".ds_store" || strings.HasSuffix(lower, ".pyc") ||
			strings.HasSuffix(lower, ".pyo") || strings.HasSuffix(lower, ".swp") ||
			strings.HasSuffix(lower, ".swo") || strings.HasSuffix(lower, ".tmp") ||
			strings.HasSuffix(lower, "~") || strings.HasPrefix(lower, ".#") {
			return true
		}
	}
	return false
}

func isControlDirectory(name string) bool {
	return name == stagingDirectoryName || name == trashDirectoryName || name == quarantineDirectoryName
}

func isRevisionName(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func revisionKey(pluginID, revision string) string {
	return pluginID + "/" + revision
}

func removeEmptyParents(directory, stop string) {
	stop, _ = filepath.Abs(stop)
	for {
		directory, _ = filepath.Abs(directory)
		if directory == stop || directory == filepath.Dir(directory) {
			return
		}
		if err := os.Remove(directory); err != nil {
			return
		}
		directory = filepath.Dir(directory)
	}
}

func removeDirectoryIfEmpty(directory string) error {
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return os.Remove(directory)
	}
	return nil
}
