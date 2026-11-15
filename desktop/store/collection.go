package store

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"

	apicollectionservice "github.com/josexy/flowlens/backend/services/api_collection_service"
	"github.com/josexy/flowlens/desktop/runtime"
)

// Collection types of the API collection service.
type (
	CollectionTree    = apicollectionservice.APICollectionTree
	CollectionFolder  = apicollectionservice.APICollectionFolder
	CollectionRequest = apicollectionservice.APICollectionRequest
	CollectionEntry   = apicollectionservice.APICollectionEntry
)

const (
	jobCollectionLoad   = "collection:load"
	jobCollectionMutate = "collection:mutate"
	jobCollectionDetail = "collection:detail:"
)

// CollectionBackend reads and changes saved requests.
type CollectionBackend interface {
	SavedRequestBackend
	ListCollection() (*CollectionTree, error)
	GetRequest(id string) (*CollectionRequest, error)
	CreateFolder(parentID, name string) (*CollectionFolder, error)
	RenameNode(id, name string) (*CollectionEntry, error)
	MoveNode(id, newParentID string) error
	DeleteNodes(ids []string) error
}

// CollectionRow is one visible row of the tree.
type CollectionRow struct {
	ID, ParentID, Name string
	Folder             bool
	Depth              int
	Expanded           bool
	HasChildren        bool
	// Children counts a folder's own folders and requests.
	Children int
	Request  *CollectionRequest
}

// CollectionFolderOption is a folder that can receive a request.
type CollectionFolderOption struct {
	ID, Path string
}

// CollectionStore holds the saved request tree.
type CollectionStore struct {
	rt        Runtime
	backend   CollectionBackend
	workspace *WorkspaceStore

	Tree     *CollectionTree
	Loading  bool
	Mutating bool
	Error    string
	Loaded   bool
	// Version changes whenever the tree does.
	Version              uint64
	Expanded             map[string]bool
	SelectedNodeID       string
	LastSelectedFolderID string

	folders  map[string]*CollectionFolder
	requests map[string]*CollectionRequest
	parents  map[string]string
}

// NewCollectionStore returns an unloaded store.
func NewCollectionStore(rt Runtime, backend CollectionBackend, workspace *WorkspaceStore) *CollectionStore {
	s := &CollectionStore{rt: rt, backend: backend, workspace: workspace, Expanded: map[string]bool{}}
	s.setTree(nil)
	return s
}

func folderOrder(left, right *CollectionFolder) int {
	return cmp.Or(cmp.Compare(left.SortOrder, right.SortOrder), compareNames(left.Name, right.Name), cmp.Compare(left.ID, right.ID))
}

func requestOrder(left, right *CollectionRequest) int {
	return cmp.Or(cmp.Compare(left.SortOrder, right.SortOrder), compareNames(left.Name, right.Name), cmp.Compare(left.ID, right.ID))
}

func compareNames(left, right string) int {
	return strings.Compare(strings.ToLower(strings.TrimSpace(left)), strings.ToLower(strings.TrimSpace(right)))
}

func sortedFolders(folders []*CollectionFolder) []*CollectionFolder {
	result := slices.DeleteFunc(slices.Clone(folders), func(folder *CollectionFolder) bool { return folder == nil })
	slices.SortStableFunc(result, folderOrder)
	return result
}

func sortedRequests(requests []*CollectionRequest) []*CollectionRequest {
	result := slices.DeleteFunc(slices.Clone(requests), func(request *CollectionRequest) bool { return request == nil })
	slices.SortStableFunc(result, requestOrder)
	return result
}

func (s *CollectionStore) setTree(tree *CollectionTree) {
	if tree == nil {
		tree = &CollectionTree{SchemaVersion: 2}
	}
	s.Tree = tree
	s.Version++
	s.folders, s.requests, s.parents = map[string]*CollectionFolder{}, map[string]*CollectionRequest{}, map[string]string{}
	var visit func(folder *CollectionFolder, parent string)
	visit = func(folder *CollectionFolder, parent string) {
		s.folders[folder.ID], s.parents[folder.ID] = folder, parent
		for _, child := range sortedFolders(folder.Folders) {
			visit(child, folder.ID)
		}
		for _, request := range sortedRequests(folder.Requests) {
			s.requests[request.ID], s.parents[request.ID] = request, folder.ID
		}
	}
	for _, folder := range sortedFolders(tree.Folders) {
		visit(folder, "")
	}
	for id := range s.Expanded {
		if s.folders[id] == nil {
			delete(s.Expanded, id)
		}
	}
	if !s.Exists(s.SelectedNodeID) {
		s.SelectedNodeID = ""
	}
	if s.folders[s.LastSelectedFolderID] == nil {
		s.LastSelectedFolderID = ""
	}
}

