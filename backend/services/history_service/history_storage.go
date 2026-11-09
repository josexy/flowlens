package historyservice

import (
	"os"
	"path/filepath"

	"github.com/josexy/flowlens/backend/pkg/fs"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
)

func getHistoryStoragePath() (string, error) {
	userConfigDir, err := fs.GetBaseStorageDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(userConfigDir, "histories"), nil
}

func loadHistoryMetadata(dir, key string) (*proxyservice.HistoryMetadata, error) {
	fp, err := os.Open(filepath.Join(dir, fs.GetHBinFileName(key)))
	if err != nil {
		return nil, err
	}
	defer fp.Close()
	return proxyservice.DecodeHistoryMetadata(fp)
}
