package proxyservice

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/josexy/flowlens/backend/pkg/fs"
	"github.com/josexy/flowlens/backend/pkg/logger"
)

const (
	historyBackupSuffix           = ".bak"
	historyTempSeparator          = ".tmp-"
	historyFlushStageWriteIndex   = "write-index"
	historyFlushStageBeforeCommit = "before-commit"
)

type historyFilePairPaths struct {
	data  string
	index string
}

func (p historyFilePairPaths) values() []string {
	return []string{p.data, p.index}
}

func (p historyFilePairPaths) backups() historyFilePairPaths {
	return historyFilePairPaths{
		data:  p.data + historyBackupSuffix,
		index: p.index + historyBackupSuffix,
	}
}

func (p historyFilePairPaths) temps(token string) historyFilePairPaths {
	return historyFilePairPaths{
		data:  p.data + historyTempSeparator + token,
		index: p.index + historyTempSeparator + token,
	}
}

func (s *ProxyService) historyRename(oldPath, newPath string) error {
	if s.renameHistoryFile != nil {
		return s.renameHistoryFile(oldPath, newPath)
	}
	return os.Rename(oldPath, newPath)
}

func (s *ProxyService) historyFlushCheckpoint(stage string) error {
	if s.historyFlushStageHook == nil {
		return nil
	}
	return s.historyFlushStageHook(stage)
}

func (s *ProxyService) writeAndCommitHistoryFilePair(
	targets historyFilePairPaths,
	write func(dataFile, indexFile *os.File) error,
) error {
	if err := recoverHistoryFilePair(targets, s.historyRename); err != nil {
		return fmt.Errorf("recover previous history transaction: %w", err)
	}

	temps := targets.temps(uuid.NewString())
	dataFile, err := openPrivateHistoryTemp(temps.data)
	if err != nil {
		return fmt.Errorf("create history data temp: %w", err)
	}
	indexFile, err := openPrivateHistoryTemp(temps.index)
	if err != nil {
		_ = dataFile.Close()
		_ = os.Remove(temps.data)
		return fmt.Errorf("create history index temp: %w", err)
	}
	defer func() {
		_ = os.Remove(temps.data)
		_ = os.Remove(temps.index)
	}()

	if err := write(dataFile, indexFile); err != nil {
		closeErr := closeHistoryTempFiles(false, dataFile, indexFile)
		return errors.Join(err, closeErr)
	}
	if err := closeHistoryTempFiles(true, dataFile, indexFile); err != nil {
		return fmt.Errorf("sync history temp files: %w", err)
	}
	if err := s.historyFlushCheckpoint(historyFlushStageBeforeCommit); err != nil {
		return fmt.Errorf("history flush checkpoint %s: %w", historyFlushStageBeforeCommit, err)
	}
	if err := commitHistoryFilePair(targets, temps, s.historyRename); err != nil {
		return fmt.Errorf("commit history file pair: %w", err)
	}
	return nil
}

func openPrivateHistoryTemp(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fs.PrivateFileMode)
	if err != nil {
		return nil, err
	}
	if err := fs.EnsurePrivateFile(path); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, err
	}
	return file, nil
}

func closeHistoryTempFiles(syncFiles bool, files ...*os.File) error {
	var closeErrors []error
	if syncFiles {
		for _, file := range files {
			if err := file.Sync(); err != nil {
				closeErrors = append(closeErrors, fmt.Errorf("sync %s: %w", file.Name(), err))
			}
		}
	}
	for _, file := range files {
		if err := file.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("close %s: %w", file.Name(), err))
		}
	}
	return errors.Join(closeErrors...)
}

