package apicollectionservice

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	"sync"

	"github.com/josexy/flowlens/backend/pkg/logger"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type APICollectionService struct {
	loadMu     sync.Mutex
	mutationMu sync.Mutex
	loaded     bool
	repository *collectionRepository
}

func New(db *sql.DB) *APICollectionService {
	return &APICollectionService{repository: newCollectionRepository(db)}
}

func (s *APICollectionService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	return s.Load()
}

func (s *APICollectionService) ServiceShutdown() error {
	return nil
}

func (s *APICollectionService) Load() error {
	s.loadMu.Lock()
	defer s.loadMu.Unlock()
	if s.loaded {
		return nil
	}
	if s.repository == nil || s.repository.db == nil {
		return errors.New("api collection database is not available")
	}
	referenced, err := s.repository.referencedManagedFilePaths(context.Background())
	if err != nil {
		return err
	}
	if err := reconcileCollectionFiles(referenced); err != nil {
		return err
	}
	s.loaded = true
	return nil
}

func (s *APICollectionService) ListCollection() (*APICollectionTree, error) {
	if err := s.ensureLoaded(); err != nil {
		return nil, err
	}
	return s.repository.listCollection(context.Background())
}

func (s *APICollectionService) GetRequest(id string) (*APICollectionRequest, error) {
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return nil, errors.New("request id cannot be empty")
	}

	if err := s.ensureLoaded(); err != nil {
		return nil, err
	}
	return s.repository.getRequest(context.Background(), trimmedID)
}

func (s *APICollectionService) CreateFolder(parentID string, name string) (*APICollectionFolder, error) {
	if err := s.ensureLoaded(); err != nil {
		return nil, err
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	folder, err := s.repository.createFolder(context.Background(), parentID, name)
	if err != nil {
		return nil, err
	}
	logger.G().Infof(
		"API collection folder created: id=%s parent_id=%s name=%q",
		folder.ID,
		strings.TrimSpace(parentID),
		folder.Name,
	)
	return folder, nil
}

func (s *APICollectionService) RenameNode(id string, name string) (*APICollectionEntry, error) {
	if err := s.ensureLoaded(); err != nil {
		return nil, err
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	entry, err := s.repository.renameNode(context.Background(), id, name)
	if err != nil {
		return nil, err
	}
	logger.G().Infof("API collection node renamed: id=%s new_name=%q", strings.TrimSpace(id), strings.TrimSpace(name))
	return entry, nil
}

func (s *APICollectionService) MoveNode(id string, newParentID string) error {
	if err := s.ensureLoaded(); err != nil {
		return err
	}
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return errors.New("node id cannot be empty")
	}
	trimmedParentID := strings.TrimSpace(newParentID)

	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	moved, err := s.repository.moveNode(context.Background(), trimmedID, trimmedParentID)
	if err != nil {
		return err
	}
	if moved {
		logger.G().Infof(
			"API collection node moved: id=%s new_parent_id=%s",
			trimmedID,
			trimmedParentID,
		)
	}
	return nil
}

func (s *APICollectionService) DeleteNode(id string) error {
	return s.DeleteNodes([]string{id})
}

func (s *APICollectionService) DeleteNodes(ids []string) error {
	if err := s.ensureLoaded(); err != nil {
		return err
	}
	if len(ids) == 0 {
		return errors.New("node ids cannot be empty")
	}
	normalizedIDs := make([]string, 0, len(ids))
	seenIDs := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		trimmedID := strings.TrimSpace(id)
		if trimmedID == "" {
			return errors.New("node id cannot be empty")
		}
		if _, exists := seenIDs[trimmedID]; exists {
			continue
		}
		seenIDs[trimmedID] = struct{}{}
		normalizedIDs = append(normalizedIDs, trimmedID)
	}

	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	deletedFiles, err := s.repository.deleteNodes(context.Background(), normalizedIDs)
	if err != nil {
		return err
	}
	s.removeUnreferencedManagedFiles(deletedFiles)
	logger.G().Infof(
		"API collection nodes deleted: requested=%d unique=%d ids=%v",
		len(ids),
		len(normalizedIDs),
		normalizedIDs,
	)
	return nil
}

