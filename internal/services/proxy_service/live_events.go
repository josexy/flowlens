package proxyservice

import "fmt"

// LiveEventKind names a kind of live event, so that drops can be counted and
// recovered from per kind.
type LiveEventKind uint8

const (
	LiveTrafficEntry LiveEventKind = iota + 1
	LiveTrafficPatch
	LiveTrafficLiveUpdate
	LiveTrafficReset
	LiveHTTPStream
	LiveWebSocket
)

func (k LiveEventKind) String() string {
	switch k {
	case LiveTrafficEntry:
		return "traffic entry"
	case LiveTrafficPatch:
		return "traffic patch"
	case LiveTrafficLiveUpdate:
		return "traffic live update"
	case LiveTrafficReset:
		return "traffic reset"
	case LiveHTTPStream:
		return "HTTP stream"
	case LiveWebSocket:
		return "WebSocket"
	default:
		return fmt.Sprintf("live event %d", uint8(k))
	}
}

// LiveEvent is one event of a LiveBatch. The set is closed: it holds the
// event types of this file, which publishers must not change once published.
type LiveEvent interface {
	// liveKind marks the closed set and names the event's kind.
	liveKind() LiveEventKind
	// liveSize approximates the memory a pending event keeps alive, so the
	// pending byte limit bounds bursts of large chunks and entries.
	liveSize() int
}

// Pending events are charged for what they keep alive: fixed costs per
// event and per header field, plus their text and payload bytes.
const (
	liveEventOverhead = 64
	liveFieldOverhead = 16
)

// TrafficEntryEvent creates or replaces a live capture entry with a complete,
// immutable snapshot.
type TrafficEntryEvent struct{ Entry *TrafficEntry }

// TrafficPatchEvent updates a live capture entry.
type TrafficPatchEvent struct{ Patch TrafficEntryPatch }

// TrafficLiveUpdateEvent streams SSE or WebSocket data of the entry shown in
// the capture detail panel.
type TrafficLiveUpdateEvent struct{ Update TrafficLiveUpdate }

// TrafficResetEvent ends one capture generation: events before it belong to
// the previous generation, events after it to CaptureGeneration.
type TrafficResetEvent struct{ CaptureGeneration uint64 }

// HTTPStreamEvent is an event of an HTTP Request Editor stream, bound to the
// sink of the send that started the stream.
type HTTPStreamEvent struct {
	Event HTTPRequestStreamEvent
	Sink  HTTPStreamSink
}

// Deliver hands the event to its stream's sink. The interface calls it on
// the main thread, in batch order.
func (e HTTPStreamEvent) Deliver() {
	if e.Sink != nil {
		e.Sink(e.Event)
	}
}

// WebSocketEvent is an event of a WebSocket Client session, bound to the
// sink of the connect that started the session.
type WebSocketEvent struct {
	Event WebSocketSessionEvent
	Sink  WebSocketSink
}

// Deliver hands the event to its session's sink. The interface calls it on
// the main thread, in batch order.
func (e WebSocketEvent) Deliver() {
	if e.Sink != nil {
		e.Sink(e.Event)
	}
}

func (TrafficEntryEvent) liveKind() LiveEventKind      { return LiveTrafficEntry }
func (TrafficPatchEvent) liveKind() LiveEventKind      { return LiveTrafficPatch }
func (TrafficLiveUpdateEvent) liveKind() LiveEventKind { return LiveTrafficLiveUpdate }
func (TrafficResetEvent) liveKind() LiveEventKind      { return LiveTrafficReset }
func (HTTPStreamEvent) liveKind() LiveEventKind        { return LiveHTTPStream }
func (WebSocketEvent) liveKind() LiveEventKind         { return LiveWebSocket }

func (e TrafficEntryEvent) liveSize() int {
	size := liveEventOverhead
	entry := e.Entry
	if entry == nil {
		return size
	}
	size += len(entry.Type) + len(entry.Method) + len(entry.URL) + len(entry.Host) + len(entry.Path) + len(entry.Status)
	size += len(entry.ResponseMetricsSource) + len(entry.RewriteExecutions)*liveFieldOverhead
	size += httpMessageLiveSize(entry.Request) + httpMessageLiveSize(entry.Response)
	if entry.Error != nil {
		size += len(entry.Error.Error)
	}
	if metadata := entry.Metadata; metadata != nil {
		size += len(metadata.LocalSourceAddr) + len(metadata.LocalDestinationAddr) + len(metadata.RemoteSourceAddr) + len(metadata.RemoteDestinationAddr)
		if metadata.Process != nil {
			size += processLiveSize(metadata.Process)
		}
	}
	return size
}

func (e TrafficPatchEvent) liveSize() int {
	patch := e.Patch
	size := liveEventOverhead + len(patch.ResponseMetricsSource) + len(patch.RewriteExecutions)*liveFieldOverhead
	if headers := patch.ResponseHeaders; headers != nil {
		size += len(headers.Status) + len(headers.Proto) + headerFieldsLiveSize(headers.HeaderFields)
	}
	if trailers := patch.ResponseTrailers; trailers != nil {
		size += headerFieldsLiveSize(trailers.TrailerFields)
	}
	if patch.Process != nil {
		size += processLiveSize(patch.Process)
	}
	if patch.Error != nil {
		size += len(patch.Error.Error)
	}
	return size
}

func (e TrafficLiveUpdateEvent) liveSize() int {
	return liveEventOverhead + len(e.Update.Chunk) + webSocketMessageLiveSize(e.Update.Message)
}

func (TrafficResetEvent) liveSize() int { return liveEventOverhead }

func (e HTTPStreamEvent) liveSize() int {
	event := e.Event
	return liveEventOverhead + len(event.SessionID) + len(event.Chunk) + headerFieldsLiveSize(event.TrailerFields) + len(event.Error)
}

func (e WebSocketEvent) liveSize() int {
	event := e.Event
	return liveEventOverhead + len(event.SessionID) + len(event.Status) + len(event.Error) + webSocketMessageLiveSize(event.Message)
}

func httpMessageLiveSize(message *HTTPMessage) int {
	if message == nil {
		return 0
	}
	return len(message.Proto) + headerFieldsLiveSize(message.HeaderFields) + headerFieldsLiveSize(message.TrailerFields)
}

func headerFieldsLiveSize(fields []HTTPHeaderField) int {
	size := 0
	for _, field := range fields {
		size += liveFieldOverhead + len(field.Name) + len(field.Value)
	}
	return size
}

func webSocketMessageLiveSize(message *WebSocketMessage) int {
	if message == nil {
		return 0
	}
	size := liveFieldOverhead + len(message.Direction) + len(message.MsgType) + len(message.Data)
	if message.Error != nil {
		size += len(message.Error.Error)
	}
	return size
}

func processLiveSize(process *ProcessInfo) int {
	return liveFieldOverhead + len(process.DisplayName) + len(process.ProcessName) + len(process.ExecutablePath) +
		len(process.AppID) + len(process.IconKey) + len(process.UnavailableReason)
}
