package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/runtime"
	"github.com/josexy/flowlens/internal/pkg/logger"
	apicollectionservice "github.com/josexy/flowlens/internal/services/api_collection_service"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	"github.com/josexy/flowlens/locales"
)

const (
	streamFlushInterval    = 100 * time.Millisecond
	webSocketSessionGone   = "websocket session not found"
	jobWebSocketConnectPfx = "workspace:ws-connect:"
)

// RequestBackend sends requests for the editor tabs.
type RequestBackend interface {
	SendHTTPRequest(ctx context.Context, config proxyservice.SendRequestConfig, method, targetURL string, headers []logic.HeaderField, body proxyservice.SendRequestBody, sink proxyservice.HTTPStreamSink) (proxyservice.SendRequestResponse, error)
	DisconnectHTTPRequestStream(sessionID string) error
	ConnectWebSocket(ctx context.Context, request proxyservice.WebSocketConnectRequest, sink proxyservice.WebSocketSink) (proxyservice.WebSocketConnectResponse, error)
	SendWebSocketMessage(request proxyservice.WebSocketSendRequest) error
	DisconnectWebSocket(sessionID string) error
	RecoverRequestBodyForEditing(requestURL string, headers []logic.HeaderField, body proxyservice.TrafficBodyView) (proxyservice.RequestBodyRecoveryResult, error)
}

// SavedRequestBackend stores editor tabs in the API collection.
type SavedRequestBackend interface {
	SaveHTTPRequest(parentID, name string, request *apicollectionservice.SavedHTTPRequest) (*apicollectionservice.APICollectionRequest, error)
	SaveWebSocketRequest(parentID, name string, request *apicollectionservice.SavedWebSocketRequest) (*apicollectionservice.APICollectionRequest, error)
	UpdateHTTPRequest(id string, request *apicollectionservice.SavedHTTPRequest) (*apicollectionservice.APICollectionRequest, error)
	UpdateWebSocketRequest(id string, request *apicollectionservice.SavedWebSocketRequest) (*apicollectionservice.APICollectionRequest, error)
}

// httpStreamRuntime is one send of an HTTP Request Editor tab and the
// streaming response it may start. It exists before the backend call, so the
// send's sink owns every event of its stream, including those that arrive
// before the call returns.
type httpStreamRuntime struct {
	key       string
	sessionID string
	state     *logic.HTTPEditorState
	// active is false once Stop, a new send, closing the tab, or cleanup ended
	// the operation. Its later events are discarded.
	active bool
	// streaming is true once the send returned a streaming response; events
	// wait in pending until then.
	streaming  bool
	byteOffset int64
	decoder    *utf8StreamDecoder
	// Append only on the main thread. String snapshots remain immutable as
	// the buffer grows; do not copy a runtime after writing to its builder.
	body      strings.Builder
	pending   []proxyservice.HTTPRequestStreamEvent
	stopFlush func()
}

// webSocketRuntime is one connect of a WebSocket Client tab and the session it
// may open. Events wait in pending until the connect call returns.
type webSocketRuntime struct {
	key       string
	sessionID string
	state     *logic.WebSocketClientState
	// active is false once the session ended, was replaced, or its tab closed.
	active bool
	// attached is true once the connect call returned a session.
	attached bool
	pending  []proxyservice.WebSocketSessionEvent
	// connecting cancels the connect call while it runs.
	connecting context.CancelFunc
}

// WorkspaceStore holds the capture, history, and request editor tabs.
type WorkspaceStore struct {
	rt      Runtime
	backend RequestBackend
	saved   SavedRequestBackend
	clock   Clock

	Tabs      []*logic.WorkspaceTab
	ActiveKey string
	seed      int

	// streams and sockets hold each tab's current request operation.
	streams map[string]*httpStreamRuntime
	sockets map[string]*webSocketRuntime
	sends   map[string]*sendOperation
	// logs gathers plugin console output for the main thread.
	logs          *runtime.Coalescer[logic.PluginLogEntry]
	messageSeed   uint64
	recoveryEpoch uint64
}

// NewWorkspaceStore returns a store holding only the capture tab.
func NewWorkspaceStore(rt Runtime, backend RequestBackend, saved SavedRequestBackend, clock Clock) *WorkspaceStore {
	if clock == nil {
		clock = dispatcherClock{dispatcher: rt.Dispatcher}
	}
	s := &WorkspaceStore{
		rt: rt, backend: backend, saved: saved, clock: clock, ActiveKey: logic.CaptureTabKey,
		Tabs:    []*logic.WorkspaceTab{{Key: logic.CaptureTabKey, Type: logic.TabTypeCapture, Title: logic.CaptureTabKey}},
		streams: map[string]*httpStreamRuntime{}, sockets: map[string]*webSocketRuntime{}, sends: map[string]*sendOperation{},
	}
	return s
}

// ActiveTab returns the tab on show.
func (s *WorkspaceStore) ActiveTab() *logic.WorkspaceTab {
	if tab := s.FindTab(s.ActiveKey); tab != nil {
		return tab
	}
	return s.Tabs[0]
}

// FindTab returns the tab of a key.
func (s *WorkspaceStore) FindTab(key string) *logic.WorkspaceTab {
	index := slices.IndexFunc(s.Tabs, func(tab *logic.WorkspaceTab) bool { return tab.Key == key })
	if index < 0 {
		return nil
	}
	return s.Tabs[index]
}

// Activate shows a tab.
func (s *WorkspaceStore) Activate(key string) {
	if s.FindTab(key) != nil {
		s.ActiveKey = key
	}
}

