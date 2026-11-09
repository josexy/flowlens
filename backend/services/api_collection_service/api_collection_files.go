package apicollectionservice

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/google/uuid"
	"github.com/josexy/flowlens/backend/pkg/fs"
	"github.com/josexy/flowlens/backend/pkg/logger"
)

func materializeSavedHTTPRequestFiles(request *SavedHTTPRequest) ([]string, bool, error) {
	if request == nil {
		return nil, false, nil
	}
	var createdFiles []string
	changed := false

	bodyFile, createdFile, didChange, err := materializeSavedFile(request.BodyFile)
	if err != nil {
		cleanupCollectionFiles(createdFiles)
		return nil, false, err
	}
	if didChange {
		request.BodyFile = bodyFile
		createdFiles = append(createdFiles, createdFile)
		changed = true
	}

	for _, item := range request.BodyFormData {
		if item == nil {
			continue
		}
		formFile, createdFile, didChange, err := materializeSavedFile(item.File)
		if err != nil {
			cleanupCollectionFiles(createdFiles)
			return nil, false, err
		}
		if didChange {
			item.File = formFile
			createdFiles = append(createdFiles, createdFile)
			changed = true
		}
	}

	return createdFiles, changed, nil
}

func materializeSavedWebSocketRequestFiles(request *SavedWebSocketRequest) ([]string, bool, error) {
	if request == nil {
		return nil, false, nil
	}
	draftFile, createdFile, changed, err := materializeSavedFile(request.DraftFile)
	if err != nil {
		return nil, false, err
	}
	if !changed {
		return nil, false, nil
	}
	request.DraftFile = draftFile
	return []string{createdFile}, true, nil
}

func materializeSavedFile(file *SavedFile) (*SavedFile, string, bool, error) {
	if file == nil || strings.TrimSpace(file.Path) == "" || !isRequestDraftCacheFilePath(file.Path) {
		return file, "", false, nil
	}

	copiedFile, err := copySavedFileToCollectionFiles(file)
	if err != nil {
		return nil, "", false, err
	}
	return copiedFile, copiedFile.Path, true, nil
}

func copySavedFileToCollectionFiles(file *SavedFile) (_ *SavedFile, err error) {
	sourcePath := strings.TrimSpace(file.Path)
	source, err := os.Open(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("copy api collection file %q: %w", sourcePath, err)
	}
	defer source.Close()

	sourceInfo, err := source.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat api collection file %q: %w", sourcePath, err)
	}
	if sourceInfo.IsDir() {
		return nil, fmt.Errorf("copy api collection file %q: source is a directory", sourcePath)
	}

	filesDir, err := getAPICollectionFilesStoragePath()
	if err != nil {
		return nil, err
	}
	if err := fs.EnsurePrivateDir(filesDir); err != nil {
		return nil, err
	}

	displayName := savedFileDisplayName(file)
	target, err := os.CreateTemp(filesDir, ".pending-*")
	if err != nil {
		return nil, err
	}
	pendingPath := target.Name()
	finalPath := ""
	defer func() {
		if err != nil {
			_ = os.Remove(pendingPath)
			if finalPath != "" {
				_ = os.Remove(finalPath)
			}
		}
	}()

	written, err := io.Copy(target, source)
	if err != nil {
		_ = target.Close()
		return nil, err
	}
	if err = target.Sync(); err != nil {
		_ = target.Close()
		return nil, err
	}
	if err = target.Close(); err != nil {
		return nil, err
	}
	finalPath = filepath.Join(filesDir, uuid.NewString()+filepath.Ext(displayName))
	if err = os.Rename(pendingPath, finalPath); err != nil {
		return nil, err
	}

	return &SavedFile{
		Path: finalPath,
		Name: displayName,
		Size: written,
	}, nil
}

func storedCollectionFilePath(runtimePath string) (string, error) {
	trimmedPath := strings.TrimSpace(runtimePath)
	if trimmedPath == "" || !isAPICollectionFilePath(trimmedPath) {
		return trimmedPath, nil
	}
	collectionDir, err := getAPICollectionStorageDir()
	if err != nil {
		return "", err
	}
	absPath, err := filepath.Abs(trimmedPath)
	if err != nil {
		return "", err
	}
	relativePath, err := filepath.Rel(collectionDir, absPath)
	if err != nil {
		return "", err
	}
	if relativePath == "." || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) || filepath.IsAbs(relativePath) {
		return "", fmt.Errorf("api collection file %q is outside storage directory", runtimePath)
	}
	return filepath.ToSlash(relativePath), nil
}

func runtimeCollectionFilePath(storedPath string) (string, error) {
	trimmedPath := strings.TrimSpace(storedPath)
	if trimmedPath == "" || filepath.IsAbs(trimmedPath) {
		return trimmedPath, nil
	}
	nativePath := filepath.FromSlash(trimmedPath)
	if nativePath != apiCollectionFilesDirName && !strings.HasPrefix(nativePath, apiCollectionFilesDirName+string(filepath.Separator)) {
		return trimmedPath, nil
	}
	collectionDir, err := getAPICollectionStorageDir()
	if err != nil {
		return "", err
	}
	runtimePath := filepath.Join(collectionDir, nativePath)
	if !isAPICollectionFilePath(runtimePath) {
		return "", fmt.Errorf("invalid stored api collection file path %q", storedPath)
	}
	return runtimePath, nil
}

