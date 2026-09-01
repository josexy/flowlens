package apicollectionservice

import (
	"path/filepath"

	"github.com/josexy/flowlens/backend/pkg/fs"
)

const (
	apiCollectionDirName      = "api_collections"
	apiCollectionFilesDirName = "files"
)

func getAPICollectionStorageDir() (string, error) {
	baseDir, err := fs.GetBaseStorageDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(baseDir, apiCollectionDirName), nil
}

func getAPICollectionFilesStoragePath() (string, error) {
	collectionDir, err := getAPICollectionStorageDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(collectionDir, apiCollectionFilesDirName), nil
}

func newEmptyCollectionStore() *apiCollectionStore {
	return &apiCollectionStore{
		SchemaVersion: apiCollectionSchemaVersion,
		Folders:       []*APICollectionFolder{},
	}
}