func (s *WorkspaceStore) activeIndex() int {
	return slices.IndexFunc(s.Tabs, func(tab *logic.WorkspaceTab) bool { return tab.Key == s.ActiveKey })
}

// ActivateNext shows the following tab, wrapping around.
func (s *WorkspaceStore) ActivateNext() {
	if index := s.activeIndex(); index >= 0 {
		s.ActiveKey = s.Tabs[(index+1)%len(s.Tabs)].Key
	}
}

// ActivatePrevious shows the preceding tab, wrapping around.
func (s *WorkspaceStore) ActivatePrevious() {
	if index := s.activeIndex(); index >= 0 {
		s.ActiveKey = s.Tabs[(index-1+len(s.Tabs))%len(s.Tabs)].Key
	}
}

// ActivateCapture shows the capture tab.
func (s *WorkspaceStore) ActivateCapture() { s.ActiveKey = logic.CaptureTabKey }

// OpenHistoryTab shows a saved session in the single history tab.
func (s *WorkspaceStore) OpenHistoryTab(key, title string) {
	tab := s.FindTab(logic.HistoryTabKey)
	if tab == nil {
		tab = &logic.WorkspaceTab{Key: logic.HistoryTabKey, Type: logic.TabTypeHistory, Closable: true}
		s.Tabs = append(s.Tabs, tab)
	}
	tab.Title, tab.HistoryKey = title, key
	s.ActiveKey = logic.HistoryTabKey
}

func (s *WorkspaceStore) nextKey(prefix string) string {
	s.seed++
	return fmt.Sprintf("%s%d", prefix, s.seed)
}

// CreateHTTPRequestTab opens a request tab. A nil state is a new request.
func (s *WorkspaceStore) CreateHTTPRequestTab(source logic.RequestDraftSource, state *logic.HTTPEditorState) string {
	if state == nil {
		state = logic.BuildEmptyHTTPEditorState(source)
	}
	key := s.nextKey(logic.HTTPRequestTabPrefix)
	s.Tabs = append(s.Tabs, &logic.WorkspaceTab{Key: key, Type: logic.TabTypeHTTPRequest, Title: state.Name, Closable: true, HTTP: state})
	s.ActiveKey = key
	return key
}

// CreateWebSocketClientTab opens a WebSocket tab. A nil state is a new client.
func (s *WorkspaceStore) CreateWebSocketClientTab(source logic.RequestDraftSource, state *logic.WebSocketClientState) string {
	if state == nil {
		state = logic.BuildEmptyWebSocketClientState(source)
	}
	key := s.nextKey(logic.WebSocketClientTabPrefix)
	s.Tabs = append(s.Tabs, &logic.WorkspaceTab{Key: key, Type: logic.TabTypeWebSocketClient, Title: state.Name, Closable: true, WebSocket: state})
	s.ActiveKey = key
	return key
}

func isRequestTab(tab *logic.WorkspaceTab) bool {
	return tab.Type == logic.TabTypeHTTPRequest || tab.Type == logic.TabTypeWebSocketClient
}

// FindTabByAPIID returns the tab linked to a saved request.
func (s *WorkspaceStore) FindTabByAPIID(id string) *logic.WorkspaceTab {
	index := slices.IndexFunc(s.Tabs, func(tab *logic.WorkspaceTab) bool { return isRequestTab(tab) && tab.APIID == id })
	if index < 0 {
		return nil
	}
	return s.Tabs[index]
}

// OpenSavedAPI shows a saved request, in its existing tab when it has one.
func (s *WorkspaceStore) OpenSavedAPI(request *apicollectionservice.APICollectionRequest) (string, error) {
	if existing := s.FindTabByAPIID(request.ID); existing != nil {
		s.ActiveKey = existing.Key
		return existing.Key, nil
	}
	var key string
	switch {
	case request.Type == apicollectionservice.APICollectionNodeTypeHTTP && request.HTTP != nil:
		key = s.CreateHTTPRequestTab(logic.DraftNew, logic.BuildHTTPEditorStateFromSaved(request.HTTP, request.Name))
	case request.Type == apicollectionservice.APICollectionNodeTypeWebSocket && request.WebSocket != nil:
		key = s.CreateWebSocketClientTab(logic.DraftNew, logic.BuildWebSocketClientStateFromSaved(request.WebSocket, request.Name))
	default:
		return "", fmt.Errorf("unsupported API request type: %s", request.Type)
	}
	tab := s.FindTab(key)
	tab.Title = request.Name
	logic.ApplySavedMetadata(tab, request)
	return key, nil
}

// OpenFromTraffic opens a captured request for editing. done receives the new
// tab's key, or "" when the workspace moved on meanwhile.
func (s *WorkspaceStore) OpenFromTraffic(entry *logic.TrafficEntry, body *proxyservice.TrafficBodyView, source logic.RequestDraftSource, historyKey string, done func(string, error)) {
	if body == nil || body.RequestBodyUnavailable {
		done("", logic.ErrRequestBodyUnavailable)
		return
	}
	if !logic.CapabilitiesOf(entry).CanEditRequest {
		done("", fmt.Errorf("traffic type %s cannot be opened in Request Editor", entry.Type))
		return
	}
	if logic.IsWebSocket(entry) {
		done(s.CreateWebSocketClientTab(source, logic.WebSocketClientStateFromTraffic(source, entry, historyKey)), nil)
		return
	}
	state, err := logic.HTTPEditorStateFromTraffic(source, entry, body, historyKey)
	if err != nil {
		done("", err)
		return
	}
	headers := []logic.HeaderField(nil)
	if entry.Request != nil {
		headers = entry.Request.HeaderFields
	}
	epoch := s.recoveryEpoch
	view := *body
	url := entry.URL
	s.seed++
	runtime.Run(s.rt.Jobs, fmt.Sprintf("workspace:recover:%d", s.seed), func(context.Context) (proxyservice.RequestBodyRecoveryResult, error) {
		return s.backend.RecoverRequestBodyForEditing(url, headers, view)
	}, func(recovery proxyservice.RequestBodyRecoveryResult, err error) {
		if epoch != s.recoveryEpoch {
			done("", nil)
			return
		}
		if err != nil {
			logger.G().Errorf("Failed to recover request body for editing: %v", err)
		} else {
			logic.ApplyRequestBodyRecovery(state, &recovery)
		}
		done(s.CreateHTTPRequestTab(source, state), nil)
	})
}

