package apicollectionservice

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appdatabase "github.com/josexy/flowlens/backend/pkg/database"
	"github.com/josexy/flowlens/backend/pkg/fs"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func configureTestStorage(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	baseDir, err := fs.GetBaseStorageDir()
	if err != nil {
		t.Fatalf("GetBaseStorageDir: %v", err)
	}
	return baseDir
}

func newTestService(t *testing.T) *APICollectionService {
	t.Helper()
	db, err := appdatabase.Open()
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return New(db)
}

func assertAPICollectionFilePath(t *testing.T, path string) {
	t.Helper()
	filesDir, err := getAPICollectionFilesStoragePath()
	if err != nil {
		t.Fatalf("getAPICollectionFilesStoragePath: %v", err)
	}
	rel, err := filepath.Rel(filesDir, path)
	if err != nil {
		t.Fatalf("filepath.Rel: %v", err)
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		t.Fatalf("path %q is not under API collection files dir %q", path, filesDir)
	}
}

func assertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", path, err)
	}
	if string(got) != string(want) {
		t.Fatalf("ReadFile(%q) = %q, want %q", path, got, want)
	}
}

func apiNodeParentID(t *testing.T, svc *APICollectionService, nodeID string) string {
	t.Helper()
	var parentID string
	if err := svc.repository.db.QueryRow(`
		SELECT COALESCE(parent_id, '') FROM api_nodes WHERE id = ?
	`, nodeID).Scan(&parentID); err != nil {
		t.Fatalf("query parent for node %q: %v", nodeID, err)
	}
	return parentID
}

func TestLoadEmptyDatabaseReturnsEmptyV2Tree(t *testing.T) {
	configureTestStorage(t)
	svc := newTestService(t)
	if err := svc.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	tree, err := svc.ListCollection()
	if err != nil {
		t.Fatalf("ListCollection: %v", err)
	}
	if tree.SchemaVersion != apiCollectionSchemaVersion || len(tree.Folders) != 0 {
		t.Fatalf("unexpected empty tree: %+v", tree)
	}
}

