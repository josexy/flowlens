package historyservice

import (
	"bufio"
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
	"github.com/josexy/flowlens/internal/pkg/fs"
	"github.com/josexy/flowlens/internal/pkg/logger"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
)

type HistoryService struct {
	settingService *settingservice.SettingService
	proxyService   historyProxyService

	storageMu sync.RWMutex
	// mu guards histories and indexes.
	mu sync.RWMutex
	// histories holds the keys of the saved histories found by storage scans.
	// It keeps no per-entry data.
	histories map[string]struct{}
	// indexes keeps the entry indexes of the most recently opened or exported
	// histories, loaded on demand.
	indexes historyIndexCache

	startupOnce    sync.Once
	maintenanceWG  sync.WaitGroup
	shutdownOnce   sync.Once
	importMu       sync.Mutex
	importWG       sync.WaitGroup
	imports        map[uint64]context.CancelFunc
	importSequence uint64
	importsClosed  bool
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
	}
}

func (s *HistoryService) ServiceStartup(ctx context.Context) error {
	s.startupOnce.Do(func() {
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

	if err := s.refreshHistories(); err != nil && !os.IsNotExist(err) {
		logger.G().Warnf("Failed to scan saved histories: %v", err)
	}
}

func (s *HistoryService) ServiceShutdown() error {
	return s.Shutdown()
}

func (s *HistoryService) Shutdown() error {
	s.shutdownOnce.Do(func() {
		s.importMu.Lock()
		s.importsClosed = true
		for _, cancel := range s.imports {
			cancel()
		}
		s.importMu.Unlock()
		s.importWG.Wait()
		s.maintenanceWG.Wait()
	})
	return nil
}

// refreshHistories scans the history directory for saved histories. It reads
// only each history's metadata; entry indexes load when a history is opened or
// exported.
func (s *HistoryService) refreshHistories() error {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	return s.refreshHistoriesStorageLocked()
}

func (s *HistoryService) refreshHistoriesStorageLocked() error {
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
		if _, known := s.histories[key]; known {
			continue
		}
		if _, err := loadHistoryMetadata(historyStorageDir, key); err != nil {
			logger.G().Errorf("Skip history with unreadable metadata: key=%s error=%v", key, err)
			continue
		}
		if err := checkHistoryIndexFile(path); err != nil {
			logger.G().Errorf("Skip history with an invalid index: key=%s error=%v", key, err)
			continue
		}
		s.addHistoryLocked(key)
	}
	return errors.Join(storageErrors...)
}

// registerHistory makes a newly written history visible after checking that
// its metadata and index are readable.
func (s *HistoryService) registerHistory(historyStorageDir, key string) error {
	if _, err := loadHistoryMetadata(historyStorageDir, key); err != nil {
		return err
	}
	if err := checkHistoryIndexFile(filepath.Join(historyStorageDir, fs.GetHIdxFileName(key))); err != nil {
		return err
	}
	s.mu.Lock()
	s.addHistoryLocked(key)
	s.mu.Unlock()
	return nil
}

func (s *HistoryService) addHistoryLocked(key string) {
	if s.histories == nil {
		s.histories = make(map[string]struct{})
	}
	s.histories[key] = struct{}{}
}

// forgetHistoryLocked removes a deleted history and its cached index.
func (s *HistoryService) forgetHistoryLocked(key string) {
	delete(s.histories, key)
	s.indexes.evict(key)
}

// historyIndex returns the entry index of a saved history, loading it when it
// is not cached. Callers hold storageMu so the history cannot be deleted while
// they use the index.
func (s *HistoryService) historyIndex(key string) (*historyFileIndex, error) {
	return s.historyIndexContext(context.Background(), key)
}

func (s *HistoryService) historyIndexContext(ctx context.Context, key string) (*historyFileIndex, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadIndexLocked(ctx, key)
}

func (s *HistoryService) loadIndexLocked(ctx context.Context, key string) (*historyFileIndex, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, known := s.histories[key]; !known {
		return nil, fmt.Errorf("history not found: %s", key)
	}
	if index, ok := s.indexes.get(key); ok {
		return index, nil
	}
	historyStorageDir, err := getHistoryStoragePath()
	if err != nil {
		return nil, err
	}
	metadata, err := loadHistoryMetadata(historyStorageDir, key)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(filepath.Join(historyStorageDir, fs.GetHIdxFileName(key)))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	entries := newOrderedIndexList()
	if err := initializeIndexMap(bufio.NewReader(historyContextReader{ctx: ctx, reader: file}), entries); err != nil {
		return nil, fmt.Errorf("load history index %s: %w", key, err)
	}
	index := &historyFileIndex{formatVersion: metadata.FormatVersion, entries: entries}
	s.indexes.put(key, index)
	return index, nil
}

func (s *HistoryService) GetHistory(key string) ([]*proxyservice.TrafficEntry, error) {
	return s.readHistory(context.Background(), key, false)
}

// ListHistoryEntries keeps the complete searchable table but not every
// record's large headers and certificates. Details/actions use GetHistoryDetail.
func (s *HistoryService) ListHistoryEntries(ctx context.Context, key string) ([]*proxyservice.TrafficEntry, error) {
	return s.readHistory(ctx, key, true)
}

func (s *HistoryService) readHistory(ctx context.Context, key string, summaries bool) ([]*proxyservice.TrafficEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.storageMu.RLock()
	defer s.storageMu.RUnlock()
	fileIndex, err := s.historyIndexContext(ctx, key)
	if err != nil {
		return nil, err
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
	reader := bufio.NewReader(historyContextReader{ctx: ctx, reader: hbinFile})
	decode := proxyservice.DecodeTrafficEntryWithVersion
	if summaries {
		decode = proxyservice.DecodeTrafficEntrySummaryWithVersion
	}
	var lastErr error
	idxMap.ForEachValue(func(value historyIndex) bool {
		if lastErr = ctx.Err(); lastErr != nil {
			return false
		}
		if _, lastErr = hbinFile.Seek(int64(value.headerIndex), io.SeekStart); lastErr != nil {
			return false
		}
		var entry *proxyservice.TrafficEntry
		reader.Reset(historyContextReader{ctx: ctx, reader: hbinFile})
		entry, lastErr = decode(reader, fileIndex.formatVersion)
		if lastErr != nil {
			return false
		}
		entries = append(entries, entry)
		return true
	})
	if lastErr == nil {
		lastErr = ctx.Err()
	}
	logger.G().Infof("Fetch History traffic entries[%d/%d] for %s took %v", len(entries), idxMap.Len(), key, time.Since(start))
	return entries, lastErr
}

// HistoryDetail is a complete record and its payload, kept apart from the
// lightweight list. Entry can remain available if only body decoding fails.
type HistoryDetail struct {
	Entry *proxyservice.TrafficEntry
	Body  *proxyservice.TrafficBodyView
}

func (s *HistoryService) GetHistoryDetail(ctx context.Context, key string, id uint64) (HistoryDetail, error) {
	if err := ctx.Err(); err != nil {
		return HistoryDetail{}, err
	}
	s.storageMu.RLock()
	defer s.storageMu.RUnlock()
	index, err := s.historyIndexContext(ctx, key)
	if err != nil {
		return HistoryDetail{}, err
	}
	position, ok := index.entries.Get(id)
	if !ok {
		return HistoryDetail{}, fmt.Errorf("traffic entry not found: %d", id)
	}
	directory, err := getHistoryStoragePath()
	if err != nil {
		return HistoryDetail{}, err
	}
	file, err := os.Open(filepath.Join(directory, fs.GetHBinFileName(key)))
	if err != nil {
		return HistoryDetail{}, err
	}
	defer file.Close()
	if _, err = file.Seek(int64(position.headerIndex), io.SeekStart); err != nil {
		return HistoryDetail{}, err
	}
	reader := bufio.NewReader(historyContextReader{ctx: ctx, reader: file})
	entry, err := proxyservice.DecodeTrafficEntryWithVersion(reader, index.formatVersion)
	if err != nil {
		return HistoryDetail{}, err
	}
	if entry.ID != id {
		return HistoryDetail{}, fmt.Errorf("history entry ID %d does not match index %d", entry.ID, id)
	}
	detail := HistoryDetail{Entry: entry}
	if entry.Type == "tcp" {
		return detail, ctx.Err()
	}
	if _, err = file.Seek(int64(position.bodyIndex), io.SeekStart); err != nil {
		return detail, err
	}
	reader.Reset(historyContextReader{ctx: ctx, reader: file})
	detail.Body, err = proxyservice.DecodeTrafficBodyWithVersion(reader, index.formatVersion)
	if err == nil {
		err = ctx.Err()
	}
	return detail, err
}

func (s *HistoryService) GetHistoryTrafficBodyView(key string, id uint64) (*proxyservice.TrafficBodyView, error) {
	s.storageMu.RLock()
	defer s.storageMu.RUnlock()
	fileIndex, err := s.historyIndex(key)
	if err != nil {
		return nil, err
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
	return proxyservice.DecodeTrafficBodyWithVersion(hbinFile, fileIndex.formatVersion)
}

func (s *HistoryService) getSingleHistoryEntryAndBodyReader(key string, id uint64) (*proxyservice.TrafficEntry, []byte, error) {
	s.storageMu.RLock()
	defer s.storageMu.RUnlock()
	fileIndex, err := s.historyIndex(key)
	if err != nil {
		return nil, nil, err
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
	reqBodyBytes, _, err := proxyservice.DecodeTrafficRequestBody(hbinFile, fileIndex.formatVersion)
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
	if _, ok := s.histories[key]; !ok {
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
	s.forgetHistoryLocked(key)
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
	refreshErr := s.refreshHistoriesStorageLocked()
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := slices.Collect(maps.Keys(s.histories))

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
		s.forgetHistoryLocked(key)
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

	if err := s.refreshHistoriesStorageLocked(); err != nil && !os.IsNotExist(err) {
		logger.G().Warnf("Failed to refresh saved histories before listing them: %v", err)
	}

	s.mu.RLock()
	keys := slices.Collect(maps.Keys(s.histories))
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