// ClearRequestDraftCacheFileReferences forgets files in a cleared draft cache.
func (s *WorkspaceStore) ClearRequestDraftCacheFileReferences(root string) int {
	s.recoveryEpoch++
	return logic.ClearRequestDraftCacheFileReferences(s.Tabs, root)
}

// IsDirty reports whether a tab holds unsaved changes.
func (s *WorkspaceStore) IsDirty(key string) bool {
	tab := s.FindTab(key)
	if tab == nil || !isRequestTab(tab) {
		return false
	}
	snapshot := logic.RequestSnapshotOf(tab)
	if tab.APIID != "" {
		return !snapshot.Equal(tab.SavedSnapshot)
	}
	return !snapshot.IsZero() && logic.HasMeaningfulRequestDraft(tab)
}

// HasUnsavedNewRequestTabs reports whether a never-saved draft exists.
func (s *WorkspaceStore) HasUnsavedNewRequestTabs() bool {
	return slices.ContainsFunc(s.Tabs, func(tab *logic.WorkspaceTab) bool {
		return isRequestTab(tab) && tab.APIID == "" && logic.HasMeaningfulRequestDraft(tab)
	})
}

// HasDirtyTabs reports whether any of keys is dirty.
func (s *WorkspaceStore) HasDirtyTabs(keys []string) bool {
	return slices.ContainsFunc(keys, func(key string) bool {
		tab := s.FindTab(key)
		return tab != nil && tab.Closable && s.IsDirty(key)
	})
}

// HasDirtyClosableTabs reports whether a closable tab is dirty.
func (s *WorkspaceStore) HasDirtyClosableTabs() bool {
	return slices.ContainsFunc(s.Tabs, func(tab *logic.WorkspaceTab) bool { return tab.Closable && s.IsDirty(tab.Key) })
}

// ClosableKeys returns the keys of closable tabs matching keep.
func (s *WorkspaceStore) ClosableKeys(keep func(index int, tab *logic.WorkspaceTab) bool) []string {
	var keys []string
	for index, tab := range s.Tabs {
		if tab.Closable && keep(index, tab) {
			keys = append(keys, tab.Key)
		}
	}
	return keys
}

// CloseTab closes a tab, ending any stream or connection it owns.
func (s *WorkspaceStore) CloseTab(key string) (closedHistoryKey string) {
	if key == logic.CaptureTabKey {
		return ""
	}
	index := slices.IndexFunc(s.Tabs, func(tab *logic.WorkspaceTab) bool { return tab.Key == key })
	if index < 0 {
		return ""
	}
	tab := s.Tabs[index]
	if tab.HTTP != nil {
		if operation := s.streams[key]; operation != nil {
			s.endHTTPOperation(operation)
		}
		if session := strings.TrimSpace(tab.HTTP.StreamSessionID); session != "" {
			tab.HTTP.IsStreaming, tab.HTTP.StreamSessionID = false, ""
			s.disconnectStream(session)
		}
		s.CancelSend(key)
	}
	if operation := s.sockets[key]; operation != nil {
		if operation.connecting != nil {
			operation.connecting()
		}
		s.endWebSocketOperation(operation)
	}
	if tab.WebSocket != nil && tab.WebSocket.SessionID != "" {
		s.disconnectWebSocket(tab.WebSocket.SessionID)
	}
	if tab.Type == logic.TabTypeHistory {
		closedHistoryKey = tab.HistoryKey
	}
	wasActive := s.ActiveKey == key
	s.Tabs = slices.Delete(slices.Clone(s.Tabs), index, index+1)
	if wasActive {
		switch {
		case index < len(s.Tabs):
			s.ActiveKey = s.Tabs[index].Key
		case index > 0:
			s.ActiveKey = s.Tabs[index-1].Key
		default:
			s.ActiveKey = logic.CaptureTabKey
		}
	}
	return closedHistoryKey
}

// CloseTabs closes tabs; without force, dirty tabs stay open.
func (s *WorkspaceStore) CloseTabs(keys []string, force bool) {
	for _, key := range keys {
		tab := s.FindTab(key)
		if tab == nil || !tab.Closable || (!force && s.IsDirty(key)) {
			continue
		}
		s.CloseTab(key)
	}
}

// CloseAPITabs closes the tabs of deleted saved requests.
func (s *WorkspaceStore) CloseAPITabs(ids []string) {
	wanted := map[string]bool{}
	for _, id := range ids {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			wanted[trimmed] = true
		}
	}
	for _, tab := range slices.Clone(s.Tabs) {
		if tab.Closable && tab.APIID != "" && wanted[tab.APIID] {
			s.CloseTab(tab.Key)
		}
	}
}

// PruneHistoryTab closes the history tab if its session no longer exists.
func (s *WorkspaceStore) PruneHistoryTab(exists func(key string) bool) {
	if tab := s.FindTab(logic.HistoryTabKey); tab != nil && tab.HistoryKey != "" && !exists(tab.HistoryKey) {
		s.CloseTab(logic.HistoryTabKey)
	}
}