func (s *APICollectionService) SaveHTTPRequest(parentID string, name string, request *SavedHTTPRequest) (*APICollectionRequest, error) {
	return s.saveRequestNode(parentID, name, APICollectionNodeTypeHTTP, request, nil)
}

func (s *APICollectionService) SaveWebSocketRequest(parentID string, name string, request *SavedWebSocketRequest) (*APICollectionRequest, error) {
	return s.saveRequestNode(parentID, name, APICollectionNodeTypeWebSocket, nil, request)
}

func (s *APICollectionService) UpdateHTTPRequest(id string, request *SavedHTTPRequest) (*APICollectionRequest, error) {
	if err := s.ensureLoaded(); err != nil {
		return nil, err
	}
	if request == nil {
		return nil, errors.New("http request cannot be nil")
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	previous, err := s.repository.getRequest(context.Background(), id)
	if err != nil {
		return nil, err
	}
	nextHTTP := cloneSavedHTTPRequest(request)
	createdFiles, _, err := materializeSavedHTTPRequestFiles(nextHTTP)
	if err != nil {
		return nil, err
	}
	updated, err := s.repository.updateHTTPRequest(context.Background(), id, nextHTTP)
	if err != nil {
		cleanupCollectionFiles(createdFiles)
		return nil, err
	}
	s.removeUnreferencedManagedFiles(mapKeys(collectRequestManagedFilePaths(previous)))
	logger.G().Infof(
		"API collection request updated: id=%s type=%s name=%q files_created=%d",
		updated.ID,
		updated.Type,
		updated.Name,
		len(createdFiles),
	)
	return updated, nil
}

func (s *APICollectionService) UpdateWebSocketRequest(id string, request *SavedWebSocketRequest) (*APICollectionRequest, error) {
	if err := s.ensureLoaded(); err != nil {
		return nil, err
	}
	if request == nil {
		return nil, errors.New("websocket request cannot be nil")
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	previous, err := s.repository.getRequest(context.Background(), id)
	if err != nil {
		return nil, err
	}
	nextWebSocket := cloneSavedWebSocketRequest(request)
	createdFiles, _, err := materializeSavedWebSocketRequestFiles(nextWebSocket)
	if err != nil {
		return nil, err
	}
	updated, err := s.repository.updateWebSocketRequest(context.Background(), id, nextWebSocket)
	if err != nil {
		cleanupCollectionFiles(createdFiles)
		return nil, err
	}
	s.removeUnreferencedManagedFiles(mapKeys(collectRequestManagedFilePaths(previous)))
	logger.G().Infof(
		"API collection request updated: id=%s type=%s name=%q files_created=%d",
		updated.ID,
		updated.Type,
		updated.Name,
		len(createdFiles),
	)
	return updated, nil
}

func (s *APICollectionService) saveRequestNode(
	parentID string,
	name string,
	nodeType APICollectionNodeType,
	httpRequest *SavedHTTPRequest,
	websocketRequest *SavedWebSocketRequest,
) (*APICollectionRequest, error) {
	if err := s.ensureLoaded(); err != nil {
		return nil, err
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	switch nodeType {
	case APICollectionNodeTypeHTTP:
		if httpRequest == nil {
			return nil, errors.New("http request cannot be nil")
		}
	case APICollectionNodeTypeWebSocket:
		if websocketRequest == nil {
			return nil, errors.New("websocket request cannot be nil")
		}
	default:
		return nil, fmt.Errorf("unsupported api node type: %s", nodeType)
	}

	var createdFiles []string
	var savedHTTP *SavedHTTPRequest
	var savedWebSocket *SavedWebSocketRequest
	if nodeType == APICollectionNodeTypeHTTP {
		savedHTTP = cloneSavedHTTPRequest(httpRequest)
		nextCreatedFiles, _, err := materializeSavedHTTPRequestFiles(savedHTTP)
		if err != nil {
			return nil, err
		}
		createdFiles = append(createdFiles, nextCreatedFiles...)
	}
	if nodeType == APICollectionNodeTypeWebSocket {
		savedWebSocket = cloneSavedWebSocketRequest(websocketRequest)
		nextCreatedFiles, _, err := materializeSavedWebSocketRequestFiles(savedWebSocket)
		if err != nil {
			cleanupCollectionFiles(createdFiles)
			return nil, err
		}
		createdFiles = append(createdFiles, nextCreatedFiles...)
	}

	var request *APICollectionRequest
	var err error
	if nodeType == APICollectionNodeTypeHTTP {
		request, err = s.repository.saveHTTPRequest(context.Background(), parentID, name, savedHTTP)
	} else {
		request, err = s.repository.saveWebSocketRequest(context.Background(), parentID, name, savedWebSocket)
	}
	if err != nil {
		cleanupCollectionFiles(createdFiles)
		return nil, err
	}
	logger.G().Infof(
		"API collection request created: id=%s parent_id=%s type=%s name=%q files_created=%d",
		request.ID,
		strings.TrimSpace(parentID),
		request.Type,
		request.Name,
		len(createdFiles),
	)
	return request, nil
}

func (s *APICollectionService) ensureLoaded() error {
	s.loadMu.Lock()
	loaded := s.loaded
	s.loadMu.Unlock()
	if loaded {
		return nil
	}
	return s.Load()
}

func (s *APICollectionService) isLoaded() bool {
	s.loadMu.Lock()
	defer s.loadMu.Unlock()
	return s.loaded
}

func collectRequestManagedFilePaths(request *APICollectionRequest) map[string]struct{} {
	paths := make(map[string]struct{})
	collectCollectionRequestManagedFilePaths(request, paths)
	return paths
}

func (s *APICollectionService) removeUnreferencedManagedFiles(candidates []string) {
	if len(candidates) == 0 {
		return
	}
	current, err := s.repository.referencedManagedFilePaths(context.Background())
	if err != nil {
		logger.G().Warnf("API collection query managed file references failed: %v", err)
		return
	}
	previous := make(map[string]struct{}, len(candidates))
	for _, path := range candidates {
		previous[normalizedFilePathKey(path)] = struct{}{}
	}
	removeUnreferencedCollectionFiles(previous, current)
}

func findFolderByID(folders []*APICollectionFolder, id string) *APICollectionFolder {
	for _, folder := range folders {
		if folder == nil {
			continue
		}
		if folder.ID == id {
			return folder
		}
		if found := findFolderByID(folder.Folders, id); found != nil {
			return found
		}
	}
	return nil
}

func findRequestByID(folders []*APICollectionFolder, id string) *APICollectionRequest {
	for _, folder := range folders {
		if folder == nil {
			continue
		}
		for _, request := range folder.Requests {
			if request != nil && request.ID == id {
				return request
			}
		}
		if found := findRequestByID(folder.Folders, id); found != nil {
			return found
		}
	}
	return nil
}

func sortFoldersInPlace(folders []*APICollectionFolder) {
	sort.SliceStable(folders, func(i, j int) bool {
		left := folders[i]
		right := folders[j]
		if left == nil || right == nil {
			return left != nil
		}
		if left.SortOrder != right.SortOrder {
			return left.SortOrder < right.SortOrder
		}
		if normalizeNodeName(left.Name) != normalizeNodeName(right.Name) {
			return normalizeNodeName(left.Name) < normalizeNodeName(right.Name)
		}
		return left.ID < right.ID
	})
	for _, folder := range folders {
		if folder == nil {
			continue
		}
		sortFoldersInPlace(folder.Folders)
		sortRequestsInPlace(folder.Requests)
	}
}

func sortRequestsInPlace(requests []*APICollectionRequest) {
	sort.SliceStable(requests, func(i, j int) bool {
		left := requests[i]
		right := requests[j]
		if left == nil || right == nil {
			return left != nil
		}
		if left.SortOrder != right.SortOrder {
			return left.SortOrder < right.SortOrder
		}
		if left.Type != right.Type {
			return left.Type < right.Type
		}
		if normalizeNodeName(left.Name) != normalizeNodeName(right.Name) {
			return normalizeNodeName(left.Name) < normalizeNodeName(right.Name)
		}
		return left.ID < right.ID
	})
}

func normalizeNodeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func cloneSavedHTTPRequest(request *SavedHTTPRequest) *SavedHTTPRequest {
	if request == nil {
		return nil
	}
	protocol := request.Protocol
	if protocol == "" {
		protocol = proxyservice.SendRequestProtocolAuto
	}
	clientHelloID := request.TLSClientHelloID
	if clientHelloID == "" {
		clientHelloID = proxyservice.TLSClientHelloGolang
	}
	var inlineScriptSource *string
	if request.InlineScriptSource != nil {
		value := *request.InlineScriptSource
		inlineScriptSource = &value
	}
	return &SavedHTTPRequest{
		Method:             request.Method,
		URL:                request.URL,
		Params:             cloneSavedKeyValues(request.Params),
		Headers:            cloneSavedKeyValues(request.Headers),
		BodyType:           request.BodyType,
		BodyText:           request.BodyText,
		BodyFile:           cloneSavedFile(request.BodyFile),
		BodyFormData:       cloneSavedFormDataItems(request.BodyFormData),
		BodyURLEncoded:     cloneSavedKeyValues(request.BodyURLEncoded),
		InlineScriptSource: inlineScriptSource,
		ProxyMode:          request.ProxyMode,
		Protocol:           protocol,
		CustomProxy:        request.CustomProxy,
		TimeoutMs:          request.TimeoutMs,
		TLSClientHelloID:   clientHelloID,
		HTTP2Fingerprint:   request.HTTP2Fingerprint,
	}
}

func cloneSavedWebSocketRequest(request *SavedWebSocketRequest) *SavedWebSocketRequest {
	if request == nil {
		return nil
	}
	clientHelloID := request.TLSClientHelloID
	if clientHelloID == "" {
		clientHelloID = proxyservice.TLSClientHelloGolang
	}
	return &SavedWebSocketRequest{
		URL:              request.URL,
		Params:           cloneSavedKeyValues(request.Params),
		Headers:          cloneSavedKeyValues(request.Headers),
		DraftType:        request.DraftType,
		DraftText:        request.DraftText,
		DraftFile:        cloneSavedFile(request.DraftFile),
		ProxyMode:        request.ProxyMode,
		CustomProxy:      request.CustomProxy,
		TimeoutMs:        request.TimeoutMs,
		TLSClientHelloID: clientHelloID,
	}
}

func cloneSavedKeyValues(items []*SavedKeyValue) []*SavedKeyValue {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]*SavedKeyValue, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		cloned = append(cloned, &SavedKeyValue{
			Key:     item.Key,
			Value:   item.Value,
			Enabled: item.Enabled,
		})
	}
	return cloned
}

func cloneSavedFile(file *SavedFile) *SavedFile {
	if file == nil {
		return nil
	}
	return &SavedFile{
		Path: file.Path,
		Name: file.Name,
		Size: file.Size,
	}
}

func cloneSavedFormDataItems(items []*SavedFormDataItem) []*SavedFormDataItem {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]*SavedFormDataItem, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		cloned = append(cloned, &SavedFormDataItem{
			ID:       item.ID,
			Enabled:  item.Enabled,
			Name:     item.Name,
			ItemType: item.ItemType,
			Value:    item.Value,
			File:     cloneSavedFile(item.File),
		})
	}
	return cloned
}