func commitHistoryFilePair(
	targets historyFilePairPaths,
	temps historyFilePairPaths,
	rename func(string, string) error,
) error {
	backups := targets.backups()
	targetPaths := targets.values()
	tempPaths := temps.values()
	backupPaths := backups.values()
	backedUp := make([]bool, len(targetPaths))

	rollback := func(commitErr error) error {
		removeCanonical := []bool{true, true}
		restoreErr := restoreHistoryBackups(targets, backedUp, removeCanonical, rename)
		return errors.Join(commitErr, restoreErr)
	}

	for i, targetPath := range targetPaths {
		exists, err := privateRegularFileExists(targetPath)
		if err != nil {
			restoreErr := restoreHistoryBackups(targets, backedUp, backedUp, rename)
			return errors.Join(fmt.Errorf("prepare existing history file %s: %w", targetPath, err), restoreErr)
		}
		if !exists {
			continue
		}
		if err := rename(targetPath, backupPaths[i]); err != nil {
			restoreErr := restoreHistoryBackups(targets, backedUp, backedUp, rename)
			return errors.Join(fmt.Errorf("back up %s: %w", targetPath, err), restoreErr)
		}
		backedUp[i] = true
	}
	if err := syncHistoryDirectory(filepath.Dir(targets.data)); err != nil {
		return rollback(fmt.Errorf("sync history backups: %w", err))
	}

	for i := range tempPaths {
		if err := rename(tempPaths[i], targetPaths[i]); err != nil {
			return rollback(fmt.Errorf("install %s: %w", targetPaths[i], err))
		}
	}
	if err := syncHistoryDirectory(filepath.Dir(targets.data)); err != nil {
		return rollback(fmt.Errorf("sync installed history pair: %w", err))
	}

	for i, backupPath := range backupPaths {
		if !backedUp[i] {
			continue
		}
		if err := os.Remove(backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			logger.G().Warnf("Remove committed history backup failed: path=%s error=%v", backupPath, err)
		}
	}
	if err := syncHistoryDirectory(filepath.Dir(targets.data)); err != nil {
		logger.G().Warnf("Sync history directory after backup cleanup failed: %v", err)
	}
	return nil
}

func restoreHistoryBackups(
	targets historyFilePairPaths,
	restoreMask []bool,
	removeCanonical []bool,
	rename func(string, string) error,
) error {
	backups := targets.backups()
	targetPaths := targets.values()
	backupPaths := backups.values()
	restoreTemps := make([]string, len(targetPaths))
	for i := range targetPaths {
		if i >= len(restoreMask) || !restoreMask[i] {
			continue
		}
		// Reuse the managed temp protocol so startup recovery and explicit
		// deletion can clean a crash at any restore stage.
		restoreTemps[i] = targetPaths[i] + historyTempSeparator + uuid.NewString()
		if err := copyPrivateSyncedFile(backupPaths[i], restoreTemps[i]); err != nil {
			removeHistoryRestoreTemps(restoreTemps)
			return fmt.Errorf("prepare restore copy for %s: %w", targetPaths[i], err)
		}
	}
	defer removeHistoryRestoreTemps(restoreTemps)

	for i, targetPath := range targetPaths {
		if i >= len(removeCanonical) || !removeCanonical[i] {
			continue
		}
		if err := os.Remove(targetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove incomplete canonical %s: %w", targetPath, err)
		}
	}

	installed := make([]bool, len(targetPaths))
	for i, restoreTemp := range restoreTemps {
		if restoreTemp == "" {
			continue
		}
		if err := rename(restoreTemp, targetPaths[i]); err != nil {
			var cleanupErrors []error
			for installedIndex, wasInstalled := range installed {
				if !wasInstalled {
					continue
				}
				if removeErr := os.Remove(targetPaths[installedIndex]); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
					cleanupErrors = append(cleanupErrors, removeErr)
				}
			}
			return errors.Join(fmt.Errorf("install restored %s: %w", targetPaths[i], err), errors.Join(cleanupErrors...))
		}
		installed[i] = true
	}
	if err := syncHistoryDirectory(filepath.Dir(targets.data)); err != nil {
		return fmt.Errorf("sync restored history pair: %w", err)
	}

	var cleanupErrors []error
	for i, shouldRestore := range restoreMask {
		if !shouldRestore {
			continue
		}
		if err := os.Remove(backupPaths[i]); err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("remove restored backup %s: %w", backupPaths[i], err))
		}
	}
	if len(cleanupErrors) == 0 {
		if err := syncHistoryDirectory(filepath.Dir(targets.data)); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("sync restored backup cleanup: %w", err))
		}
	}
	return errors.Join(cleanupErrors...)
}