// Reorder arranges the movable tabs; capture stays first.
func (s *WorkspaceStore) Reorder(keys []string) {
	byKey := map[string]*logic.WorkspaceTab{}
	for _, tab := range s.Tabs {
		if tab.Key != logic.CaptureTabKey {
			byKey[tab.Key] = tab
		}
	}
	ordered := []*logic.WorkspaceTab{s.FindTab(logic.CaptureTabKey)}
	seen := map[string]bool{}
	for _, key := range keys {
		if tab, ok := byKey[key]; ok && !seen[key] {
			ordered = append(ordered, tab)
			seen[key] = true
		}
	}
	for _, tab := range s.Tabs {
		if tab.Key != logic.CaptureTabKey && !seen[tab.Key] {
			ordered = append(ordered, tab)
		}
	}
	s.Tabs = ordered
}

// UpdateRequestTabTitle names an unsaved tab after its URL.
func (s *WorkspaceStore) UpdateRequestTabTitle(key, rawURL string) {
	tab := s.FindTab(key)
	if tab == nil || !isRequestTab(tab) || tab.APIID != "" {
		return
	}
	title := logic.DeriveRequestTabTitle(rawURL)
	tab.Title = title
	if tab.HTTP != nil {
		tab.HTTP.Name = title
	}
	if tab.WebSocket != nil {
		tab.WebSocket.Name = title
	}
}

// SaveTabAsAPI stores a tab in the collection.
func (s *WorkspaceStore) SaveTabAsAPI(key, parentID, name string, done func(*apicollectionservice.APICollectionRequest, error)) {
	s.persistTab(key, done, func(tab *logic.WorkspaceTab) (*apicollectionservice.APICollectionRequest, error) {
		if tab.HTTP != nil {
			return s.saved.SaveHTTPRequest(parentID, name, logic.BuildSavedHTTPRequest(tab.HTTP))
		}
		return s.saved.SaveWebSocketRequest(parentID, name, logic.BuildSavedWebSocketRequest(tab.WebSocket))
	})
}

// SaveExistingTab updates a tab's saved request.
func (s *WorkspaceStore) SaveExistingTab(key string, done func(*apicollectionservice.APICollectionRequest, error)) {
	tab := s.FindTab(key)
	if tab == nil || tab.APIID == "" {
		done(nil, fmt.Errorf("tab %s is not linked to a saved API", key))
		return
	}
	id := tab.APIID
	s.persistTab(key, done, func(tab *logic.WorkspaceTab) (*apicollectionservice.APICollectionRequest, error) {
		if tab.HTTP != nil {
			return s.saved.UpdateHTTPRequest(id, logic.BuildSavedHTTPRequest(tab.HTTP))
		}
		return s.saved.UpdateWebSocketRequest(id, logic.BuildSavedWebSocketRequest(tab.WebSocket))
	})
}

func (s *WorkspaceStore) persistTab(key string, done func(*apicollectionservice.APICollectionRequest, error), save func(*logic.WorkspaceTab) (*apicollectionservice.APICollectionRequest, error)) {
	tab := s.FindTab(key)
	if tab == nil || !isRequestTab(tab) {
		done(nil, fmt.Errorf("tab %s is not a request editor tab", key))
		return
	}
	runtime.Run(s.rt.Jobs, "workspace:save:"+key, func(context.Context) (*apicollectionservice.APICollectionRequest, error) {
		request, err := save(tab)
		if err == nil && request == nil {
			err = errors.New("save returned no request")
		}
		return request, err
	}, func(request *apicollectionservice.APICollectionRequest, err error) {
		if err != nil {
			done(nil, err)
			return
		}
		if tab.HTTP != nil && request.HTTP != nil {
			logic.ApplySavedHTTPRequest(tab.HTTP, request.HTTP, request.Name)
		}
		if tab.WebSocket != nil && request.WebSocket != nil {
			logic.ApplySavedWebSocketRequest(tab.WebSocket, request.WebSocket, request.Name)
		}
		tab.Title = request.Name
		logic.ApplySavedMetadata(tab, request)
		done(request, nil)
	})
}

func (s *WorkspaceStore) disconnectStream(session string) {
	runtime.Run(s.rt.Jobs, "workspace:disconnect-http:"+session, func(context.Context) (struct{}, error) {
		return struct{}{}, s.backend.DisconnectHTTPRequestStream(session)
	}, func(_ struct{}, err error) {
		if err != nil {
			logger.G().Errorf("Failed to disconnect HTTP request stream: %v", err)
		}
	})
}

func (s *WorkspaceStore) disconnectWebSocket(session string) {
	runtime.Run(s.rt.Jobs, "workspace:disconnect-ws:"+session, func(context.Context) (struct{}, error) {
		return struct{}{}, s.backend.DisconnectWebSocket(session)
	}, func(_ struct{}, err error) {
		if err != nil {
			logger.G().Errorf("Failed to disconnect WebSocket session: %v", err)
		}
	})
}

// beginHTTPOperation ends the tab's previous operation and starts the one
// whose sink receives the next send's events.
func (s *WorkspaceStore) beginHTTPOperation(key string, state *logic.HTTPEditorState) *httpStreamRuntime {
	if previous := s.streams[key]; previous != nil {
		s.endHTTPOperation(previous)
	}
	operation := &httpStreamRuntime{key: key, state: state, active: true, decoder: &utf8StreamDecoder{}}
	s.streams[key] = operation
	return operation
}