func prepareHTTPRequestForStorage(request *SavedHTTPRequest) (*SavedHTTPRequest, error) {
	cloned := cloneSavedHTTPRequest(request)
	if cloned == nil {
		return nil, nil
	}
	if err := prepareSavedFileForStorage(cloned.BodyFile); err != nil {
		return nil, err
	}
	for _, item := range cloned.BodyFormData {
		if item != nil {
			if err := prepareSavedFileForStorage(item.File); err != nil {
				return nil, err
			}
		}
	}
	return cloned, nil
}

func prepareWebSocketRequestForStorage(request *SavedWebSocketRequest) (*SavedWebSocketRequest, error) {
	cloned := cloneSavedWebSocketRequest(request)
	if cloned == nil {
		return nil, nil
	}
	if err := prepareSavedFileForStorage(cloned.DraftFile); err != nil {
		return nil, err
	}
	return cloned, nil
}

func prepareSavedFileForStorage(file *SavedFile) error {
	if file == nil {
		return nil
	}
	path, err := storedCollectionFilePath(file.Path)
	if err != nil {
		return err
	}
	runtimePath, err := runtimeCollectionFilePath(path)
	if err != nil {
		return err
	}
	if isAPICollectionFilePath(runtimePath) {
		if err := validateManagedCollectionFile(runtimePath); err != nil {
			return err
		}
	}
	file.Path = path
	return nil
}

func hydrateHTTPRequestFiles(request *SavedHTTPRequest) error {
	if request == nil {
		return nil
	}
	if err := hydrateSavedFile(request.BodyFile); err != nil {
		return err
	}
	for _, item := range request.BodyFormData {
		if item != nil {
			if err := hydrateSavedFile(item.File); err != nil {
				return err
			}
		}
	}
	return nil
}

func hydrateWebSocketRequestFiles(request *SavedWebSocketRequest) error {
	if request == nil {
		return nil
	}
	return hydrateSavedFile(request.DraftFile)
}

func hydrateSavedFile(file *SavedFile) error {
	if file == nil {
		return nil
	}
	path, err := runtimeCollectionFilePath(file.Path)
	if err != nil {
		return err
	}
	file.Path = path
	return nil
}

func reconcileCollectionFiles(referenced map[string]struct{}) error {
	for path := range referenced {
		if err := validateManagedCollectionFile(path); err != nil {
			return err
		}
	}
	filesDir, err := getAPICollectionFilesStoragePath()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(filesDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(filesDir, entry.Name())
		if _, exists := referenced[normalizedFilePathKey(path)]; exists {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			logger.G().Warnf("API collection remove orphan file failed: path=%s error=%v", path, err)
		}
	}
	return nil
}

func validateManagedCollectionFile(path string) error {
	if !isAPICollectionFilePath(path) {
		return fmt.Errorf("managed api collection file %q is outside storage directory", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat managed api collection file %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("managed api collection file %q is not a regular file", path)
	}
	return nil
}

func savedFileDisplayName(file *SavedFile) string {
	if file == nil {
		return "file.bin"
	}
	name := strings.TrimSpace(filepath.Base(file.Name))
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = strings.TrimSpace(filepath.Base(file.Path))
	}
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "file.bin"
	}
	return name
}

func collectCollectionRequestManagedFilePaths(request *APICollectionRequest, paths map[string]struct{}) {
	if request == nil {
		return
	}
	if request.HTTP != nil {
		collectSavedFileManagedPath(request.HTTP.BodyFile, paths)
		for _, item := range request.HTTP.BodyFormData {
			if item != nil {
				collectSavedFileManagedPath(item.File, paths)
			}
		}
	}
	if request.WebSocket != nil {
		collectSavedFileManagedPath(request.WebSocket.DraftFile, paths)
	}
}

func collectSavedFileManagedPath(file *SavedFile, paths map[string]struct{}) {
	if file == nil || !isAPICollectionFilePath(file.Path) {
		return
	}
	paths[normalizedFilePathKey(file.Path)] = struct{}{}
}

func cleanupCollectionFiles(paths []string) {
	for _, path := range paths {
		if isAPICollectionFilePath(path) {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				logger.G().Warnf("API collection cleanup file failed: path=%s error=%v", path, err)
			}
		}
	}
}

func removeUnreferencedCollectionFiles(previous map[string]struct{}, current map[string]struct{}) {
	for path := range previous {
		if _, stillReferenced := current[path]; stillReferenced {
			continue
		}
		if isAPICollectionFilePath(path) {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				logger.G().Warnf("API collection remove unreferenced file failed: path=%s error=%v", path, err)
			}
		}
	}
}

func isRequestDraftCacheFilePath(path string) bool {
	baseDir, err := fs.GetBaseStorageDir()
	if err != nil {
		return false
	}
	return isPathInsideDir(path, filepath.Join(baseDir, "request-draft-cache"))
}

func isAPICollectionFilePath(path string) bool {
	filesDir, err := getAPICollectionFilesStoragePath()
	if err != nil {
		return false
	}
	return isPathInsideDir(path, filesDir)
}

func isPathInsideDir(path string, dir string) bool {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return false
	}

	absPath, err := filepath.Abs(trimmedPath)
	if err != nil {
		return false
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absDir, absPath)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func normalizedFilePathKey(path string) string {
	key := strings.TrimSpace(path)
	if absPath, err := filepath.Abs(key); err == nil {
		key = absPath
	}
	key = filepath.Clean(key)
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	return key
}