// Exists reports whether a node is in the tree.
func (s *CollectionStore) Exists(id string) bool {
	return s.folders[id] != nil || s.requests[id] != nil
}

// IsFolder reports whether a node is a folder.
func (s *CollectionStore) IsFolder(id string) bool { return s.folders[id] != nil }

// Request returns a request node.
func (s *CollectionStore) Request(id string) *CollectionRequest { return s.requests[id] }

// ParentOf returns a node's folder, "" at the root.
func (s *CollectionStore) ParentOf(id string) string { return s.parents[id] }

// Empty reports whether nothing is saved.
func (s *CollectionStore) Empty() bool { return len(s.folders) == 0 && len(s.requests) == 0 }

// NodeName returns a node's name.
func (s *CollectionStore) NodeName(id string) string {
	if folder := s.folders[id]; folder != nil {
		return folder.Name
	}
	if request := s.requests[id]; request != nil {
		return request.Name
	}
	return ""
}

// IsDescendant reports whether folder lies inside ancestor.
func (s *CollectionStore) IsDescendant(folder, ancestor string) bool {
	seen := map[string]bool{}
	for parent := s.parents[folder]; parent != "" && !seen[parent]; parent = s.parents[parent] {
		if parent == ancestor {
			return true
		}
		seen[parent] = true
	}
	return false
}

// CanMove reports whether a node may move into a folder.
func (s *CollectionStore) CanMove(id, folder string) bool {
	if s.folders[folder] == nil || !s.Exists(id) || id == folder || s.parents[id] == folder {
		return false
	}
	return s.folders[id] == nil || !s.IsDescendant(folder, id)
}

// CanMoveToRoot reports whether a node may move to the top level.
func (s *CollectionStore) CanMoveToRoot(id string) bool {
	return s.folders[id] != nil && s.parents[id] != ""
}

// Rows returns the rows the expanded folders reveal.
func (s *CollectionStore) Rows() []CollectionRow {
	return s.rows(false)
}

// AllRows returns every node in tree order, including descendants of collapsed folders.
func (s *CollectionStore) AllRows() []CollectionRow {
	return s.rows(true)
}

func (s *CollectionStore) rows(includeCollapsed bool) []CollectionRow {
	var rows []CollectionRow
	var visit func(folder *CollectionFolder, parent string, depth int)
	visit = func(folder *CollectionFolder, parent string, depth int) {
		folders, requests := sortedFolders(folder.Folders), sortedRequests(folder.Requests)
		expanded := s.Expanded[folder.ID]
		rows = append(rows, CollectionRow{ID: folder.ID, ParentID: parent, Name: folder.Name, Folder: true, Depth: depth, Expanded: expanded, HasChildren: len(folders)+len(requests) > 0, Children: len(folders) + len(requests)})
		if !expanded && !includeCollapsed {
			return
		}
		for _, child := range folders {
			visit(child, folder.ID, depth+1)
		}
		for _, request := range requests {
			rows = append(rows, CollectionRow{ID: request.ID, ParentID: folder.ID, Name: request.Name, Depth: depth + 1, Request: request})
		}
	}
	for _, folder := range sortedFolders(s.Tree.Folders) {
		visit(folder, "", 0)
	}
	return rows
}

// FolderOptions lists every folder with its path.
func (s *CollectionStore) FolderOptions() []CollectionFolderOption {
	var options []CollectionFolderOption
	var visit func(folder *CollectionFolder, prefix string)
	visit = func(folder *CollectionFolder, prefix string) {
		path := prefix + "/" + folder.Name
		options = append(options, CollectionFolderOption{ID: folder.ID, Path: path})
		for _, child := range sortedFolders(folder.Folders) {
			visit(child, path)
		}
	}
	for _, folder := range sortedFolders(s.Tree.Folders) {
		visit(folder, "")
	}
	return options
}