// endHTTPOperation discards the operation's pending and later events.
func (s *WorkspaceStore) endHTTPOperation(operation *httpStreamRuntime) {
	operation.active = false
	operation.pending = nil
	if operation.stopFlush != nil {
		operation.stopFlush()
		operation.stopFlush = nil
	}
	if s.streams[operation.key] == operation {
		delete(s.streams, operation.key)
	}
}

// receiveHTTP is the sink of one send. It runs on the main thread.
func (s *WorkspaceStore) receiveHTTP(operation *httpStreamRuntime, event proxyservice.HTTPRequestStreamEvent) {
	if !operation.active {
		return
	}
	operation.pending = append(operation.pending, event)
	if operation.streaming {
		s.scheduleStreamFlush(operation)
	}
}

func (s *WorkspaceStore) scheduleStreamFlush(stream *httpStreamRuntime) {
	if stream.stopFlush != nil {
		return
	}
	stream.stopFlush = s.clock.After(streamFlushInterval, func() {
		stream.stopFlush = nil
		s.flushStream(stream)
	})
}

// attachHTTPStream follows the streaming response the send returned, whose
// session ID Stop and closing the tab use. Events that arrived before the
// send returned are applied on the next flush.
func (s *WorkspaceStore) attachHTTPStream(operation *httpStreamRuntime, sessionID string) bool {
	session := strings.TrimSpace(sessionID)
	if session == "" {
		s.endHTTPOperation(operation)
		return false
	}
	state := operation.state
	if !operation.active || state.Response == nil {
		// The tab closed or a new operation replaced this one while sending.
		s.endHTTPOperation(operation)
		s.disconnectStream(session)
		return false
	}
	operation.sessionID, operation.streaming = session, true
	operation.byteOffset = int64(len(state.Response.Body))
	operation.body.WriteString(state.Response.Body)
	state.StreamSessionID, state.IsStreaming = session, true
	state.Response.StreamState = logic.StreamStateStreaming
	if len(operation.pending) > 0 {
		s.scheduleStreamFlush(operation)
	}
	return true
}

func (s *WorkspaceStore) finishStream(stream *httpStreamRuntime, event proxyservice.HTTPRequestStreamEvent) {
	response := stream.state.Response
	if response != nil {
		if event.EventType == proxyservice.HTTPStreamComplete {
			response.Trailers = logic.HeaderFieldsToEditableRows(event.TrailerFields)
			response.TrailersHaveWireOrder = logic.HasWireOrder(event.TrailerFields, event.TrailerOrderUnavailable)
			response.TrailersTruncated = event.TrailersTruncated
		}
		if event.EventType == proxyservice.HTTPStreamError && event.Error != "" {
			response.ErrorMessage = event.Error
		}
		response.ElapsedMicros = time.Since(stream.state.SentAt).Microseconds()
		response.StreamState = logic.StreamStateCompleted
		if event.EventType == proxyservice.HTTPStreamError {
			response.StreamState = logic.StreamStateError
		}
	}
	stream.state.IsStreaming, stream.state.StreamSessionID = false, ""
	s.endHTTPOperation(stream)
}

func (s *WorkspaceStore) flushStream(stream *httpStreamRuntime) {
	if !stream.active || !stream.streaming {
		return
	}
	var chunks, terminal []proxyservice.HTTPRequestStreamEvent
	for _, event := range stream.pending {
		switch {
		case event.EventType == proxyservice.HTTPStreamChunk && event.Offset != nil:
			chunks = append(chunks, event)
		case event.EventType != proxyservice.HTTPStreamChunk:
			terminal = append(terminal, event)
		}
	}
	slices.SortStableFunc(chunks, func(a, b proxyservice.HTTPRequestStreamEvent) int {
		switch {
		case *a.Offset < *b.Offset:
			return -1
		case *a.Offset > *b.Offset:
			return 1
		default:
			return 0
		}
	})
	var deferred []proxyservice.HTTPRequestStreamEvent
	response := stream.state.Response
	for _, event := range chunks {
		chunk := event.Chunk
		start, end := *event.Offset, *event.Offset+int64(len(chunk))
		if end <= stream.byteOffset {
			continue
		}
		if start > stream.byteOffset {
			deferred = append(deferred, event)
			continue
		}
		fresh := chunk[max(0, stream.byteOffset-start):]
		switch {
		case response == nil:
		case response.BodyBinary:
			stream.body.Write(fresh)
		default:
			stream.body.WriteString(stream.decoder.decode(fresh))
		}
		stream.byteOffset = end
	}
	if response != nil {
		// Publish without copying the accumulated prefix on every flush.
		response.Body = stream.body.String()
	}
	stream.pending = deferred
	for _, event := range terminal {
		if event.Offset == nil || *event.Offset <= stream.byteOffset {
			s.finishStream(stream, event)
			return
		}
	}
	stream.pending = append(stream.pending, terminal...)
}

// DisconnectHTTPTab stops following a tab's stream. What arrived before Stop
// is applied; later events of the stream are discarded.
func (s *WorkspaceStore) DisconnectHTTPTab(key string) {
	tab := s.FindTab(key)
	if tab == nil || tab.HTTP == nil {
		return
	}
	state := tab.HTTP
	session := strings.TrimSpace(state.StreamSessionID)
	if operation := s.streams[key]; operation != nil {
		if operation.stopFlush != nil {
			operation.stopFlush()
			operation.stopFlush = nil
		}
		s.flushStream(operation)
		s.endHTTPOperation(operation)
	}
	state.IsStreaming, state.StreamSessionID = false, ""
	if state.Response != nil && state.Response.Kind == "success" {
		state.Response.StreamState = logic.StreamStateStopped
		state.Response.ElapsedMicros = time.Since(state.SentAt).Microseconds()
	}
	if session != "" {
		s.disconnectStream(session)
	}
}