func copyPrivateSyncedFile(sourcePath, destinationPath string) error {
	if _, err := privateRegularFileExists(sourcePath); err != nil {
		return err
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	destination, err := openPrivateHistoryTemp(destinationPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(destination, source); err != nil {
		_ = destination.Close()
		_ = os.Remove(destinationPath)
		return err
	}
	if err := closeHistoryTempFiles(true, destination); err != nil {
		_ = os.Remove(destinationPath)
		return err
	}
	return nil
}

func removeHistoryRestoreTemps(paths []string) {
	for _, path := range paths {
		if path != "" {
			_ = os.Remove(path)
		}
	}
}

func privateRegularFileExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("path is not a regular file")
	}
	if err := fs.EnsurePrivateFile(path); err != nil {
		return false, err
	}
	return true, nil
}

func syncHistoryDirectory(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

// RecoverHistoryFileTransactions restores interrupted fixed-backup commits and
// removes uncommitted managed temp files. The active key is excluded because a
// periodic flush may currently own its temp or backup files.
//
//wails:ignore
func RecoverHistoryFileTransactions(historyDir, activeKey string) error {
	return RecoverHistoryFileTransactionsWithActiveCheck(historyDir, func(key string) bool {
		return key == activeKey
	})
}

// RecoverHistoryFileTransactionsWithActiveCheck dynamically excludes the
// capture currently being flushed. The callback is evaluated immediately
// before each destructive recovery action so capture-key rotation cannot make
// a stale scan snapshot delete a new transaction's files.
//
//wails:ignore
func RecoverHistoryFileTransactionsWithActiveCheck(historyDir string, isActive func(string) bool) error {
	entries, err := os.ReadDir(historyDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	backupKeys := make(map[string]struct{})
	type managedHistoryTemp struct {
		key  string
		path string
	}
	var tempFiles []managedHistoryTemp
	var recoveryErrors []error
	for _, entry := range entries {
		key, kind, managed := managedHistoryFile(entry.Name())
		if !managed {
			continue
		}
		if isActive != nil && isActive(key) {
			continue
		}
		path := filepath.Join(historyDir, entry.Name())
		if err := fs.EnsurePrivateFile(path); err != nil {
			recoveryErrors = append(recoveryErrors, fmt.Errorf("secure managed history file %s: %w", path, err))
			continue
		}
		switch kind {
		case "data-backup", "index-backup":
			backupKeys[key] = struct{}{}
		case "data-temp", "index-temp":
			tempFiles = append(tempFiles, managedHistoryTemp{key: key, path: path})
		}
	}

	keys := make([]string, 0, len(backupKeys))
	for key := range backupKeys {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if isActive != nil && isActive(key) {
			continue
		}
		targets := historyFilePairPaths{
			data:  filepath.Join(historyDir, fs.GetHBinFileName(key)),
			index: filepath.Join(historyDir, fs.GetHIdxFileName(key)),
		}
		if err := recoverHistoryFilePair(targets, os.Rename); err != nil {
			recoveryErrors = append(recoveryErrors, fmt.Errorf("recover history %s: %w", key, err))
		}
	}
	for _, tempFile := range tempFiles {
		if isActive != nil && isActive(tempFile.key) {
			continue
		}
		if err := os.Remove(tempFile.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			recoveryErrors = append(recoveryErrors, fmt.Errorf("remove abandoned history temp %s: %w", tempFile.path, err))
		}
	}
	if len(keys) > 0 || len(tempFiles) > 0 {
		if err := syncHistoryDirectory(historyDir); err != nil {
			recoveryErrors = append(recoveryErrors, fmt.Errorf("sync recovered history directory: %w", err))
		}
	}
	return errors.Join(recoveryErrors...)
}

// ManagedHistoryFilePaths returns every file owned by one history key in a
// data-first order. Keeping data, backups, and abandoned temps ahead of index
// files lets deletion stop without removing the retryable index when sensitive
// payload bytes could not be removed.
//
//wails:ignore
func ManagedHistoryFilePaths(historyDir, key string) ([]string, error) {
	if key == "" || filepath.Base(key) != key || strings.ContainsAny(key, `/\\`) {
		return nil, fmt.Errorf("invalid history key: %q", key)
	}
	targets := historyFilePairPaths{
		data:  filepath.Join(historyDir, fs.GetHBinFileName(key)),
		index: filepath.Join(historyDir, fs.GetHIdxFileName(key)),
	}
	backups := targets.backups()
	dataPaths := []string{targets.data, backups.data}
	indexPaths := []string{targets.index, backups.index}

	entries, err := os.ReadDir(historyDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, entry := range entries {
		entryKey, kind, managed := managedHistoryFile(entry.Name())
		if !managed || entryKey != key {
			continue
		}
		path := filepath.Join(historyDir, entry.Name())
		switch kind {
		case "data-temp":
			dataPaths = append(dataPaths, path)
		case "index-temp":
			indexPaths = append(indexPaths, path)
		}
	}
	sort.Strings(dataPaths[2:])
	sort.Strings(indexPaths[2:])
	return append(dataPaths, indexPaths...), nil
}

func recoverHistoryFilePair(targets historyFilePairPaths, rename func(string, string) error) error {
	backups := targets.backups()
	targetPaths := targets.values()
	backupPaths := backups.values()
	targetExists := make([]bool, len(targetPaths))
	backupExists := make([]bool, len(backupPaths))
	for i := range targetPaths {
		var err error
		targetExists[i], err = pathExistsLstat(targetPaths[i])
		if err != nil {
			return err
		}
		backupExists[i], err = pathExistsLstat(backupPaths[i])
		if err != nil {
			return err
		}
	}
	if !backupExists[0] && !backupExists[1] {
		return nil
	}

	if targetExists[0] && targetExists[1] {
		var cleanupErrors []error
		for i, backupPath := range backupPaths {
			if !backupExists[i] {
				continue
			}
			if err := os.Remove(backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("remove completed backup %s: %w", backupPath, err))
			}
		}
		return errors.Join(cleanupErrors...)
	}

	removeCanonical := []bool{backupExists[0], backupExists[1]}
	if backupExists[0] && backupExists[1] {
		// Backup phase completed. Any canonical component belongs to the new
		// transaction or a partial prior restore, so replace the whole pair.
		removeCanonical[0] = true
		removeCanonical[1] = true
	} else if backupExists[1] && !backupExists[0] && targetExists[0] && !targetExists[1] {
		// Data did not exist in the old orphan state (otherwise data-first backup
		// ordering would have produced data.bak). A canonical data file here is a
		// partially installed new component and must not be mixed with old index.
		removeCanonical[0] = true
	}
	return restoreHistoryBackups(targets, backupExists, removeCanonical, rename)
}

func pathExistsLstat(path string) (bool, error) {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func managedHistoryFile(name string) (key, kind string, ok bool) {
	for _, candidate := range []struct {
		suffix string
		kind   string
	}{
		{suffix: fs.HBIN_SUFFIX + historyBackupSuffix, kind: "data-backup"},
		{suffix: fs.HIDX_SUFFIX + historyBackupSuffix, kind: "index-backup"},
		{suffix: fs.HBIN_SUFFIX, kind: "data"},
		{suffix: fs.HIDX_SUFFIX, kind: "index"},
	} {
		if before, ok0 := strings.CutSuffix(name, candidate.suffix); ok0 {
			key = before
			return key, candidate.kind, key != ""
		}
	}
	for _, candidate := range []struct {
		marker string
		kind   string
	}{
		{marker: fs.HBIN_SUFFIX + ".tmp", kind: "data-temp"},
		{marker: fs.HIDX_SUFFIX + ".tmp", kind: "index-temp"},
	} {
		markerIndex := strings.LastIndex(name, candidate.marker)
		if markerIndex <= 0 {
			continue
		}
		tail := name[markerIndex+len(candidate.marker):]
		if !isManagedHistoryTempTail(tail) {
			continue
		}
		return name[:markerIndex], candidate.kind, true
	}
	return "", "", false
}

func isManagedHistoryTempTail(tail string) bool {
	if tail == "" {
		return false
	}
	if after, ok := strings.CutPrefix(tail, "-"); ok {
		_, err := uuid.Parse(after)
		return err == nil
	}
	for _, char := range tail {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}