func (s *CollectionStore) expandableIDs() []string {
	var ids []string
	for id, folder := range s.folders {
		if len(folder.Folders)+len(folder.Requests) > 0 {
			ids = append(ids, id)
		}
	}
	return ids
}

// HasExpandableFolders reports whether a folder has children.
func (s *CollectionStore) HasExpandableFolders() bool { return len(s.expandableIDs()) > 0 }

// AllExpanded reports whether every folder with children is open.
func (s *CollectionStore) AllExpanded() bool {
	ids := s.expandableIDs()
	return len(ids) > 0 && !slices.ContainsFunc(ids, func(id string) bool { return !s.Expanded[id] })
}

// ToggleAllExpanded opens every folder, or closes them all.
func (s *CollectionStore) ToggleAllExpanded() {
	all := s.AllExpanded()
	s.Expanded = map[string]bool{}
	if !all {
		for _, id := range s.expandableIDs() {
			s.Expanded[id] = true
		}
	}
}

// ToggleExpanded opens or closes a folder.
func (s *CollectionStore) ToggleExpanded(id string) {
	if s.folders[id] == nil {
		return
	}
	if s.Expanded[id] {
		delete(s.Expanded, id)
		return
	}
	s.Expanded[id] = true
}

// SelectNode selects a node and reveals it.
func (s *CollectionStore) SelectNode(id string) {
	s.SelectedNodeID = id
	if !s.Exists(id) {
		return
	}
	seen := map[string]bool{}
	for parent := s.parents[id]; parent != "" && !seen[parent]; parent = s.parents[parent] {
		s.Expanded[parent], seen[parent] = true, true
	}
	if s.folders[id] != nil {
		s.LastSelectedFolderID = id
	} else if parent := s.parents[id]; s.folders[parent] != nil {
		s.LastSelectedFolderID = parent
	}
}

// Load reads the tree. done, when set, hears the outcome.
func (s *CollectionStore) Load(done func(error)) {
	s.Loading, s.Error = true, ""
	runtime.Run(s.rt.Jobs, jobCollectionLoad, func(context.Context) (*CollectionTree, error) {
		return s.backend.ListCollection()
	}, func(tree *CollectionTree, err error) {
		s.Loading = false
		if err != nil {
			s.Error = err.Error()
		} else {
			s.setTree(tree)
			s.Loaded = true
		}
		if done != nil {
			done(err)
		}
	})
}

// EnsureLoaded loads the tree unless that already happened.
func (s *CollectionStore) EnsureLoaded(done func(error)) {
	if s.Loaded {
		done(nil)
		return
	}
	s.Load(done)
}

type mutationResult[T any] struct {
	value T
	tree  *CollectionTree
}

func mutateCollection[T any](s *CollectionStore, work func() (T, error), selected func(T) string, done func(T, error)) {
	s.Mutating = true
	runtime.Run(s.rt.Jobs, jobCollectionMutate, func(context.Context) (mutationResult[T], error) {
		value, err := work()
		if err != nil {
			return mutationResult[T]{value: value}, err
		}
		tree, err := s.backend.ListCollection()
		return mutationResult[T]{value, tree}, err
	}, func(result mutationResult[T], err error) {
		s.Mutating = false
		if err != nil {
			done(result.value, err)
			return
		}
		s.setTree(result.tree)
		s.Loaded = true
		if selected != nil {
			s.SelectNode(selected(result.value))
		}
		done(result.value, nil)
	})
}

// GetRequest reads a saved request in full.
func (s *CollectionStore) GetRequest(id string, done func(*CollectionRequest, error)) {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" || s.requests[trimmed] == nil {
		done(nil, errors.New("request node not found"))
		return
	}
	runtime.Run(s.rt.Jobs, jobCollectionDetail+trimmed, func(context.Context) (*CollectionRequest, error) {
		request, err := s.backend.GetRequest(trimmed)
		if err == nil && (request == nil || request.ID != trimmed) {
			err = errors.New("request details not found")
		}
		return request, err
	}, done)
}