type sendOperation struct {
	cancel    context.CancelFunc
	requested bool
}

// SendTab sends a tab's request, or returns why it cannot be sent.
func (s *WorkspaceStore) SendTab(key string, pythonEnabled bool) string {
	tab := s.FindTab(key)
	if tab == nil || tab.HTTP == nil || tab.HTTP.IsSending || tab.HTTP.IsStreaming {
		return ""
	}
	state := tab.HTTP
	if problem := logic.ValidateRequestURL(state.URL); problem != "" {
		return problem
	}
	if strings.TrimSpace(state.Method) == "" {
		return locales.WorkspaceHttpRequestInvalidMethod()
	}
	if state.InlineScriptEnabled && !pythonEnabled {
		state.ActiveRequestTab = logic.RequestTabScript
		return locales.WorkspaceHttpRequestInlineScriptRuntimeDisabled()
	}
	if state.InlineScriptEnabled && strings.TrimSpace(state.InlineScriptSource) == "" {
		state.ActiveRequestTab = logic.RequestTabScript
		return locales.WorkspaceHttpRequestInlineScriptEmpty()
	}
	if name, invalid := logic.FindInvalidRequestHeaderName(state.Headers, logic.RequestProtocol(state.Settings.Protocol)); invalid {
		state.ActiveRequestTab = logic.RequestTabHeaders
		return locales.WorkspaceHttpRequestHeaderInvalidName(name)
	}
	body, err := logic.BuildRequestBodyPayload(state)
	if err != nil {
		return err.Error()
	}
	executionID := ""
	if pythonEnabled && (state.PluginsEnabled || state.InlineScriptEnabled) {
		executionID = newExecutionID()
		state.ActiveResponseTab = logic.ResponseTabConsole
	}
	if s.logs != nil {
		s.logs.Discard()
	}
	state.ScriptExecutionID = executionID
	state.ScriptConsoleEntries = nil
	config := logic.BuildSendConfig(state, executionID)
	headers := logic.EditableRowsToHeaderFields(state.Headers)
	method, target := state.Method, state.URL
	state.IsSending = true
	state.SentAt = time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	operation := &sendOperation{cancel: cancel}
	s.sends[key] = operation
	stream := s.beginHTTPOperation(key, state)
	sink := func(event proxyservice.HTTPRequestStreamEvent) { s.receiveHTTP(stream, event) }
	runtime.Run(s.rt.Jobs, "workspace:send:"+key, func(context.Context) (proxyservice.SendRequestResponse, error) {
		return s.backend.SendHTTPRequest(ctx, config, method, target, headers, body, sink)
	}, func(response proxyservice.SendRequestResponse, err error) {
		cancel()
		if s.sends[key] == operation {
			delete(s.sends, key)
		}
		state.IsSending = false
		s.finishSend(stream, response, err, operation.requested)
	})
	return ""
}

func (s *WorkspaceStore) finishSend(stream *httpStreamRuntime, response proxyservice.SendRequestResponse, err error, cancelled bool) {
	state := stream.state
	elapsed := time.Since(state.SentAt).Microseconds()
	if err != nil {
		s.endHTTPOperation(stream)
		state.IsStreaming, state.StreamSessionID = false, ""
		if cancelled || errors.Is(err, context.Canceled) {
			state.Response = logic.CancelledResponseView()
			return
		}
		state.Response = logic.ErrorResponseView(err)
		state.Response.ElapsedMicros = elapsed
		state.ActiveResponseTab = logic.ResponseTabError
		return
	}
	view, streaming := logic.ResponseViewOf(response)
	view.ElapsedMicros = elapsed
	state.Response = view
	state.IsStreaming, state.StreamSessionID = false, ""
	if response.PluginExecution != nil && response.PluginExecution.ExecutionID != "" {
		state.ScriptExecutionID = response.PluginExecution.ExecutionID
	}
	attached := false
	if streaming {
		attached = s.attachHTTPStream(stream, response.StreamSessionID)
	} else {
		s.endHTTPOperation(stream)
	}
	state.ActiveResponseTab = logic.ResponseTabAfterSend(view, streaming, attached, state.InlineScriptEnabled)
}

// CancelSend abandons a request being sent.
func (s *WorkspaceStore) CancelSend(key string) {
	if operation := s.sends[key]; operation != nil {
		operation.requested = true
		operation.cancel()
	}
}

// StopTab stops a tab's request or stream.
func (s *WorkspaceStore) StopTab(key string) {
	tab := s.FindTab(key)
	if tab == nil || tab.HTTP == nil {
		return
	}
	s.CancelSend(key)
	if tab.HTTP.IsStreaming {
		s.DisconnectHTTPTab(key)
	}
}

// ClearScriptConsole empties a tab's console.
func (s *WorkspaceStore) ClearScriptConsole(key string) {
	if tab := s.FindTab(key); tab != nil && tab.HTTP != nil {
		if s.logs != nil {
			s.logs.Discard()
		}
		tab.HTTP.ScriptConsoleEntries = nil
	}
}

// ConnectPluginLogs follows Python plugin output. Lines reach the consoles in
// batches, one interface update per flush interval. off stops following and
// drops output that has not reached the main thread.
func (s *WorkspaceStore) ConnectPluginLogs(logs Topic[logic.PluginLogEntry]) (off func()) {
	coalescer := runtime.NewCoalescer(s.rt.Dispatcher, s.applyPluginLogs, boundPluginLogs)
	s.logs = coalescer
	unsubscribe := logs.Subscribe(coalescer.Add)
	return func() {
		unsubscribe()
		coalescer.Close()
	}
}

