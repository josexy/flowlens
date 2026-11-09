package historyservice

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/josexy/flowlens/backend/pkg/fs"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
)

func TestHistoryExpirationTimeUsesNaturalCalendar(t *testing.T) {
	location := time.FixedZone("test", 8*60*60)
	tests := []struct {
		name      string
		createdAt time.Time
		value     int
		unit      settingservice.HistoryRetentionUnit
		want      time.Time
	}{
		{
			name:      "hours use elapsed duration",
			createdAt: time.Date(2026, time.January, 1, 10, 30, 0, 0, location),
			value:     2,
			unit:      settingservice.HistoryRetentionUnitHour,
			want:      time.Date(2026, time.January, 1, 12, 30, 0, 0, location),
		},
		{
			name:      "days keep local clock time",
			createdAt: time.Date(2026, time.January, 1, 10, 30, 0, 0, location),
			value:     2,
			unit:      settingservice.HistoryRetentionUnitDay,
			want:      time.Date(2026, time.January, 3, 10, 30, 0, 0, location),
		},
		{
			name:      "weeks use seven calendar days",
			createdAt: time.Date(2026, time.January, 1, 10, 30, 0, 0, location),
			value:     2,
			unit:      settingservice.HistoryRetentionUnitWeek,
			want:      time.Date(2026, time.January, 15, 10, 30, 0, 0, location),
		},
		{
			name:      "month clamps to last day",
			createdAt: time.Date(2025, time.January, 31, 10, 30, 0, 0, location),
			value:     1,
			unit:      settingservice.HistoryRetentionUnitMonth,
			want:      time.Date(2025, time.February, 28, 10, 30, 0, 0, location),
		},
		{
			name:      "month clamps to leap day",
			createdAt: time.Date(2024, time.January, 31, 10, 30, 0, 0, location),
			value:     1,
			unit:      settingservice.HistoryRetentionUnitMonth,
			want:      time.Date(2024, time.February, 29, 10, 30, 0, 0, location),
		},
		{
			name:      "year clamps leap day",
			createdAt: time.Date(2024, time.February, 29, 10, 30, 0, 0, location),
			value:     1,
			unit:      settingservice.HistoryRetentionUnitYear,
			want:      time.Date(2025, time.February, 28, 10, 30, 0, 0, location),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := historyExpirationTime(tt.createdAt, settingservice.HistoryRetentionConfig{
				Enabled: true,
				Value:   tt.value,
				Unit:    tt.unit,
			})
			if err != nil {
				t.Fatalf("historyExpirationTime: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Fatalf("historyExpirationTime = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestIsHistoryExpiredIncludesExactBoundary(t *testing.T) {
	createdAt := time.Date(2026, time.January, 1, 10, 0, 0, 0, time.UTC)
	config := settingservice.HistoryRetentionConfig{
		Enabled: true,
		Value:   1,
		Unit:    settingservice.HistoryRetentionUnitDay,
	}

	expired, err := isHistoryExpired(createdAt, createdAt.AddDate(0, 0, 1), config)
	if err != nil {
		t.Fatalf("isHistoryExpired exact boundary: %v", err)
	}
	if !expired {
		t.Fatal("history should be expired at the exact expiration boundary")
	}

	expired, err = isHistoryExpired(createdAt, createdAt.AddDate(0, 0, 1).Add(-time.Millisecond), config)
	if err != nil {
		t.Fatalf("isHistoryExpired before boundary: %v", err)
	}
	if expired {
		t.Fatal("history should not be expired before the expiration boundary")
	}
}

func TestDeleteExpiredHistoriesDeletesPairsAndPreservesUnreadableFiles(t *testing.T) {
	historyDir := setupHistoryTestStorage(t)
	now := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.Local)
	writeTestHistoryPair(t, historyDir, "expired", now.AddDate(0, 0, -2).UnixMilli())
	writeTestHistoryPair(t, historyDir, "fresh", now.Add(-12*time.Hour).UnixMilli())
	writeTestHistoryPair(t, historyDir, "invalid-time", 0)
	writeTestHistoryPairWithMetadataKey(t, historyDir, "empty-key", "", now.AddDate(0, 0, -2).UnixMilli())
	writeTestHistoryPairWithMetadataKey(t, historyDir, "mismatched-key", "other-key", now.AddDate(0, 0, -2).UnixMilli())
	writeTestCorruptHistoryPair(t, historyDir, "corrupt")

	service := &HistoryService{indexMap: make(historyIndexMap)}
	stats, err := service.deleteExpiredHistories(now, settingservice.HistoryRetentionConfig{
		Enabled: true,
		Value:   1,
		Unit:    settingservice.HistoryRetentionUnitDay,
	})
	if err == nil {
		t.Fatal("expected corrupt metadata to be reported")
	}
	if stats != (historyCleanupStats{Scanned: 6, Deleted: 1, Failed: 4}) {
		t.Fatalf("cleanup stats = %+v", stats)
	}
	assertHistoryPairExists(t, historyDir, "expired", false)
	assertHistoryPairExists(t, historyDir, "fresh", true)
	assertHistoryPairExists(t, historyDir, "invalid-time", true)
	assertHistoryPairExists(t, historyDir, "empty-key", true)
	assertHistoryPairExists(t, historyDir, "mismatched-key", true)
	assertHistoryPairExists(t, historyDir, "corrupt", true)
}

func TestDeleteExpiredHistoriesDisabledDoesNotScan(t *testing.T) {
	historyDir := setupHistoryTestStorage(t)
	writeTestHistoryPair(t, historyDir, "old", time.Now().AddDate(-10, 0, 0).UnixMilli())

	service := &HistoryService{indexMap: make(historyIndexMap)}
	stats, err := service.deleteExpiredHistories(time.Now(), settingservice.HistoryRetentionConfig{
		Enabled: false,
		Value:   1,
		Unit:    settingservice.HistoryRetentionUnitHour,
	})
	if err != nil {
		t.Fatalf("deleteExpiredHistories: %v", err)
	}
	if stats != (historyCleanupStats{}) {
		t.Fatalf("cleanup stats = %+v, want zero", stats)
	}
	assertHistoryPairExists(t, historyDir, "old", true)
}

func TestRunStartupMaintenanceCleansBeforeInitializingIndexes(t *testing.T) {
	historyDir := setupHistoryTestStorage(t)
	writeTestHistoryPair(t, historyDir, "expired", time.Now().Add(-48*time.Hour).UnixMilli())
	writeTestHistoryPair(t, historyDir, "fresh", time.Now().Add(-time.Hour).UnixMilli())

	settingSvc := settingservice.New(nil)
	if err := settingSvc.Update(&settingservice.Settings{
		HistoryRetentionConfig: &settingservice.HistoryRetentionConfig{
			Enabled: true,
			Value:   1,
			Unit:    settingservice.HistoryRetentionUnitDay,
		},
	}); err != nil {
		t.Fatalf("Update settings: %v", err)
	}

	service := New(settingSvc, nil)
	service.runStartupMaintenance()

	assertHistoryPairExists(t, historyDir, "expired", false)
	assertHistoryPairExists(t, historyDir, "fresh", true)

	service.mu.RLock()
	_, expiredIndexed := service.indexMap["expired"]
	_, freshIndexed := service.indexMap["fresh"]
	service.mu.RUnlock()
	if expiredIndexed {
		t.Fatal("expired history should be deleted before index initialization")
	}
	if !freshIndexed {
		t.Fatal("fresh history should be initialized after cleanup")
	}
}

func setupHistoryTestStorage(t *testing.T) string {
	t.Helper()
	configRoot := t.TempDir()
	t.Setenv("APPDATA", configRoot)
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	t.Setenv("HOME", configRoot)
	historyDir, err := getHistoryStoragePath()
	if err != nil {
		t.Fatalf("getHistoryStoragePath: %v", err)
	}
	if err := os.MkdirAll(historyDir, 0o755); err != nil {
		t.Fatalf("MkdirAll history dir: %v", err)
	}
	return historyDir
}

func writeTestHistoryPair(t *testing.T, dir, key string, createdAt int64) {
	t.Helper()
	writeTestHistoryPairWithMetadataKey(t, dir, key, key, createdAt)
}

func writeTestHistoryPairWithMetadataKey(t *testing.T, dir, fileKey, metadataKey string, createdAt int64) {
	t.Helper()
	file, err := os.Create(filepath.Join(dir, fs.GetHBinFileName(fileKey)))
	if err != nil {
		t.Fatalf("Create hbin: %v", err)
	}
	defer file.Close()
	if _, err := file.WriteString("PGHI"); err != nil {
		t.Fatalf("Write magic: %v", err)
	}
	for _, value := range []any{uint16(1), uint32(len(metadataKey))} {
		if err := binary.Write(file, binary.BigEndian, value); err != nil {
			t.Fatalf("Write metadata prefix: %v", err)
		}
	}
	if _, err := file.WriteString(metadataKey); err != nil {
		t.Fatalf("Write key: %v", err)
	}
	if err := binary.Write(file, binary.BigEndian, uint32(0)); err != nil {
		t.Fatalf("Write alias length: %v", err)
	}
	if err := binary.Write(file, binary.BigEndian, createdAt); err != nil {
		t.Fatalf("Write createdAt: %v", err)
	}
	if err := binary.Write(file, binary.BigEndian, uint32(0)); err != nil {
		t.Fatalf("Write entry count: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, fs.GetHIdxFileName(fileKey)), []byte{0, 0, 0, 0}, 0o644); err != nil {
		t.Fatalf("Write hidx: %v", err)
	}
}

func writeTestCorruptHistoryPair(t *testing.T, dir, key string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, fs.GetHBinFileName(key)), []byte("broken"), 0o644); err != nil {
		t.Fatalf("Write corrupt hbin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, fs.GetHIdxFileName(key)), []byte{0, 0, 0, 0}, 0o644); err != nil {
		t.Fatalf("Write corrupt hidx: %v", err)
	}
}

func assertHistoryPairExists(t *testing.T, dir, key string, want bool) {
	t.Helper()
	for _, path := range []string{
		filepath.Join(dir, fs.GetHBinFileName(key)),
		filepath.Join(dir, fs.GetHIdxFileName(key)),
	} {
		_, err := os.Stat(path)
		if want && err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
		if !want && !os.IsNotExist(err) {
			t.Fatalf("expected %s to be deleted, got %v", path, err)
		}
	}
}
