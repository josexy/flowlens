package historyservice

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/josexy/flowlens/backend/pkg/fs"
	"github.com/josexy/flowlens/backend/pkg/logger"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type HistoryService struct {
	app            *application.App
	settingService *settingservice.SettingService
	proxyService   historyProxyService

	storageMu sync.RWMutex
	mu        sync.RWMutex
	indexMap  historyIndexMap

	initIndexMapOnce sync.Once
	maintenanceWG    sync.WaitGroup
	shutdownOnce     sync.Once
}

type historyProxyService interface {
	CurrentHistoryKey() string
	ResendRequestWithTrafficEntry(
		context.Context,
		proxyservice.ResendConfig,
		*proxyservice.TrafficEntry,
		[]byte,
	) (proxyservice.ResendResult, error)
}

func New(
	settingService *settingservice.SettingService,
	proxyService historyProxyService,
) *HistoryService {
	return &HistoryService{
		settingService: settingService,
		proxyService:   proxyService,
		indexMap:       make(historyIndexMap),
	}
}

func (s *HistoryService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	s.app = application.Get()
	s.initIndexMapOnce.Do(func() {
		s.maintenanceWG.Go(func() {
			s.runStartupMaintenance()
		})
	})
	return nil
}

func (s *HistoryService) runStartupMaintenance() {
	if config, err := settingservice.GetHistoryRetentionConfig(s.settingService); err != nil {
		logger.G().Warnf("Failed to load history retention config, skipping startup cleanup: %v", err)
	} else if config.Enabled {
		stats, cleanupErr := s.deleteExpiredHistories(time.Now(), config)
		logger.G().Infof(
			"Expired history cleanup completed: scanned=%d deleted=%d failed=%d",
			stats.Scanned,
			stats.Deleted,
			stats.Failed,
		)
		if cleanupErr != nil {
			logger.G().Warnf("Expired history cleanup encountered errors: %v", cleanupErr)
		}
	}

	if err := s.initializeHistoryIndexMap(); err != nil && !os.IsNotExist(err) {
		logger.G().Warnf("Failed to initialize history index map: %v", err)
	}
}

func (s *HistoryService) ServiceShutdown() error {
	return s.Shutdown()
}

//wails:ignore
func (s *HistoryService) Shutdown() error {
	s.shutdownOnce.Do(func() {
		s.maintenanceWG.Wait()
	})
	return nil
}

func (s *HistoryService) initializeHistoryIndexMap() error {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	return s.initializeHistoryIndexMapStorageLocked()
}