// boundPluginLogs keeps the newest MaxRequestConsoleEntries entries of each
// execution, which is all its console keeps, in order.
func boundPluginLogs(pending []logic.PluginLogEntry) []logic.PluginLogEntry {
	if len(pending) <= logic.MaxRequestConsoleEntries {
		return pending
	}
	excess := map[string]int{}
	for _, entry := range pending {
		excess[entry.ExecutionID]++
	}
	trimmed := false
	for execution, count := range excess {
		excess[execution] = count - logic.MaxRequestConsoleEntries
		trimmed = trimmed || count > logic.MaxRequestConsoleEntries
	}
	if !trimmed {
		return pending
	}
	kept := pending[:0]
	for _, entry := range pending {
		if excess[entry.ExecutionID] > 0 {
			excess[entry.ExecutionID]--
			continue
		}
		kept = append(kept, entry)
	}
	clear(pending[len(kept):])
	return kept
}

// applyPluginLogs adds a batch of output to the consoles of the tabs running
// its executions. Output of an execution no open tab runs is discarded.
func (s *WorkspaceStore) applyPluginLogs(batch []logic.PluginLogEntry) {
	for _, tab := range s.Tabs {
		if tab.HTTP == nil || tab.HTTP.ScriptExecutionID == "" {
			continue
		}
		if matching := logic.FilterPythonLogBatch(batch, tab.HTTP.ScriptExecutionID); len(matching) > 0 {
			tab.HTTP.ScriptConsoleEntries = logic.AppendPythonLogBatch(tab.HTTP.ScriptConsoleEntries, matching, logic.MaxRequestConsoleEntries)
		}
	}
}

func newExecutionID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("request-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw[:])
}

// endWebSocketOperation discards the operation's pending and later events.
func (s *WorkspaceStore) endWebSocketOperation(operation *webSocketRuntime) {
	operation.active = false
	operation.pending = nil
	if s.sockets[operation.key] == operation {
		delete(s.sockets, operation.key)
	}
}

// receiveWebSocket is the sink of one connect. It runs on the main thread.
func (s *WorkspaceStore) receiveWebSocket(operation *webSocketRuntime, event proxyservice.WebSocketSessionEvent) {
	if !operation.active {
		return
	}
	if !operation.attached {
		operation.pending = append(operation.pending, event)
		return
	}
	s.applyWebSocketEvent(operation, event)
}

func isTerminalWebSocket(state *logic.WebSocketClientState) bool {
	return state.ConnectionStatus == logic.WebSocketClosed || state.ConnectionStatus == logic.WebSocketError || state.ConnectionStatus == logic.WebSocketCancelled
}

func (s *WorkspaceStore) nextMessageID() string {
	s.messageSeed++
	return fmt.Sprintf("websocket-client-msg:%d", s.messageSeed)
}

func (s *WorkspaceStore) applyWebSocketEvent(operation *webSocketRuntime, event proxyservice.WebSocketSessionEvent) {
	state := operation.state
	switch {
	case event.EventType == proxyservice.WebSocketEventConnected:
		if !isTerminalWebSocket(state) {
			state.SessionID, state.ConnectionStatus, state.ResponseError = event.SessionID, logic.WebSocketConnected, ""
		}
	case event.EventType == proxyservice.WebSocketEventMessage && event.Message != nil:
		if !isTerminalWebSocket(state) {
			state.ConnectionStatus, state.ResponseError = logic.WebSocketConnected, ""
			state.Messages = append(state.Messages, logic.ToWebSocketDisplayMessage(event.Message, s.nextMessageID(), time.Now()))
		}
	case event.EventType == proxyservice.WebSocketEventClosed || event.EventType == proxyservice.WebSocketEventError:
		s.endWebSocketOperation(operation)
		failed := event.Status == "error" || event.EventType == proxyservice.WebSocketEventError
		state.ConnectionStatus, state.SessionID, state.ResponseError = logic.WebSocketClosed, "", ""
		if failed {
			state.ConnectionStatus, state.ResponseError = logic.WebSocketError, event.Error
		}
	}
}

// endWebSocketSession closes the tab's session if it is still session.
func (s *WorkspaceStore) endWebSocketSession(key, session string, state *logic.WebSocketClientState) {
	if operation := s.sockets[key]; operation != nil && operation.sessionID == session {
		s.endWebSocketOperation(operation)
	}
	state.SessionID, state.ConnectionStatus, state.ResponseError = "", logic.WebSocketClosed, ""
}

// ClearWebSocketMessages empties a tab's message list.
func (s *WorkspaceStore) ClearWebSocketMessages(key string) {
	if tab := s.FindTab(key); tab != nil && tab.WebSocket != nil {
		tab.WebSocket.Messages = nil
	}
}