// CreateFolder adds a folder.
func (s *CollectionStore) CreateFolder(parentID, name string, done func(*CollectionFolder, error)) {
	mutateCollection(s, func() (*CollectionFolder, error) {
		folder, err := s.backend.CreateFolder(parentID, name)
		if err == nil && folder == nil {
			err = errors.New("create folder returned no folder")
		}
		return folder, err
	}, func(folder *CollectionFolder) string { return folder.ID }, done)
}

// Rename names a node.
func (s *CollectionStore) Rename(id, name string, done func(error)) {
	mutateCollection(s, func() (string, error) {
		entry, err := s.backend.RenameNode(id, name)
		if err == nil && (entry == nil || (entry.Folder == nil && entry.Request == nil)) {
			err = errors.New("rename node returned no entry")
		}
		return id, err
	}, func(id string) string { return id }, func(_ string, err error) { done(err) })
}

// Move places a node into a folder, or the root for "".
func (s *CollectionStore) Move(id, parentID string, done func(error)) {
	node := strings.TrimSpace(id)
	mutateCollection(s, func() (string, error) {
		if node == "" {
			return "", errors.New("API collection node id is required")
		}
		return node, s.backend.MoveNode(node, strings.TrimSpace(parentID))
	}, func(id string) string { return id }, func(_ string, err error) { done(err) })
}

// collectRequestIDs lists the requests at or below a node.
func (s *CollectionStore) collectRequestIDs(id string) []string {
	if s.requests[id] != nil {
		return []string{id}
	}
	folder := s.folders[id]
	if folder == nil {
		return nil
	}
	var ids []string
	for _, request := range folder.Requests {
		if request != nil {
			ids = append(ids, request.ID)
		}
	}
	for _, child := range folder.Folders {
		if child != nil {
			ids = append(ids, s.collectRequestIDs(child.ID)...)
		}
	}
	return ids
}

// DeleteNodes removes nodes and closes the tabs of removed requests.
func (s *CollectionStore) DeleteNodes(ids []string, done func(error)) {
	normalized := make([]string, 0, len(ids))
	for _, id := range ids {
		if trimmed := strings.TrimSpace(id); trimmed != "" && !slices.Contains(normalized, trimmed) {
			normalized = append(normalized, trimmed)
		}
	}
	if len(normalized) == 0 {
		done(errors.New("at least one API collection node is required"))
		return
	}
	var removed []string
	for _, id := range normalized {
		removed = append(removed, s.collectRequestIDs(id)...)
	}
	mutateCollection(s, func() (struct{}, error) {
		return struct{}{}, s.backend.DeleteNodes(normalized)
	}, nil, func(_ struct{}, err error) {
		if err == nil && s.workspace != nil {
			s.workspace.CloseAPITabs(removed)
		}
		done(err)
	})
}

// Duplicate copies a request beside itself.
func (s *CollectionStore) Duplicate(id string, done func(error)) {
	summary := s.requests[id]
	if summary == nil {
		done(errors.New("request node not found"))
		return
	}
	parent := s.parents[id]
	if parent == "" {
		done(errors.New("request parent folder not found"))
		return
	}
	mutateCollection(s, func() (string, error) {
		node, err := s.backend.GetRequest(id)
		if err != nil {
			return "", err
		}
		var copied *CollectionRequest
		switch {
		case node != nil && node.Type == apicollectionservice.APICollectionNodeTypeHTTP && node.HTTP != nil:
			copied, err = s.backend.SaveHTTPRequest(parent, node.Name, node.HTTP)
		case node != nil && node.Type == apicollectionservice.APICollectionNodeTypeWebSocket && node.WebSocket != nil:
			copied, err = s.backend.SaveWebSocketRequest(parent, node.Name, node.WebSocket)
		default:
			return "", errors.New("unsupported request type")
		}
		if err != nil {
			return "", err
		}
		if copied == nil {
			return "", errors.New("copy request returned no request")
		}
		return copied.ID, nil
	}, func(id string) string { return id }, func(_ string, err error) { done(err) })
}