func (s *HistoryService) initializeHistoryIndexMapStorageLocked() error {
	historyStorageDir, err := getHistoryStoragePath()
	if err != nil {
		return err
	}
	if err := fs.EnsurePrivateDir(historyStorageDir); err != nil {
		return fmt.Errorf("secure history storage directory: %w", err)
	}
	isActiveHistory := func(key string) bool {
		return s.proxyService != nil && s.proxyService.CurrentHistoryKey() == key
	}
	var storageErrors []error
	recoveryFailed := false
	if err := proxyservice.RecoverHistoryFileTransactionsWithActiveCheck(historyStorageDir, isActiveHistory); err != nil {
		// Continue indexing unaffected pairs. Recovery keeps fixed-name backups
		// whenever it cannot safely restore a canonical pair, so a later refresh
		// can retry without silently discarding the previous capture.
		logger.G().Warnf("History transaction recovery completed with errors: %v", err)
		storageErrors = append(storageErrors, err)
		recoveryFailed = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dires, err := os.ReadDir(historyStorageDir)
	if err != nil {
		return errors.Join(errors.Join(storageErrors...), err)
	}
	for _, de := range dires {
		if de.IsDir() || !strings.HasSuffix(de.Name(), fs.HIDX_SUFFIX) {
			continue
		}
		key := strings.TrimSuffix(de.Name(), fs.HIDX_SUFFIX)
		// A two-file flush temporarily moves the active data file to its backup
		// before doing the same for the index. Never interpret that commit window
		// as an orphaned index. Re-read the key as well as using the scan snapshot
		// so a capture rotation followed by a new flush is covered.
		if isActiveHistory(key) {
			continue
		}
		path := filepath.Join(historyStorageDir, de.Name())
		// check hbin file exists
		hbinPath := filepath.Join(historyStorageDir, fs.GetHBinFileName(key))
		if !fs.PathExists(hbinPath) {
			// A failed recovery may have left the only complete data copy in a
			// backup. Keep its canonical index so a later retry can reconstruct
			// the original pair instead of turning it into an unrecoverable orphan.
			if recoveryFailed {
				continue
			}
			if err := fs.DeleteFile(path); err != nil && !os.IsNotExist(err) {
				storageErrors = append(storageErrors, fmt.Errorf("delete orphan history index %s: %w", path, err))
				logger.G().Warnf("Delete orphan history index failed: path=%s error=%v", path, err)
			} else {
				logger.G().Warnf("History index file %s exists but bin file not found, deleted the index file", path)
			}
			continue
		}
		if err := fs.EnsurePrivateFile(path); err != nil {
			logger.G().Warnf("Skip history with insecure index permissions: key=%s error=%v", key, err)
			storageErrors = append(storageErrors, fmt.Errorf("secure history index %s: %w", key, err))
			continue
		}
		if err := fs.EnsurePrivateFile(hbinPath); err != nil {
			logger.G().Warnf("Skip history with insecure data permissions: key=%s error=%v", key, err)
			storageErrors = append(storageErrors, fmt.Errorf("secure history data %s: %w", key, err))
			continue
		}
		if err := s.initializeIndexMapLocked(key, path); err != nil {
			logger.G().Errorf("Failed to initialize index map for %s: %v", key, err)
			continue
		}
	}
	return errors.Join(storageErrors...)
}

func (s *HistoryService) initializeIndexMapLocked(key, path string) error {
	if s.proxyService != nil && s.proxyService.CurrentHistoryKey() == key {
		// current capturing history, skip initializing index map to avoid potential concurrent access issue
		return nil
	}
	if existing := s.indexMap[key]; existing != nil {
		return nil
	}
	metadata, err := loadHistoryMetadata(filepath.Dir(path), key)
	if err != nil {
		return err
	}
	hindexFile, err := os.Open(path)
	if err != nil {
		return err
	}
	defer hindexFile.Close()
	entries := newOrderedIndexList()
	if err := initializeIndexMap(hindexFile, entries); err != nil {
		return err
	}
	s.indexMap[key] = &historyFileIndex{
		formatVersion: metadata.FormatVersion,
		entries:       entries,
	}
	return nil
}

func (s *HistoryService) GetHistory(key string) ([]*proxyservice.TrafficEntry, error) {
	s.storageMu.RLock()
	defer s.storageMu.RUnlock()
	s.mu.RLock()
	fileIndex := s.indexMap[key]
	s.mu.RUnlock()
	if fileIndex == nil || fileIndex.entries == nil {
		return nil, fmt.Errorf("history not found: %s", key)
	}
	idxMap := fileIndex.entries

	historyStorageDir, err := getHistoryStoragePath()
	if err != nil {
		return nil, err
	}
	start := time.Now()
	hbinFile, err := os.Open(filepath.Join(historyStorageDir, fs.GetHBinFileName(key)))
	if err != nil {
		return nil, err
	}
	defer hbinFile.Close()

	entries := make([]*proxyservice.TrafficEntry, 0, idxMap.Len())
	var lastErr error
	idxMap.ForEachValue(func(value historyIndex) bool {
		if _, lastErr = hbinFile.Seek(int64(value.headerIndex), io.SeekStart); lastErr != nil {
			return false
		}
		var entry *proxyservice.TrafficEntry
		entry, lastErr = proxyservice.DecodeTrafficEntryWithVersion(hbinFile, fileIndex.formatVersion)
		if lastErr != nil {
			return false
		}
		entries = append(entries, entry)
		return true
	})
	logger.G().Infof("Fetch History traffic entries[%d/%d] for %s took %v", len(entries), idxMap.Len(), key, time.Since(start))
	return entries, lastErr
}

func (s *HistoryService) GetHistoryTrafficBodyView(key string, id uint64) (*proxyservice.TrafficBodyView, error) {
	s.storageMu.RLock()
	defer s.storageMu.RUnlock()
	s.mu.RLock()
	fileIndex := s.indexMap[key]
	s.mu.RUnlock()
	if fileIndex == nil || fileIndex.entries == nil {
		return nil, fmt.Errorf("history not found: %s", key)
	}
	idxMap := fileIndex.entries
	idx, ok := idxMap.Get(id)
	if !ok {
		return nil, fmt.Errorf("traffic entry not found: %d", id)
	}
	historyStorageDir, err := getHistoryStoragePath()
	if err != nil {
		return nil, err
	}
	hbinFile, err := os.Open(filepath.Join(historyStorageDir, fs.GetHBinFileName(key)))
	if err != nil {
		return nil, err
	}
	defer hbinFile.Close()
	if _, err := hbinFile.Seek(int64(idx.bodyIndex), io.SeekStart); err != nil {
		return nil, err
	}
	return proxyservice.DecodeTrafficBody(hbinFile)
}

func (s *HistoryService) getSingleHistoryEntryAndBodyReader(key string, id uint64) (*proxyservice.TrafficEntry, []byte, error) {
	s.storageMu.RLock()
	defer s.storageMu.RUnlock()
	s.mu.RLock()
	fileIndex := s.indexMap[key]
	s.mu.RUnlock()
	if fileIndex == nil || fileIndex.entries == nil {
		return nil, nil, fmt.Errorf("history not found: %s", key)
	}
	idxMap := fileIndex.entries

	historyStorageDir, err := getHistoryStoragePath()
	if err != nil {
		return nil, nil, err
	}
	hbinFile, err := os.Open(filepath.Join(historyStorageDir, fs.GetHBinFileName(key)))
	if err != nil {
		return nil, nil, err
	}
	defer hbinFile.Close()
	idx, ok := idxMap.Get(id)
	if !ok {
		return nil, nil, fmt.Errorf("traffic entry not found: %d", id)
	}
	if _, err := hbinFile.Seek(int64(idx.headerIndex), io.SeekStart); err != nil {
		return nil, nil, err
	}
	te, err := proxyservice.DecodeTrafficEntryWithVersion(hbinFile, fileIndex.formatVersion)
	if err != nil {
		return nil, nil, err
	}
	if _, err := hbinFile.Seek(int64(idx.bodyIndex), io.SeekStart); err != nil {
		return nil, nil, err
	}
	reqBodyBytes, _, err := proxyservice.DecodeTrafficRequestBody(hbinFile)
	if err != nil {
		return nil, nil, err
	}
	return te, reqBodyBytes, nil
}

func (s *HistoryService) ResendRequest(callCtx context.Context, key string, id uint64, cfg proxyservice.ResendConfig) (proxyservice.ResendResult, error) {
	te, bodyBytes, err := s.getSingleHistoryEntryAndBodyReader(key, id)
	if err != nil {
		return proxyservice.ResendResult{}, err
	}
	if s.proxyService == nil {
		return proxyservice.ResendResult{}, errors.New("proxy service is not available")
	}
	result, err := s.proxyService.ResendRequestWithTrafficEntry(callCtx, cfg, te, bodyBytes)
	if errors.Is(err, context.Canceled) {
		return result, nil
	}
	return result, err
}

func (s *HistoryService) DeleteHistory(key string) error {
	if err := validateHistoryKey(key); err != nil {
		return err
	}

	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.indexMap[key]; !ok {
		return fmt.Errorf("history not found: %s", key)
	}

	logger.G().Infof("Delete history requested: key=%s", key)
	historyStorageDir, err := getHistoryStoragePath()
	if err != nil {
		return err
	}
	paths, err := historyFilePaths(historyStorageDir, key)
	if err != nil {
		return err
	}
	deletedFiles, deleteErr := deleteHistoryFiles(paths...)
	if deleteErr != nil {
		logger.G().Warnf("Delete history completed with errors: key=%s files=%d error=%v", key, deletedFiles, deleteErr)
		return deleteErr
	}
	delete(s.indexMap, key)
	logger.G().Infof("Delete history completed: key=%s files=%d", key, deletedFiles)
	return nil
}

func validateHistoryKey(key string) error {
	if err := validateHistoryPathKey(key); err != nil {
		return err
	}
	parsed, err := uuid.Parse(key)
	if err != nil || parsed.String() != key {
		return fmt.Errorf("invalid history key: %q", key)
	}
	return nil
}

func validateHistoryPathKey(key string) error {
	if key == "" || key == "." || key == ".." || filepath.IsAbs(key) || filepath.Base(key) != key || strings.ContainsAny(key, `/\\`) {
		return fmt.Errorf("invalid history key: %q", key)
	}
	return nil
}

func historyFilePaths(historyStorageDir, key string) ([]string, error) {
	if err := validateHistoryPathKey(key); err != nil {
		return nil, err
	}
	absDir, err := filepath.Abs(filepath.Clean(historyStorageDir))
	if err != nil {
		return nil, fmt.Errorf("resolve history storage directory: %w", err)
	}
	paths, err := proxyservice.ManagedHistoryFilePaths(absDir, key)
	if err != nil {
		return nil, fmt.Errorf("resolve managed history files: %w", err)
	}
	for _, path := range paths {
		relative, err := filepath.Rel(absDir, path)
		if err != nil {
			return nil, fmt.Errorf("resolve history file containment: %w", err)
		}
		if relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("history file escapes storage directory: %q", key)
		}
	}
	return paths, nil
}