func TestLoadIgnoresLegacyCollectionJSON(t *testing.T) {
	baseDir := configureTestStorage(t)
	legacyPath := filepath.Join(baseDir, apiCollectionDirName, "collection.json")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	legacyContent := []byte(`{"schemaVersion":`)
	if err := os.WriteFile(legacyPath, legacyContent, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	svc := newTestService(t)
	if _, err := svc.repository.createFolder(context.Background(), "", "Database"); err != nil {
		t.Fatalf("create database folder: %v", err)
	}
	if err := svc.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	tree, err := svc.ListCollection()
	if err != nil {
		t.Fatalf("ListCollection: %v", err)
	}
	if len(tree.Folders) != 1 || tree.Folders[0].Name != "Database" {
		t.Fatalf("legacy JSON affected database tree: %+v", tree.Folders)
	}
	got, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(legacyContent) {
		t.Fatal("legacy collection JSON was modified")
	}
}

func TestServiceStartupLoadsCollectionSynchronously(t *testing.T) {
	configureTestStorage(t)
	svc := newTestService(t)
	if _, err := svc.repository.createFolder(context.Background(), "", "Startup"); err != nil {
		t.Fatalf("create database folder: %v", err)
	}
	if err := svc.ServiceStartup(context.Background(), application.ServiceOptions{}); err != nil {
		t.Fatalf("ServiceStartup: %v", err)
	}
	if !svc.isLoaded() {
		t.Fatal("expected startup load to mark collection loaded")
	}
	tree, err := svc.ListCollection()
	if err != nil {
		t.Fatalf("ListCollection: %v", err)
	}
	if len(tree.Folders) != 1 || tree.Folders[0].Name != "Startup" {
		t.Fatalf("unexpected startup tree: %+v", tree.Folders)
	}
}

func TestListCollectionLazyLoadsWhenStartupHasNotFinished(t *testing.T) {
	configureTestStorage(t)
	svc := newTestService(t)
	if _, err := svc.repository.createFolder(context.Background(), "", "Lazy"); err != nil {
		t.Fatalf("create database folder: %v", err)
	}
	if svc.isLoaded() {
		t.Fatal("expected new service to start unloaded")
	}
	tree, err := svc.ListCollection()
	if err != nil {
		t.Fatalf("ListCollection: %v", err)
	}
	if !svc.isLoaded() {
		t.Fatal("expected ListCollection to lazy-load collection")
	}
	if len(tree.Folders) != 1 || tree.Folders[0].Name != "Lazy" {
		t.Fatalf("unexpected lazy-loaded tree: %+v", tree.Folders)
	}
}

func newLoadedTestService(t *testing.T) *APICollectionService {
	t.Helper()
	configureTestStorage(t)
	svc := newTestService(t)
	if err := svc.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return svc
}

func TestCreateFolderSupportsRootAndChildFolders(t *testing.T) {
	svc := newLoadedTestService(t)

	parent, err := svc.CreateFolder("", "Parent")
	if err != nil {
		t.Fatalf("CreateFolder parent: %v", err)
	}
	child, err := svc.CreateFolder(parent.ID, "Child")
	if err != nil {
		t.Fatalf("CreateFolder child: %v", err)
	}

	tree, err := svc.ListCollection()
	if err != nil {
		t.Fatalf("ListCollection: %v", err)
	}
	if len(tree.Folders) != 1 || tree.Folders[0].ID != parent.ID {
		t.Fatalf("unexpected root folders: %+v", tree.Folders)
	}
	if len(tree.Folders[0].Folders) != 1 || tree.Folders[0].Folders[0].ID != child.ID {
		t.Fatalf("unexpected child folders: %+v", tree.Folders[0].Folders)
	}
}

func TestAllowsSameNameUnderDifferentParents(t *testing.T) {
	svc := newLoadedTestService(t)

	first, err := svc.CreateFolder("", "Parent A")
	if err != nil {
		t.Fatalf("CreateFolder first parent: %v", err)
	}
	second, err := svc.CreateFolder("", "Parent B")
	if err != nil {
		t.Fatalf("CreateFolder second parent: %v", err)
	}
	if _, err := svc.CreateFolder(first.ID, "Shared"); err != nil {
		t.Fatalf("CreateFolder shared under first parent: %v", err)
	}
	if _, err := svc.CreateFolder(second.ID, "Shared"); err != nil {
		t.Fatalf("CreateFolder shared under second parent: %v", err)
	}
}

func TestSaveHTTPRequestRejectsRootParent(t *testing.T) {
	svc := newLoadedTestService(t)

	_, err := svc.SaveHTTPRequest("", "Get Users", &SavedHTTPRequest{
		Method:    "GET",
		URL:       "https://api.example.com/users",
		BodyType:  "none",
		ProxyMode: "none",
	})
	if err == nil {
		t.Fatal("expected saving request at root to fail")
	}
	if !strings.Contains(err.Error(), "parent folder id cannot be empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSaveHTTPRequestCopiesRequestDraftCacheFilesIntoCollectionStorage(t *testing.T) {
	baseDir := configureTestStorage(t)
	requestDraftCacheDir := filepath.Join(baseDir, "request-draft-cache")
	if err := os.MkdirAll(requestDraftCacheDir, 0o755); err != nil {
		t.Fatalf("MkdirAll request draft cache: %v", err)
	}
	sourcePath := filepath.Join(requestDraftCacheDir, "upload.bin")
	sourceBytes := []byte("cached upload")
	if err := os.WriteFile(sourcePath, sourceBytes, 0o644); err != nil {
		t.Fatalf("WriteFile request draft cache: %v", err)
	}

	svc := newTestService(t)
	if err := svc.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	folder, err := svc.CreateFolder("", "Uploads")
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	request, err := svc.SaveHTTPRequest(folder.ID, "Upload", &SavedHTTPRequest{
		Method:   "POST",
		URL:      "https://example.com/upload",
		BodyType: "file",
		BodyFile: &SavedFile{
			Path: sourcePath,
			Name: "upload.bin",
			Size: int64(len(sourceBytes)),
		},
		ProxyMode: "none",
	})
	if err != nil {
		t.Fatalf("SaveHTTPRequest: %v", err)
	}

	if request.HTTP.BodyFile == nil {
		t.Fatal("expected saved body file")
	}
	if request.HTTP.BodyFile.Path == sourcePath {
		t.Fatal("expected API collection to save a durable file copy, got original request draft cache path")
	}
	assertAPICollectionFilePath(t, request.HTTP.BodyFile.Path)
	assertFileBytes(t, request.HTTP.BodyFile.Path, sourceBytes)
	var payload string
	if err := svc.repository.db.QueryRow(`
		SELECT payload_json FROM api_request_payloads WHERE node_id = ?
	`, request.ID).Scan(&payload); err != nil {
		t.Fatalf("query stored request payload: %v", err)
	}
	var stored SavedHTTPRequest
	if err := json.Unmarshal([]byte(payload), &stored); err != nil {
		t.Fatalf("decode stored request payload: %v", err)
	}
	if stored.BodyFile == nil || filepath.IsAbs(stored.BodyFile.Path) || !strings.HasPrefix(filepath.ToSlash(stored.BodyFile.Path), "files/") {
		t.Fatalf("expected relative managed file path in database, got %+v", stored.BodyFile)
	}

	if err := os.RemoveAll(requestDraftCacheDir); err != nil {
		t.Fatalf("RemoveAll request draft cache: %v", err)
	}
	assertFileBytes(t, request.HTTP.BodyFile.Path, sourceBytes)
}

func TestSaveHTTPRequestCleansCopiedFileWhenDatabaseInsertFails(t *testing.T) {
	baseDir := configureTestStorage(t)
	requestDraftCacheDir := filepath.Join(baseDir, "request-draft-cache")
	if err := os.MkdirAll(requestDraftCacheDir, 0o755); err != nil {
		t.Fatalf("MkdirAll request draft cache: %v", err)
	}
	sourcePath := filepath.Join(requestDraftCacheDir, "failure.bin")
	if err := os.WriteFile(sourcePath, []byte("failure"), 0o644); err != nil {
		t.Fatalf("WriteFile source: %v", err)
	}

	svc := newTestService(t)
	if err := svc.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	folder, err := svc.CreateFolder("", "Failure")
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if _, err := svc.repository.db.Exec(`
		CREATE TRIGGER fail_http_insert
		BEFORE INSERT ON api_nodes
		WHEN NEW.kind = 'http'
		BEGIN
			SELECT RAISE(ABORT, 'forced http insert failure');
		END
	`); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	if _, err := svc.SaveHTTPRequest(folder.ID, "Failure", &SavedHTTPRequest{
		Method: "POST", URL: "https://example.com", BodyType: "file",
		BodyFile:  &SavedFile{Path: sourcePath, Name: "failure.bin", Size: 7},
		ProxyMode: "none",
	}); err == nil {
		t.Fatal("expected forced request insert failure")
	}
	filesDir, err := getAPICollectionFilesStoragePath()
	if err != nil {
		t.Fatalf("get files dir: %v", err)
	}
	entries, err := os.ReadDir(filesDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("ReadDir files: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected copied files to be cleaned after rollback, got %v", entries)
	}
}

func TestDeleteNodeKeepsManagedFileWhenDatabaseDeleteFails(t *testing.T) {
	baseDir := configureTestStorage(t)
	requestDraftCacheDir := filepath.Join(baseDir, "request-draft-cache")
	if err := os.MkdirAll(requestDraftCacheDir, 0o755); err != nil {
		t.Fatalf("MkdirAll request draft cache: %v", err)
	}
	sourcePath := filepath.Join(requestDraftCacheDir, "keep.bin")
	if err := os.WriteFile(sourcePath, []byte("keep"), 0o644); err != nil {
		t.Fatalf("WriteFile source: %v", err)
	}

	svc := newTestService(t)
	if err := svc.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	folder, err := svc.CreateFolder("", "Keep")
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	request, err := svc.SaveHTTPRequest(folder.ID, "Keep", &SavedHTTPRequest{
		Method: "POST", URL: "https://example.com", BodyType: "file",
		BodyFile:  &SavedFile{Path: sourcePath, Name: "keep.bin", Size: 4},
		ProxyMode: "none",
	})
	if err != nil {
		t.Fatalf("SaveHTTPRequest: %v", err)
	}
	managedPath := request.HTTP.BodyFile.Path
	if _, err := svc.repository.db.Exec(fmt.Sprintf(`
		CREATE TRIGGER fail_request_delete
		BEFORE DELETE ON api_nodes
		WHEN OLD.id = '%s'
		BEGIN
			SELECT RAISE(ABORT, 'forced request delete failure');
		END
	`, request.ID)); err != nil {
		t.Fatalf("create delete failure trigger: %v", err)
	}
	if err := svc.DeleteNode(request.ID); err == nil {
		t.Fatal("expected forced request delete failure")
	}
	assertFileBytes(t, managedPath, []byte("keep"))
	if _, err := svc.GetRequest(request.ID); err != nil {
		t.Fatalf("request should remain after rollback: %v", err)
	}
}

func TestLoadRemovesOrphanManagedFilesAndKeepsReferencedFiles(t *testing.T) {
	baseDir := configureTestStorage(t)
	requestDraftCacheDir := filepath.Join(baseDir, "request-draft-cache")
	if err := os.MkdirAll(requestDraftCacheDir, 0o755); err != nil {
		t.Fatalf("MkdirAll request draft cache: %v", err)
	}
	sourcePath := filepath.Join(requestDraftCacheDir, "referenced.bin")
	if err := os.WriteFile(sourcePath, []byte("referenced"), 0o644); err != nil {
		t.Fatalf("WriteFile source: %v", err)
	}
	svc := newTestService(t)
	if err := svc.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	folder, err := svc.CreateFolder("", "Files")
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	request, err := svc.SaveHTTPRequest(folder.ID, "Referenced", &SavedHTTPRequest{
		Method: "POST", URL: "https://example.com", BodyType: "file",
		BodyFile:  &SavedFile{Path: sourcePath, Name: "referenced.bin", Size: 10},
		ProxyMode: "none",
	})
	if err != nil {
		t.Fatalf("SaveHTTPRequest: %v", err)
	}
	filesDir, err := getAPICollectionFilesStoragePath()
	if err != nil {
		t.Fatalf("get files dir: %v", err)
	}
	orphanPath := filepath.Join(filesDir, "orphan.bin")
	if err := os.WriteFile(orphanPath, []byte("orphan"), 0o644); err != nil {
		t.Fatalf("WriteFile orphan: %v", err)
	}

	reloaded := newTestService(t)
	if err := reloaded.Load(); err != nil {
		t.Fatalf("Load reloaded: %v", err)
	}
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Fatalf("orphan file should be removed, got %v", err)
	}
	assertFileBytes(t, request.HTTP.BodyFile.Path, []byte("referenced"))
}

func TestLoadRejectsMissingReferencedManagedFile(t *testing.T) {
	baseDir := configureTestStorage(t)
	requestDraftCacheDir := filepath.Join(baseDir, "request-draft-cache")
	if err := os.MkdirAll(requestDraftCacheDir, 0o755); err != nil {
		t.Fatalf("MkdirAll request draft cache: %v", err)
	}
	sourcePath := filepath.Join(requestDraftCacheDir, "missing-later.bin")
	if err := os.WriteFile(sourcePath, []byte("present"), 0o644); err != nil {
		t.Fatalf("WriteFile source: %v", err)
	}
	svc := newTestService(t)
	if err := svc.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	folder, err := svc.CreateFolder("", "Files")
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	request, err := svc.SaveHTTPRequest(folder.ID, "Missing Later", &SavedHTTPRequest{
		Method: "POST", URL: "https://example.com", BodyType: "file",
		BodyFile:  &SavedFile{Path: sourcePath, Name: "missing-later.bin", Size: 7},
		ProxyMode: "none",
	})
	if err != nil {
		t.Fatalf("SaveHTTPRequest: %v", err)
	}
	if err := os.Remove(request.HTTP.BodyFile.Path); err != nil {
		t.Fatalf("remove managed file: %v", err)
	}

	reloaded := newTestService(t)
	if err := reloaded.Load(); err == nil {
		t.Fatal("expected missing referenced managed file to fail startup load")
	}
}

func TestUpdateAndDeleteHTTPRequestRemoveUnreferencedCollectionFiles(t *testing.T) {
	baseDir := configureTestStorage(t)
	requestDraftCacheDir := filepath.Join(baseDir, "request-draft-cache")
	if err := os.MkdirAll(requestDraftCacheDir, 0o755); err != nil {
		t.Fatalf("MkdirAll request draft cache: %v", err)
	}
	firstSourcePath := filepath.Join(requestDraftCacheDir, "first.bin")
	secondSourcePath := filepath.Join(requestDraftCacheDir, "second.bin")
	if err := os.WriteFile(firstSourcePath, []byte("first"), 0o644); err != nil {
		t.Fatalf("WriteFile first source: %v", err)
	}
	if err := os.WriteFile(secondSourcePath, []byte("second"), 0o644); err != nil {
		t.Fatalf("WriteFile second source: %v", err)
	}

	svc := newTestService(t)
	if err := svc.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	folder, err := svc.CreateFolder("", "Uploads")
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	request, err := svc.SaveHTTPRequest(folder.ID, "Upload", &SavedHTTPRequest{
		Method:    "POST",
		URL:       "https://example.com/upload",
		BodyType:  "file",
		BodyFile:  &SavedFile{Path: firstSourcePath, Name: "first.bin", Size: 5},
		ProxyMode: "none",
	})
	if err != nil {
		t.Fatalf("SaveHTTPRequest: %v", err)
	}
	firstCollectionPath := request.HTTP.BodyFile.Path

	updated, err := svc.UpdateHTTPRequest(request.ID, &SavedHTTPRequest{
		Method:    "POST",
		URL:       "https://example.com/upload",
		BodyType:  "file",
		BodyFile:  &SavedFile{Path: secondSourcePath, Name: "second.bin", Size: 6},
		ProxyMode: "none",
	})
	if err != nil {
		t.Fatalf("UpdateHTTPRequest: %v", err)
	}
	if _, err := os.Stat(firstCollectionPath); !os.IsNotExist(err) {
		t.Fatalf("first collection file should be removed after update, got %v", err)
	}
	secondCollectionPath := updated.HTTP.BodyFile.Path
	assertAPICollectionFilePath(t, secondCollectionPath)
	assertFileBytes(t, secondCollectionPath, []byte("second"))

	if err := svc.DeleteNode(request.ID); err != nil {
		t.Fatalf("DeleteNode: %v", err)
	}
	if _, err := os.Stat(secondCollectionPath); !os.IsNotExist(err) {
		t.Fatalf("second collection file should be removed after delete, got %v", err)
	}
}

func TestSaveAndUpdateHTTPAPIInFolder(t *testing.T) {
	svc := newLoadedTestService(t)
	folder, err := svc.CreateFolder("", "Folder")
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	request, err := svc.SaveHTTPRequest(folder.ID, "Get Users", &SavedHTTPRequest{
		Method:    "GET",
		URL:       "https://api.example.com/users",
		BodyType:  "none",
		ProxyMode: "none",
	})
	if err != nil {
		t.Fatalf("SaveHTTPRequest: %v", err)
	}
	if request.Type != APICollectionNodeTypeHTTP || request.HTTP == nil {
		t.Fatalf("expected HTTP request, got %+v", request)
	}

	originalUpdatedAt := request.UpdatedAt
	updated, err := svc.UpdateHTTPRequest(request.ID, &SavedHTTPRequest{
		Method:    "POST",
		URL:       "https://api.example.com/users",
		BodyType:  "json",
		BodyText:  `{"name":"Ada"}`,
		ProxyMode: "mitm",
	})
	if err != nil {
		t.Fatalf("UpdateHTTPRequest: %v", err)
	}
	if updated.HTTP.Method != "POST" || updated.HTTP.BodyType != "json" {
		t.Fatalf("unexpected updated HTTP payload: %+v", updated.HTTP)
	}
	if updated.UpdatedAt < originalUpdatedAt {
		t.Fatalf("expected updated timestamp to move forward")
	}
}

func TestServiceRequestUnderFolderPersistsThroughTreeBridge(t *testing.T) {
	configureTestStorage(t)
	svc := newTestService(t)
	if err := svc.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	folder, err := svc.CreateFolder("", "Folder")
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if _, err := svc.SaveHTTPRequest(folder.ID, "Get Users", &SavedHTTPRequest{
		Method:    "GET",
		URL:       "https://api.example.com/users",
		BodyType:  "none",
		ProxyMode: "none",
	}); err != nil {
		t.Fatalf("SaveHTTPRequest: %v", err)
	}

	reloaded := newTestService(t)
	if err := reloaded.Load(); err != nil {
		t.Fatalf("Load reloaded: %v", err)
	}
	tree, err := reloaded.ListCollection()
	if err != nil {
		t.Fatalf("ListCollection: %v", err)
	}
	if len(tree.Folders) != 1 || len(tree.Folders[0].Requests) != 1 {
		t.Fatalf("expected folder and request after reload, got %+v", tree.Folders)
	}
	requestSummary := tree.Folders[0].Requests[0]
	if tree.Folders[0].ID != folder.ID || requestSummary.HTTP == nil || requestSummary.HTTP.Method != "GET" {
		t.Fatalf("unexpected reloaded request summary: %+v", requestSummary)
	}
	request, err := reloaded.GetRequest(requestSummary.ID)
	if err != nil {
		t.Fatalf("GetRequest: %v", err)
	}
	if request.HTTP == nil || request.HTTP.URL != "https://api.example.com/users" {
		t.Fatalf("unexpected reloaded request details: %+v", request)
	}
}

func TestSaveAndUpdateWebSocketAPIInFolder(t *testing.T) {
	svc := newLoadedTestService(t)

	folder, err := svc.CreateFolder("", "Folder")
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	request, err := svc.SaveWebSocketRequest(folder.ID, "WS Feed", &SavedWebSocketRequest{
		URL:              "wss://api.example.com/feed",
		DraftType:        "json",
		DraftText:        `{"op":"subscribe"}`,
		ProxyMode:        "none",
		TLSClientHelloID: proxyservice.TLSClientHelloChromeAuto,
	})
	if err != nil {
		t.Fatalf("SaveWebSocketRequest: %v", err)
	}
	if request.Type != APICollectionNodeTypeWebSocket || request.WebSocket == nil {
		t.Fatalf("expected websocket request, got %+v", request)
	}

	updated, err := svc.UpdateWebSocketRequest(request.ID, &SavedWebSocketRequest{
		URL:              "wss://api.example.com/feed",
		DraftType:        "text",
		DraftText:        "ping",
		ProxyMode:        "none",
		TLSClientHelloID: proxyservice.TLSClientHelloFirefoxAuto,
	})
	if err != nil {
		t.Fatalf("UpdateWebSocketRequest: %v", err)
	}
	if updated.WebSocket.DraftType != "text" ||
		updated.WebSocket.DraftText != "ping" ||
		updated.WebSocket.TLSClientHelloID != proxyservice.TLSClientHelloFirefoxAuto {
		t.Fatalf("unexpected websocket payload: %+v", updated.WebSocket)
	}
}

func TestListCollectionReturnsSummariesAndGetRequestReturnsDetails(t *testing.T) {
	svc := newLoadedTestService(t)
	folder, err := svc.CreateFolder("", "Folder")
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	httpRequest, err := svc.SaveHTTPRequest(folder.ID, "Create User", &SavedHTTPRequest{
		Method:    "POST",
		URL:       "https://api.example.com/users",
		Headers:   []*SavedKeyValue{{Key: "Content-Type", Value: "application/json", Enabled: true}},
		BodyType:  "json",
		BodyText:  `{"name":"Ada"}`,
		ProxyMode: "none",
	})
	if err != nil {
		t.Fatalf("SaveHTTPRequest: %v", err)
	}
	webSocketRequest, err := svc.SaveWebSocketRequest(folder.ID, "Events", &SavedWebSocketRequest{
		URL:       "wss://api.example.com/events",
		DraftType: "text",
		DraftText: "ping",
		ProxyMode: "none",
	})
	if err != nil {
		t.Fatalf("SaveWebSocketRequest: %v", err)
	}

	tree, err := svc.ListCollection()
	if err != nil {
		t.Fatalf("ListCollection: %v", err)
	}
	httpSummary := findRequestByID(tree.Folders, httpRequest.ID)
	if httpSummary == nil || httpSummary.HTTP == nil || httpSummary.HTTP.Method != "POST" {
		t.Fatalf("expected HTTP summary with method, got %+v", httpSummary)
	}
	if httpSummary.HTTP.URL != "" || httpSummary.HTTP.BodyText != "" || len(httpSummary.HTTP.Headers) != 0 {
		t.Fatalf("expected HTTP summary without request details, got %+v", httpSummary.HTTP)
	}
	webSocketSummary := findRequestByID(tree.Folders, webSocketRequest.ID)
	if webSocketSummary == nil || webSocketSummary.WebSocket != nil {
		t.Fatalf("expected WebSocket summary without request details, got %+v", webSocketSummary)
	}

	httpDetails, err := svc.GetRequest("  " + httpRequest.ID + "  ")
	if err != nil {
		t.Fatalf("GetRequest HTTP: %v", err)
	}
	if httpDetails.HTTP == nil || httpDetails.HTTP.URL != "https://api.example.com/users" || httpDetails.HTTP.BodyText != `{"name":"Ada"}` {
		t.Fatalf("unexpected HTTP details: %+v", httpDetails)
	}
	webSocketDetails, err := svc.GetRequest(webSocketRequest.ID)
	if err != nil {
		t.Fatalf("GetRequest WebSocket: %v", err)
	}
	if webSocketDetails.WebSocket == nil || webSocketDetails.WebSocket.DraftText != "ping" {
		t.Fatalf("unexpected WebSocket details: %+v", webSocketDetails)
	}

	httpDetails.HTTP.BodyText = "changed"
	reloadedHTTPDetails, err := svc.GetRequest(httpRequest.ID)
	if err != nil {
		t.Fatalf("GetRequest HTTP after mutation: %v", err)
	}
	if reloadedHTTPDetails.HTTP == nil || reloadedHTTPDetails.HTTP.BodyText != `{"name":"Ada"}` {
		t.Fatalf("expected GetRequest to return an isolated clone, got %+v", reloadedHTTPDetails)
	}
	if _, err := svc.GetRequest(" "); err == nil {
		t.Fatal("expected blank request ID to fail")
	}
	if _, err := svc.GetRequest("missing-request"); err == nil {
		t.Fatal("expected missing request ID to fail")
	}
}

func TestSavedHTTPRequestSettingsPersistAndLegacyPayloadUsesDefaults(t *testing.T) {
	svc := newLoadedTestService(t)
	folder, err := svc.CreateFolder("", "Protocols")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := svc.SaveHTTPRequest(folder.ID, "HTTP 2", &SavedHTTPRequest{
		Method:           "GET",
		URL:              "https://example.test/",
		BodyType:         "none",
		ProxyMode:        proxyservice.SendRequestProxyModeNone,
		Protocol:         proxyservice.SendRequestProtocolHTTP2,
		TLSClientHelloID: proxyservice.TLSClientHelloSafariAuto,
		HTTP2Fingerprint: "1:65536;3:1000;4:6291456;6:262144|15663105|0|m,a,s,p",
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := svc.GetRequest(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.HTTP == nil || loaded.HTTP.Protocol != proxyservice.SendRequestProtocolHTTP2 {
		t.Fatalf("saved protocol = %+v, want http2", loaded.HTTP)
	}
	if loaded.HTTP.TLSClientHelloID != proxyservice.TLSClientHelloSafariAuto {
		t.Fatalf("saved TLS ClientHello = %q, want safari_auto", loaded.HTTP.TLSClientHelloID)
	}
	if loaded.HTTP.HTTP2Fingerprint != "1:65536;3:1000;4:6291456;6:262144|15663105|0|m,a,s,p" {
		t.Fatalf("saved HTTP/2 fingerprint = %q", loaded.HTTP.HTTP2Fingerprint)
	}
	cloned := cloneSavedHTTPRequest(loaded.HTTP)
	if cloned.Protocol != proxyservice.SendRequestProtocolHTTP2 {
		t.Fatalf("cloned protocol = %q, want http2", cloned.Protocol)
	}
	if cloned.TLSClientHelloID != proxyservice.TLSClientHelloSafariAuto {
		t.Fatalf("cloned TLS ClientHello = %q, want safari_auto", cloned.TLSClientHelloID)
	}
	if cloned.HTTP2Fingerprint != loaded.HTTP.HTTP2Fingerprint {
		t.Fatalf("cloned HTTP/2 fingerprint = %q, want %q", cloned.HTTP2Fingerprint, loaded.HTTP.HTTP2Fingerprint)
	}

	legacy, err := requestFromPayload(APICollectionNodeTypeHTTP, `{"method":"GET","url":"https://legacy.test/","bodyType":"none","bodyText":"","proxyMode":"none","timeoutMs":0}`)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.HTTP == nil || legacy.HTTP.Protocol != proxyservice.SendRequestProtocolAuto {
		t.Fatalf("legacy protocol = %+v, want auto", legacy.HTTP)
	}
	if legacy.HTTP.TLSClientHelloID != proxyservice.TLSClientHelloGolang {
		t.Fatalf("legacy TLS ClientHello = %q, want golang", legacy.HTTP.TLSClientHelloID)
	}
	if legacy.HTTP.HTTP2Fingerprint != "" {
		t.Fatalf("legacy HTTP/2 fingerprint = %q, want empty", legacy.HTTP.HTTP2Fingerprint)
	}
}

func TestSavedHTTPInlineScriptSourcePersistsAndLegacyPayloadRemainsUnset(t *testing.T) {
	svc := newLoadedTestService(t)
	folder, err := svc.CreateFolder("", "Scripts")
	if err != nil {
		t.Fatal(err)
	}

	source := "def onRequest(context, request):\n    return request\n"
	saved, err := svc.SaveHTTPRequest(folder.ID, "Scripted request", &SavedHTTPRequest{
		Method:             "GET",
		URL:                "https://example.test/",
		BodyType:           "none",
		ProxyMode:          proxyservice.SendRequestProxyModeNone,
		InlineScriptSource: &source,
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.HTTP == nil || saved.HTTP.InlineScriptSource == nil || *saved.HTTP.InlineScriptSource != source {
		t.Fatalf("saved inline script = %+v, want %q", saved.HTTP, source)
	}

	loaded, err := svc.GetRequest(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.HTTP == nil || loaded.HTTP.InlineScriptSource == nil || *loaded.HTTP.InlineScriptSource != source {
		t.Fatalf("loaded inline script = %+v, want %q", loaded.HTTP, source)
	}
	cloned := cloneSavedHTTPRequest(loaded.HTTP)
	if cloned.InlineScriptSource == loaded.HTTP.InlineScriptSource {
		t.Fatal("cloned inline script source aliases the original pointer")
	}

	emptySource := ""
	updated, err := svc.UpdateHTTPRequest(saved.ID, &SavedHTTPRequest{
		Method:             "GET",
		URL:                "https://example.test/",
		BodyType:           "none",
		ProxyMode:          proxyservice.SendRequestProxyModeNone,
		InlineScriptSource: &emptySource,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.HTTP == nil || updated.HTTP.InlineScriptSource == nil || *updated.HTTP.InlineScriptSource != "" {
		t.Fatalf("updated empty inline script = %+v, want explicit empty source", updated.HTTP)
	}

	legacy, err := requestFromPayload(APICollectionNodeTypeHTTP, `{"method":"GET","url":"https://legacy.test/","bodyType":"none","bodyText":"","proxyMode":"none","timeoutMs":0}`)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.HTTP == nil || legacy.HTTP.InlineScriptSource != nil {
		t.Fatalf("legacy inline script = %+v, want nil", legacy.HTTP)
	}
}

func TestSaveRequestAutoRenamesDuplicateSiblingNames(t *testing.T) {
	svc := newLoadedTestService(t)
	folder, err := svc.CreateFolder("", "Folder")
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if _, err := svc.CreateFolder("", " folder "); err == nil {
		t.Fatal("expected duplicate root folder name to fail")
	}
	first, err := svc.SaveHTTPRequest(folder.ID, "Get Users", &SavedHTTPRequest{
		Method:    "GET",
		URL:       "https://api.example.com/users",
		BodyType:  "none",
		ProxyMode: "none",
	})
	if err != nil {
		t.Fatalf("SaveHTTPRequest first: %v", err)
	}
	if first.Name != "Get Users" {
		t.Fatalf("first request name = %q, want Get Users", first.Name)
	}
	second, err := svc.SaveWebSocketRequest(folder.ID, " get users ", &SavedWebSocketRequest{
		URL:       "wss://api.example.com/users",
		DraftType: "text",
		ProxyMode: "none",
	})
	if err != nil {
		t.Fatalf("SaveWebSocketRequest duplicate: %v", err)
	}
	if second.Name != "get users (1)" {
		t.Fatalf("duplicate request name = %q, want get users (1)", second.Name)
	}
	if _, err := svc.CreateFolder(folder.ID, " get users "); err == nil {
		t.Fatal("expected duplicate folder/request sibling name to fail")
	}
}

func TestSaveRequestAutoRenameSkipsExistingSuffixes(t *testing.T) {
	svc := newLoadedTestService(t)
	folder, err := svc.CreateFolder("", "Folder")
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if _, err := svc.CreateFolder(folder.ID, "edge.microsoft.com (1)"); err != nil {
		t.Fatalf("CreateFolder suffix sibling: %v", err)
	}
	if _, err := svc.SaveHTTPRequest(folder.ID, "edge.microsoft.com", &SavedHTTPRequest{
		Method:    "GET",
		URL:       "https://edge.microsoft.com",
		BodyType:  "none",
		ProxyMode: "none",
	}); err != nil {
		t.Fatalf("SaveHTTPRequest base: %v", err)
	}

	renamed, err := svc.SaveHTTPRequest(folder.ID, "edge.microsoft.com", &SavedHTTPRequest{
		Method:    "GET",
		URL:       "https://edge.microsoft.com",
		BodyType:  "none",
		ProxyMode: "none",
	})
	if err != nil {
		t.Fatalf("SaveHTTPRequest duplicate: %v", err)
	}
	if renamed.Name != "edge.microsoft.com (2)" {
		t.Fatalf("renamed request name = %q, want edge.microsoft.com (2)", renamed.Name)
	}
}

func TestRenameRootFolderReturnsFolderEntry(t *testing.T) {
	svc := newLoadedTestService(t)
	folder, err := svc.CreateFolder("", "Folder")
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	entry, err := svc.RenameNode(folder.ID, "Renamed")
	if err != nil {
		t.Fatalf("RenameNode: %v", err)
	}
	if entry.Folder == nil || entry.Request != nil {
		t.Fatalf("expected folder entry, got %+v", entry)
	}
	if entry.Folder.ID != folder.ID || entry.Folder.Name != "Renamed" {
		t.Fatalf("unexpected renamed folder: %+v", entry.Folder)
	}
	if entry.Folder.SortOrder != folder.SortOrder || entry.Folder.CreatedAt != folder.CreatedAt || entry.Folder.UpdatedAt < folder.UpdatedAt {
		t.Fatalf("renamed folder lost metadata: before=%+v after=%+v", folder, entry.Folder)
	}

	tree, err := svc.ListCollection()
	if err != nil {
		t.Fatalf("ListCollection: %v", err)
	}
	if len(tree.Folders) != 1 || tree.Folders[0].Name != "Renamed" {
		t.Fatalf("unexpected tree after rename: %+v", tree.Folders)
	}
}

func TestRenameNestedFolderRejectsDuplicateSiblingName(t *testing.T) {
	svc := newLoadedTestService(t)
	parent, err := svc.CreateFolder("", "Parent")
	if err != nil {
		t.Fatalf("CreateFolder parent: %v", err)
	}
	first, err := svc.CreateFolder(parent.ID, "First")
	if err != nil {
		t.Fatalf("CreateFolder first: %v", err)
	}
	if _, err := svc.CreateFolder(parent.ID, "Second"); err != nil {
		t.Fatalf("CreateFolder second: %v", err)
	}

	if _, err := svc.RenameNode(first.ID, " second "); err == nil {
		t.Fatal("expected duplicate nested folder rename to fail")
	}
}

func TestRenameRequestReturnsRequestEntry(t *testing.T) {
	svc := newLoadedTestService(t)
	folder, err := svc.CreateFolder("", "Folder")
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	request, err := svc.SaveHTTPRequest(folder.ID, "Get Users", &SavedHTTPRequest{
		Method:    "GET",
		URL:       "https://api.example.com/users",
		BodyType:  "none",
		ProxyMode: "none",
	})
	if err != nil {
		t.Fatalf("SaveHTTPRequest: %v", err)
	}

	entry, err := svc.RenameNode(request.ID, "List Users")
	if err != nil {
		t.Fatalf("RenameNode: %v", err)
	}
	if entry.Request == nil || entry.Folder != nil {
		t.Fatalf("expected request entry, got %+v", entry)
	}
	if entry.Request.ID != request.ID || entry.Request.Name != "List Users" {
		t.Fatalf("unexpected renamed request: %+v", entry.Request)
	}
	if entry.Request.HTTP == nil || entry.Request.HTTP.Method != "GET" {
		t.Fatalf("expected renamed request to keep payload, got %+v", entry.Request)
	}
}

func TestRenameRequestRejectsDuplicateSiblingRequestName(t *testing.T) {
	svc := newLoadedTestService(t)
	folder, err := svc.CreateFolder("", "Folder")
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	first, err := svc.SaveHTTPRequest(folder.ID, "First", &SavedHTTPRequest{
		Method:    "GET",
		URL:       "https://api.example.com/first",
		BodyType:  "none",
		ProxyMode: "none",
	})
	if err != nil {
		t.Fatalf("SaveHTTPRequest first: %v", err)
	}
	if _, err := svc.SaveWebSocketRequest(folder.ID, "Second", &SavedWebSocketRequest{
		URL:       "wss://api.example.com/second",
		DraftType: "text",
		ProxyMode: "none",
	}); err != nil {
		t.Fatalf("SaveWebSocketRequest second: %v", err)
	}

	if _, err := svc.RenameNode(first.ID, " second "); err == nil {
		t.Fatal("expected duplicate request rename to fail")
	}
}

func TestMoveRequestAcrossFoldersPreservesPayloadAndManagedFile(t *testing.T) {
	baseDir := configureTestStorage(t)
	sourceDir := filepath.Join(baseDir, "request-draft-cache")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll source dir: %v", err)
	}
	sourcePath := filepath.Join(sourceDir, "move-request.bin")
	fileContents := []byte("move request body")
	if err := os.WriteFile(sourcePath, fileContents, 0o644); err != nil {
		t.Fatalf("WriteFile source: %v", err)
	}

	svc := newTestService(t)
	if err := svc.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	sourceFolder, err := svc.CreateFolder("", "Source")
	if err != nil {
		t.Fatalf("CreateFolder source: %v", err)
	}
	destinationFolder, err := svc.CreateFolder("", "Destination")
	if err != nil {
		t.Fatalf("CreateFolder destination: %v", err)
	}
	existing, err := svc.SaveWebSocketRequest(destinationFolder.ID, "Existing", &SavedWebSocketRequest{
		URL:       "wss://example.com/existing",
		DraftType: "text",
		ProxyMode: "none",
	})
	if err != nil {
		t.Fatalf("SaveWebSocketRequest existing: %v", err)
	}
	moved, err := svc.SaveHTTPRequest(sourceFolder.ID, "Moved", &SavedHTTPRequest{
		Method:    "POST",
		URL:       "https://example.com/moved",
		BodyType:  "file",
		BodyFile:  &SavedFile{Path: sourcePath, Name: "move-request.bin", Size: int64(len(fileContents))},
		ProxyMode: "none",
	})
	if err != nil {
		t.Fatalf("SaveHTTPRequest moved: %v", err)
	}
	managedPath := moved.HTTP.BodyFile.Path

	if err := svc.MoveNode("  "+moved.ID+"  ", "  "+destinationFolder.ID+"  "); err != nil {
		t.Fatalf("MoveNode: %v", err)
	}

	tree, err := svc.ListCollection()
	if err != nil {
		t.Fatalf("ListCollection: %v", err)
	}
	sourceAfter := findFolderByID(tree.Folders, sourceFolder.ID)
	destinationAfter := findFolderByID(tree.Folders, destinationFolder.ID)
	if sourceAfter == nil || destinationAfter == nil {
		t.Fatalf("missing source or destination after move: %+v", tree.Folders)
	}
	if len(sourceAfter.Requests) != 0 {
		t.Fatalf("source requests after move = %+v, want none", sourceAfter.Requests)
	}
	if len(destinationAfter.Requests) != 2 {
		t.Fatalf("destination requests after move = %+v, want two", destinationAfter.Requests)
	}
	if destinationAfter.Requests[0].ID != existing.ID || destinationAfter.Requests[1].ID != moved.ID {
		t.Fatalf("destination request order = %+v, want existing then moved", destinationAfter.Requests)
	}
	if destinationAfter.Requests[1].SortOrder != existing.SortOrder+1 {
		t.Fatalf("moved sort order = %d, want %d", destinationAfter.Requests[1].SortOrder, existing.SortOrder+1)
	}

	loaded, err := svc.GetRequest(moved.ID)
	if err != nil {
		t.Fatalf("GetRequest moved: %v", err)
	}
	if loaded.ID != moved.ID || loaded.ParentID != destinationFolder.ID || loaded.CreatedAt != moved.CreatedAt {
		t.Fatalf("moved request identity changed: before=%+v after=%+v", moved, loaded)
	}
	if loaded.HTTP == nil || loaded.HTTP.URL != moved.HTTP.URL || loaded.HTTP.BodyFile == nil || loaded.HTTP.BodyFile.Path != managedPath {
		t.Fatalf("moved request payload changed: before=%+v after=%+v", moved.HTTP, loaded.HTTP)
	}
	assertFileBytes(t, managedPath, fileContents)
}

func TestMoveFolderSubtreeAndMoveItBackToRoot(t *testing.T) {
	svc := newLoadedTestService(t)
	sourceRoot, err := svc.CreateFolder("", "Source Root")
	if err != nil {
		t.Fatalf("CreateFolder source root: %v", err)
	}
	destinationRoot, err := svc.CreateFolder("", "Destination Root")
	if err != nil {
		t.Fatalf("CreateFolder destination root: %v", err)
	}
	existingChild, err := svc.CreateFolder(destinationRoot.ID, "Existing Child")
	if err != nil {
		t.Fatalf("CreateFolder existing child: %v", err)
	}
	branch, err := svc.CreateFolder(sourceRoot.ID, "Branch")
	if err != nil {
		t.Fatalf("CreateFolder branch: %v", err)
	}
	descendant, err := svc.CreateFolder(branch.ID, "Descendant")
	if err != nil {
		t.Fatalf("CreateFolder descendant: %v", err)
	}
	request, err := svc.SaveHTTPRequest(descendant.ID, "Nested Request", &SavedHTTPRequest{
		Method:    "GET",
		URL:       "https://example.com/nested",
		BodyType:  "none",
		ProxyMode: "none",
	})
	if err != nil {
		t.Fatalf("SaveHTTPRequest nested: %v", err)
	}

	if err := svc.MoveNode(branch.ID, destinationRoot.ID); err != nil {
		t.Fatalf("MoveNode into destination: %v", err)
	}
	tree, err := svc.ListCollection()
	if err != nil {
		t.Fatalf("ListCollection nested: %v", err)
	}
	sourceAfter := findFolderByID(tree.Folders, sourceRoot.ID)
	destinationAfter := findFolderByID(tree.Folders, destinationRoot.ID)
	if sourceAfter == nil || len(sourceAfter.Folders) != 0 {
		t.Fatalf("source folders after move = %+v, want none", sourceAfter)
	}
	if destinationAfter == nil || len(destinationAfter.Folders) != 2 {
		t.Fatalf("destination folders after move = %+v, want two", destinationAfter)
	}
	if destinationAfter.Folders[0].ID != existingChild.ID || destinationAfter.Folders[1].ID != branch.ID {
		t.Fatalf("destination folder order = %+v, want existing then branch", destinationAfter.Folders)
	}
	movedBranch := destinationAfter.Folders[1]
	if movedBranch.SortOrder != existingChild.SortOrder+1 || findFolderByID(movedBranch.Folders, descendant.ID) == nil || findRequestByID(movedBranch.Folders, request.ID) == nil {
		t.Fatalf("moved branch lost order or descendants: %+v", movedBranch)
	}

	if err := svc.MoveNode(branch.ID, ""); err != nil {
		t.Fatalf("MoveNode to root: %v", err)
	}
	tree, err = svc.ListCollection()
	if err != nil {
		t.Fatalf("ListCollection root: %v", err)
	}
	if len(tree.Folders) != 3 || tree.Folders[2].ID != branch.ID {
		t.Fatalf("root folders after move = %+v, want branch appended", tree.Folders)
	}
	if findFolderByID(tree.Folders[2].Folders, descendant.ID) == nil || findRequestByID(tree.Folders[2].Folders, request.ID) == nil {
		t.Fatalf("root branch lost descendants: %+v", tree.Folders[2])
	}
}

func TestMoveNodeRejectsInvalidTargetsWithoutChangingParents(t *testing.T) {
	svc := newLoadedTestService(t)
	root, err := svc.CreateFolder("", "Root")
	if err != nil {
		t.Fatalf("CreateFolder root: %v", err)
	}
	child, err := svc.CreateFolder(root.ID, "Child")
	if err != nil {
		t.Fatalf("CreateFolder child: %v", err)
	}
	descendant, err := svc.CreateFolder(child.ID, "Descendant")
	if err != nil {
		t.Fatalf("CreateFolder descendant: %v", err)
	}
	destination, err := svc.CreateFolder("", "Destination")
	if err != nil {
		t.Fatalf("CreateFolder destination: %v", err)
	}
	if _, err := svc.CreateFolder(destination.ID, "Duplicate"); err != nil {
		t.Fatalf("CreateFolder duplicate target: %v", err)
	}
	request, err := svc.SaveHTTPRequest(root.ID, "Duplicate", &SavedHTTPRequest{
		Method:    "GET",
		URL:       "https://example.com/duplicate",
		BodyType:  "none",
		ProxyMode: "none",
	})
	if err != nil {
		t.Fatalf("SaveHTTPRequest: %v", err)
	}

	tests := []struct {
		name       string
		nodeID     string
		parentID   string
		trackedID  string
		wantParent string
	}{
		{name: "request to root", nodeID: request.ID, parentID: "", trackedID: request.ID, wantParent: root.ID},
		{name: "folder to itself", nodeID: child.ID, parentID: child.ID, trackedID: child.ID, wantParent: root.ID},
		{name: "folder to descendant", nodeID: root.ID, parentID: descendant.ID, trackedID: root.ID, wantParent: ""},
		{name: "target is request", nodeID: child.ID, parentID: request.ID, trackedID: child.ID, wantParent: root.ID},
		{name: "missing target", nodeID: child.ID, parentID: "missing-folder", trackedID: child.ID, wantParent: root.ID},
		{name: "duplicate destination name", nodeID: request.ID, parentID: destination.ID, trackedID: request.ID, wantParent: root.ID},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := svc.MoveNode(test.nodeID, test.parentID); err == nil {
				t.Fatal("expected MoveNode to fail")
			}
			if got := apiNodeParentID(t, svc, test.trackedID); got != test.wantParent {
				t.Fatalf("parent after failed move = %q, want %q", got, test.wantParent)
			}
		})
	}
	if err := svc.MoveNode("", destination.ID); err == nil {
		t.Fatal("expected empty node id to fail")
	}
	if err := svc.MoveNode("missing-node", destination.ID); err == nil {
		t.Fatal("expected missing node id to fail")
	}
}

func TestMoveNodeToCurrentParentIsNoOp(t *testing.T) {
	svc := newLoadedTestService(t)
	folder, err := svc.CreateFolder("", "Folder")
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	request, err := svc.SaveHTTPRequest(folder.ID, "Request", &SavedHTTPRequest{
		Method:    "GET",
		URL:       "https://example.com",
		BodyType:  "none",
		ProxyMode: "none",
	})
	if err != nil {
		t.Fatalf("SaveHTTPRequest: %v", err)
	}

	if err := svc.MoveNode(request.ID, folder.ID); err != nil {
		t.Fatalf("MoveNode current parent: %v", err)
	}
	loaded, err := svc.GetRequest(request.ID)
	if err != nil {
		t.Fatalf("GetRequest: %v", err)
	}
	if loaded.ParentID != folder.ID || loaded.SortOrder != request.SortOrder || loaded.UpdatedAt != request.UpdatedAt {
		t.Fatalf("no-op move changed request: before=%+v after=%+v", request, loaded)
	}
}

func TestRejectsWrongUpdateType(t *testing.T) {
	svc := newLoadedTestService(t)
	folder, err := svc.CreateFolder("", "Folder")
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	node, err := svc.SaveWebSocketRequest(folder.ID, "WS", &SavedWebSocketRequest{
		URL:       "wss://api.example.com",
		DraftType: "text",
		ProxyMode: "none",
	})
	if err != nil {
		t.Fatalf("SaveWebSocketRequest: %v", err)
	}
	if _, err := svc.UpdateHTTPRequest(node.ID, &SavedHTTPRequest{
		Method:    "GET",
		URL:       "https://api.example.com",
		BodyType:  "none",
		ProxyMode: "none",
	}); err == nil {
		t.Fatal("expected wrong update type to fail")
	}
}

func TestDeleteFolderRecursivelyFromTree(t *testing.T) {
	svc := newLoadedTestService(t)
	parent, err := svc.CreateFolder("", "A")
	if err != nil {
		t.Fatalf("CreateFolder parent: %v", err)
	}
	child, err := svc.CreateFolder(parent.ID, "B")
	if err != nil {
		t.Fatalf("CreateFolder child: %v", err)
	}
	if _, err := svc.SaveHTTPRequest(child.ID, "Request", &SavedHTTPRequest{
		Method:    "GET",
		URL:       "https://api.example.com",
		BodyType:  "none",
		ProxyMode: "none",
	}); err != nil {
		t.Fatalf("SaveHTTPRequest: %v", err)
	}

	if err := svc.DeleteNode(parent.ID); err != nil {
		t.Fatalf("DeleteNode: %v", err)
	}
	tree, err := svc.ListCollection()
	if err != nil {
		t.Fatalf("ListCollection: %v", err)
	}
	if len(tree.Folders) != 0 {
		t.Fatalf("expected recursive delete to remove all folders, got %+v", tree.Folders)
	}
}

func TestDeleteNodesDeletesAcrossBranchesAndCleansManagedFiles(t *testing.T) {
	baseDir := configureTestStorage(t)
	sourceDir := filepath.Join(baseDir, "request-draft-cache")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll source dir: %v", err)
	}

	ancestorSourcePath := filepath.Join(sourceDir, "ancestor.bin")
	branchSourcePath := filepath.Join(sourceDir, "branch.bin")
	keepSourcePath := filepath.Join(sourceDir, "keep.bin")
	for path, content := range map[string][]byte{
		ancestorSourcePath: []byte("ancestor"),
		branchSourcePath:   []byte("branch"),
		keepSourcePath:     []byte("keep"),
	} {
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("WriteFile(%q): %v", path, err)
		}
	}

	svc := newTestService(t)
	if err := svc.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	branchA, err := svc.CreateFolder("", "Branch A")
	if err != nil {
		t.Fatalf("CreateFolder branch A: %v", err)
	}
	ancestor, err := svc.CreateFolder(branchA.ID, "Delete Ancestor")
	if err != nil {
		t.Fatalf("CreateFolder ancestor: %v", err)
	}
	descendantFolder, err := svc.CreateFolder(ancestor.ID, "Descendant")
	if err != nil {
		t.Fatalf("CreateFolder descendant: %v", err)
	}
	descendantRequest, err := svc.SaveHTTPRequest(descendantFolder.ID, "Ancestor File", &SavedHTTPRequest{
		Method:    "POST",
		URL:       "https://example.com/ancestor",
		BodyType:  "file",
		BodyFile:  &SavedFile{Path: ancestorSourcePath, Name: "ancestor.bin", Size: 8},
		ProxyMode: "none",
	})
	if err != nil {
		t.Fatalf("SaveHTTPRequest descendant: %v", err)
	}

	branchB, err := svc.CreateFolder("", "Branch B")
	if err != nil {
		t.Fatalf("CreateFolder branch B: %v", err)
	}
	branchRequest, err := svc.SaveHTTPRequest(branchB.ID, "Branch File", &SavedHTTPRequest{
		Method:    "POST",
		URL:       "https://example.com/branch",
		BodyType:  "file",
		BodyFile:  &SavedFile{Path: branchSourcePath, Name: "branch.bin", Size: 6},
		ProxyMode: "none",
	})
	if err != nil {
		t.Fatalf("SaveHTTPRequest branch: %v", err)
	}
	keepRequest, err := svc.SaveHTTPRequest(branchB.ID, "Keep File", &SavedHTTPRequest{
		Method:    "POST",
		URL:       "https://example.com/keep",
		BodyType:  "file",
		BodyFile:  &SavedFile{Path: keepSourcePath, Name: "keep.bin", Size: 4},
		ProxyMode: "none",
	})
	if err != nil {
		t.Fatalf("SaveHTTPRequest keep: %v", err)
	}

	ancestorManagedPath := descendantRequest.HTTP.BodyFile.Path
	branchManagedPath := branchRequest.HTTP.BodyFile.Path
	keepManagedPath := keepRequest.HTTP.BodyFile.Path
	if err := svc.DeleteNodes([]string{
		"  " + ancestor.ID + "  ",
		descendantRequest.ID,
		branchRequest.ID,
		"\t" + branchRequest.ID + "\n",
		ancestor.ID,
	}); err != nil {
		t.Fatalf("DeleteNodes: %v", err)
	}

	tree, err := svc.ListCollection()
	if err != nil {
		t.Fatalf("ListCollection: %v", err)
	}
	if findFolderByID(tree.Folders, branchA.ID) == nil || findFolderByID(tree.Folders, branchB.ID) == nil {
		t.Fatalf("expected unrelated root folders to remain: %+v", tree.Folders)
	}
	if findFolderByID(tree.Folders, ancestor.ID) != nil {
		t.Fatalf("expected selected ancestor %q to be removed", ancestor.ID)
	}
	if findRequestByID(tree.Folders, descendantRequest.ID) != nil {
		t.Fatalf("expected descendant request %q to be removed recursively", descendantRequest.ID)
	}
	if findRequestByID(tree.Folders, branchRequest.ID) != nil {
		t.Fatalf("expected request %q from another branch to be removed", branchRequest.ID)
	}
	if findRequestByID(tree.Folders, keepRequest.ID) == nil {
		t.Fatalf("expected unrelated request %q to remain", keepRequest.ID)
	}

	for _, deletedPath := range []string{ancestorManagedPath, branchManagedPath} {
		if _, err := os.Stat(deletedPath); !os.IsNotExist(err) {
			t.Fatalf("managed file %q should be removed after batch delete, got %v", deletedPath, err)
		}
	}
	assertFileBytes(t, keepManagedPath, []byte("keep"))
}

func TestDeleteNodesMissingIDLeavesValidNodeAndManagedFileUntouched(t *testing.T) {
	baseDir := configureTestStorage(t)
	sourceDir := filepath.Join(baseDir, "request-draft-cache")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll source dir: %v", err)
	}
	sourcePath := filepath.Join(sourceDir, "atomic.bin")
	if err := os.WriteFile(sourcePath, []byte("atomic"), 0o644); err != nil {
		t.Fatalf("WriteFile source: %v", err)
	}

	svc := newTestService(t)
	if err := svc.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	folder, err := svc.CreateFolder("", "Atomic")
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	request, err := svc.SaveHTTPRequest(folder.ID, "Keep Request", &SavedHTTPRequest{
		Method:    "POST",
		URL:       "https://example.com/atomic",
		BodyType:  "file",
		BodyFile:  &SavedFile{Path: sourcePath, Name: "atomic.bin", Size: 6},
		ProxyMode: "none",
	})
	if err != nil {
		t.Fatalf("SaveHTTPRequest: %v", err)
	}
	managedPath := request.HTTP.BodyFile.Path

	err = svc.DeleteNodes([]string{request.ID, "missing-node"})
	if err == nil {
		t.Fatal("expected DeleteNodes with a missing ID to fail")
	}
	if !strings.Contains(err.Error(), "missing-node") {
		t.Fatalf("expected missing ID in error, got %v", err)
	}

	tree, err := svc.ListCollection()
	if err != nil {
		t.Fatalf("ListCollection: %v", err)
	}
	if findRequestByID(tree.Folders, request.ID) == nil {
		t.Fatalf("expected valid request %q to remain in memory", request.ID)
	}
	assertFileBytes(t, managedPath, []byte("atomic"))

	reloaded := newTestService(t)
	if err := reloaded.Load(); err != nil {
		t.Fatalf("Load reloaded: %v", err)
	}
	reloadedTree, err := reloaded.ListCollection()
	if err != nil {
		t.Fatalf("ListCollection reloaded: %v", err)
	}
	if findRequestByID(reloadedTree.Folders, request.ID) == nil {
		t.Fatalf("expected valid request %q to remain after reload", request.ID)
	}
	assertFileBytes(t, managedPath, []byte("atomic"))
}