// ConnectWebSocket opens a tab's connection. done hears a failure.
func (s *WorkspaceStore) ConnectWebSocket(key string, done func(error)) {
	tab := s.FindTab(key)
	if tab == nil || tab.WebSocket == nil || tab.WebSocket.ConnectionStatus == logic.WebSocketConnecting || tab.WebSocket.ConnectionStatus == logic.WebSocketConnected {
		return
	}
	state := tab.WebSocket
	state.ConnectionStatus, state.ResponseError = logic.WebSocketConnecting, ""
	timeout := max(state.Settings.TimeoutMs, 0)
	request := proxyservice.WebSocketConnectRequest{
		URL: state.URL, HeaderFields: logic.EditableRowsToHeaderFields(state.Headers), ProxyMode: proxyservice.SendRequestProxyMode(state.Settings.ProxyMode),
		CustomProxy: state.Settings.CustomProxy, TimeoutMs: timeout, SkipVerifyTLS: state.Settings.SkipVerifyTLS, TLSClientHelloID: proxyservice.TLSClientHelloID(state.Settings.TLSClientHelloID),
	}
	if previous := s.sockets[key]; previous != nil {
		s.endWebSocketOperation(previous)
	}
	ctx, cancel := context.WithCancel(context.Background())
	operation := &webSocketRuntime{key: key, state: state, active: true, connecting: cancel}
	s.sockets[key] = operation
	sink := func(event proxyservice.WebSocketSessionEvent) { s.receiveWebSocket(operation, event) }
	runtime.Run(s.rt.Jobs, jobWebSocketConnectPfx+key, func(context.Context) (proxyservice.WebSocketConnectResponse, error) {
		return s.backend.ConnectWebSocket(ctx, request, sink)
	}, func(response proxyservice.WebSocketConnectResponse, err error) {
		cancel()
		operation.connecting = nil
		session := strings.TrimSpace(response.SessionID)
		if !operation.active {
			// The tab closed or a new connection replaced this one.
			if err == nil && session != "" {
				s.disconnectWebSocket(session)
			}
			return
		}
		switch {
		case errors.Is(err, context.Canceled):
			s.endWebSocketOperation(operation)
			state.ConnectionStatus, state.SessionID, state.ResponseHeaders, state.ResponseError = logic.WebSocketCancelled, "", nil, ""
		case err != nil:
			s.endWebSocketOperation(operation)
			state.ConnectionStatus, state.SessionID, state.ResponseHeaders, state.ResponseError = logic.WebSocketError, "", nil, err.Error()
			if done != nil {
				done(err)
			}
		default:
			operation.sessionID, operation.attached = session, true
			state.SessionID, state.ConnectionStatus = session, logic.WebSocketIdle
			if response.Status == "connected" {
				state.ConnectionStatus = logic.WebSocketConnected
			}
			state.ResponseHeaders, state.ResponseError = logic.HeaderFieldsToEditableRows(response.HeaderFields), ""
			pending := operation.pending
			operation.pending = nil
			for _, event := range pending {
				if !operation.active {
					break
				}
				s.applyWebSocketEvent(operation, event)
			}
		}
	})
}

// CancelWebSocketConnection abandons a connection attempt.
func (s *WorkspaceStore) CancelWebSocketConnection(key string) {
	tab := s.FindTab(key)
	operation := s.sockets[key]
	if tab == nil || tab.WebSocket == nil || operation == nil || operation.connecting == nil || tab.WebSocket.ConnectionStatus != logic.WebSocketConnecting {
		return
	}
	operation.connecting()
}

// SendWebSocketMessage sends a tab's draft.
func (s *WorkspaceStore) SendWebSocketMessage(key string, done func(error)) {
	tab := s.FindTab(key)
	if tab == nil || tab.WebSocket == nil {
		return
	}
	state := tab.WebSocket
	if state.ConnectionStatus != logic.WebSocketConnected || state.SessionID == "" {
		if done != nil {
			done(errors.New("websocket session is not connected"))
		}
		return
	}
	if state.IsSendingMessage {
		return
	}
	state.IsSendingMessage = true
	request := proxyservice.WebSocketSendRequest{SessionID: state.SessionID, MsgType: "text", Text: state.DraftText}
	if state.DraftType == "binary-file" {
		request.MsgType, request.Text = "binary", ""
		if state.DraftFile != nil {
			request.File = &proxyservice.SendRequestFile{Path: state.DraftFile.Path, Name: state.DraftFile.Name, Size: state.DraftFile.Size}
		}
	}
	session := state.SessionID
	runtime.Run(s.rt.Jobs, "workspace:ws-send:"+key, func(context.Context) (struct{}, error) {
		return struct{}{}, s.backend.SendWebSocketMessage(request)
	}, func(_ struct{}, err error) {
		state.IsSendingMessage = false
		if err != nil && strings.Contains(err.Error(), webSocketSessionGone) {
			s.endWebSocketSession(key, session, state)
		}
		if done != nil {
			done(err)
		}
	})
}

// DisconnectWebSocket closes a tab's connection.
func (s *WorkspaceStore) DisconnectWebSocket(key string, done func(error)) {
	tab := s.FindTab(key)
	if tab == nil || tab.WebSocket == nil {
		return
	}
	state := tab.WebSocket
	session := strings.TrimSpace(state.SessionID)
	if session == "" {
		state.ConnectionStatus, state.ResponseError = logic.WebSocketClosed, ""
		return
	}
	runtime.Run(s.rt.Jobs, "workspace:ws-close:"+key, func(context.Context) (struct{}, error) {
		return struct{}{}, s.backend.DisconnectWebSocket(session)
	}, func(_ struct{}, err error) {
		if err == nil {
			s.endWebSocketSession(key, session, state)
		}
		if done != nil {
			done(err)
		}
	})
}

// Cleanup ends every request operation and the console flush.
func (s *WorkspaceStore) Cleanup() {
	for _, operation := range s.sockets {
		if operation.connecting != nil {
			operation.connecting()
		}
		s.endWebSocketOperation(operation)
	}
	for _, stream := range s.streams {
		session := stream.sessionID
		if stream.streaming {
			stream.state.IsStreaming, stream.state.StreamSessionID = false, ""
		}
		s.endHTTPOperation(stream)
		if session != "" {
			s.disconnectStream(session)
		}
	}
	if s.logs != nil {
		s.logs.Close()
	}
}
