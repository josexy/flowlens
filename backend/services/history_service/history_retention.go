package historyservice

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/josexy/flowlens/backend/pkg/fs"
	"github.com/josexy/flowlens/backend/pkg/logger"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
)

type historyCleanupStats struct {
	Scanned int
	Deleted int
	Failed  int
}

func (s *HistoryService) deleteExpiredHistories(now time.Time, config settingservice.HistoryRetentionConfig) (historyCleanupStats, error) {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	var stats historyCleanupStats
	if !config.Enabled {
		return stats, nil
	}

	historyStorageDir, err := getHistoryStoragePath()
	if err != nil {
		return stats, err
	}
	entries, err := os.ReadDir(historyStorageDir)
	if os.IsNotExist(err) {
		return stats, nil
	}
	if err != nil {
		return stats, err
	}
	activeKey := ""
	if s.proxyService != nil {
		activeKey = s.proxyService.CurrentHistoryKey()
	}

	var cleanupErrors []error
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), fs.HBIN_SUFFIX) {
			continue
		}
		stats.Scanned++
		key := strings.TrimSuffix(entry.Name(), fs.HBIN_SUFFIX)
		if key == activeKey {
			continue
		}
		metadata, loadErr := loadHistoryMetadata(historyStorageDir, key)
		if loadErr != nil {
			stats.Failed++
			cleanupErrors = append(cleanupErrors, fmt.Errorf("load history metadata %s: %w", key, loadErr))
			logger.G().Warnf("Skip expired history check because metadata could not be loaded: key=%s error=%v", key, loadErr)
			continue
		}
		if metadata.CreatedAt <= 0 {
			stats.Failed++
			invalidErr := fmt.Errorf("history %s has invalid createdAt %d", key, metadata.CreatedAt)
			cleanupErrors = append(cleanupErrors, invalidErr)
			logger.G().Warnf("Skip expired history check because createdAt is invalid: key=%s createdAt=%d", key, metadata.CreatedAt)
			continue
		}
		if metadata.Key == "" || metadata.Key != key {
			stats.Failed++
			invalidErr := fmt.Errorf("history %s has mismatched metadata key %q", key, metadata.Key)
			cleanupErrors = append(cleanupErrors, invalidErr)
			logger.G().Warnf("Skip expired history check because metadata key is invalid: fileKey=%s metadataKey=%q", key, metadata.Key)
			continue
		}

		createdAt := time.UnixMilli(metadata.CreatedAt).In(now.Location())
		expired, expirationErr := isHistoryExpired(createdAt, now, config)
		if expirationErr != nil {
			stats.Failed++
			cleanupErrors = append(cleanupErrors, fmt.Errorf("calculate history expiration %s: %w", key, expirationErr))
			logger.G().Warnf("Skip expired history check because expiration could not be calculated: key=%s error=%v", key, expirationErr)
			continue
		}
		if !expired {
			continue
		}
		paths, pathErr := historyFilePaths(historyStorageDir, key)
		if pathErr != nil {
			stats.Failed++
			cleanupErrors = append(cleanupErrors, fmt.Errorf("resolve expired history %s: %w", key, pathErr))
			continue
		}
		if _, deleteErr := deleteHistoryFiles(paths...); deleteErr != nil {
			stats.Failed++
			cleanupErrors = append(cleanupErrors, fmt.Errorf("delete expired history %s: %w", key, deleteErr))
			continue
		}
		stats.Deleted++
	}

	return stats, errors.Join(cleanupErrors...)
}

func isHistoryExpired(createdAt, now time.Time, config settingservice.HistoryRetentionConfig) (bool, error) {
	expiresAt, err := historyExpirationTime(createdAt, config)
	if err != nil {
		return false, err
	}
	return !expiresAt.After(now), nil
}

func historyExpirationTime(createdAt time.Time, config settingservice.HistoryRetentionConfig) (time.Time, error) {
	if config.Value < 1 || config.Value > 9999 {
		return time.Time{}, fmt.Errorf("history retention value out of range: %d", config.Value)
	}
	switch config.Unit {
	case settingservice.HistoryRetentionUnitHour:
		return createdAt.Add(time.Duration(config.Value) * time.Hour), nil
	case settingservice.HistoryRetentionUnitDay:
		return createdAt.AddDate(0, 0, config.Value), nil
	case settingservice.HistoryRetentionUnitWeek:
		return createdAt.AddDate(0, 0, config.Value*7), nil
	case settingservice.HistoryRetentionUnitMonth:
		return addCalendarMonthsClamped(createdAt, config.Value), nil
	case settingservice.HistoryRetentionUnitYear:
		return addCalendarMonthsClamped(createdAt, config.Value*12), nil
	default:
		return time.Time{}, fmt.Errorf("invalid history retention unit: %q", config.Unit)
	}
}

func addCalendarMonthsClamped(value time.Time, months int) time.Time {
	year, month, day := value.Date()
	hour, minute, second := value.Clock()
	targetMonthStart := time.Date(year, month+time.Month(months), 1, hour, minute, second, value.Nanosecond(), value.Location())
	lastTargetDay := time.Date(
		targetMonthStart.Year(),
		targetMonthStart.Month()+1,
		0,
		hour,
		minute,
		second,
		value.Nanosecond(),
		value.Location(),
	).Day()
	if day > lastTargetDay {
		day = lastTargetDay
	}
	return time.Date(
		targetMonthStart.Year(),
		targetMonthStart.Month(),
		day,
		hour,
		minute,
		second,
		value.Nanosecond(),
		value.Location(),
	)
}