func deleteHistoryFiles(paths ...string) (int, error) {
	deletedFiles := 0
	for _, path := range paths {
		if !fs.PathExists(path) {
			continue
		}
		logger.G().Debugf("Deleting history file: %s", path)
		if err := fs.DeleteFile(path); err != nil && !os.IsNotExist(err) {
			// historyFilePaths orders the data file before its index. Stop on
			// failure so a retryable index is not removed while sensitive data
			// remains on disk.
			return deletedFiles, fmt.Errorf("delete %s: %w", path, err)
		}
		deletedFiles++
	}
	return deletedFiles, nil
}

func (s *HistoryService) ClearHistories() error {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	refreshErr := s.initializeHistoryIndexMapStorageLocked()
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := slices.Collect(maps.Keys(s.indexMap))

	logger.G().Infof("Clear histories requested: keys=%d", len(keys))
	historyStorageDir, err := getHistoryStoragePath()
	if err != nil {
		return err
	}
	deletedFiles := 0
	var clearErrors []error
	if refreshErr != nil {
		clearErrors = append(clearErrors, fmt.Errorf("refresh history index before clearing: %w", refreshErr))
	}
	for _, key := range keys {
		paths, pathErr := historyFilePaths(historyStorageDir, key)
		if pathErr != nil {
			clearErrors = append(clearErrors, fmt.Errorf("clear history %q: %w", key, pathErr))
			continue
		}
		deleted, deleteErr := deleteHistoryFiles(paths...)
		deletedFiles += deleted
		if deleteErr != nil {
			logger.G().Warnf("Clear history files failed: key=%s error=%v", key, deleteErr)
			clearErrors = append(clearErrors, fmt.Errorf("clear history %s: %w", key, deleteErr))
			continue
		}
		delete(s.indexMap, key)
	}
	clearErr := errors.Join(clearErrors...)
	if clearErr != nil {
		logger.G().Warnf("Clear histories completed with errors: keys=%d files=%d error=%v", len(keys), deletedFiles, clearErr)
		return clearErr
	}
	logger.G().Infof("Clear histories completed: keys=%d files=%d", len(keys), deletedFiles)
	return nil
}

func (s *HistoryService) ListHistoryKeys() ([]*proxyservice.HistoryMetadata, error) {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	historyStorageDir, err := getHistoryStoragePath()
	if err != nil {
		return nil, err
	}

	if err := s.initializeHistoryIndexMapStorageLocked(); err != nil && !os.IsNotExist(err) {
		logger.G().Warnf("Failed to refresh history index map before listing histories: %v", err)
	}

	s.mu.RLock()
	keys := slices.Collect(maps.Keys(s.indexMap))
	s.mu.RUnlock()

	mds := make([]*proxyservice.HistoryMetadata, 0, len(keys))
	for _, key := range keys {
		md, err := loadHistoryMetadata(historyStorageDir, key)
		if err != nil {
			logger.G().Errorf("Failed to load history metadata for %s: %v", key, err)
			continue
		}
		mds = append(mds, md)
	}
	return mds, nil
}
