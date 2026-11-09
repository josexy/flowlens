package logic

import proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"

// TrafficPatch is an incremental update of an entry.
type TrafficPatch = proxyservice.TrafficEntryPatch

// HTTPMessageSide names a side of an exchange.
type HTTPMessageSide string

// Sides of an exchange.
const (
	SideRequest  HTTPMessageSide = "request"
	SideResponse HTTPMessageSide = "response"
)

func isTerminalMessage(message *proxyservice.HTTPMessage) bool {
	if message == nil || message.Metrics == nil {
		return false
	}
	switch message.Metrics.State {
	case proxyservice.HTTPMessageStateCompleted, proxyservice.HTTPMessageStateFailed, proxyservice.HTTPMessageStateCanceled:
		return true
	}
	return false
}

// NewTerminalMessageSides lists the sides that finished between two versions.
func NewTerminalMessageSides(previous, next *TrafficEntry) []HTTPMessageSide {
	var sides []HTTPMessageSide
	if !isTerminalMessage(previous.Request) && isTerminalMessage(next.Request) {
		sides = append(sides, SideRequest)
	}
	if !isTerminalMessage(previous.Response) && isTerminalMessage(next.Response) {
		sides = append(sides, SideResponse)
	}
	return sides
}

// TerminalBodyRefreshQueue decides when a finished message needs its body
// loaded again, merging duplicates and waiting for a load in progress.
type TerminalBodyRefreshQueue struct {
	entryID  uint64
	active   bool
	observed map[HTTPMessageSide]bool
	pending  bool
}

// Activate starts tracking an entry.
func (q *TerminalBodyRefreshQueue) Activate(entryID uint64) {
	if q.active && q.entryID == entryID {
		return
	}
	q.entryID, q.active = entryID, true
	q.observed = map[HTTPMessageSide]bool{}
	q.pending = false
}

// Cancel stops tracking.
func (q *TerminalBodyRefreshQueue) Cancel() {
	q.active, q.entryID = false, 0
	q.observed = nil
	q.pending = false
}

// Request records finished sides and reports whether to load now.
func (q *TerminalBodyRefreshQueue) Request(entryID uint64, sides []HTTPMessageSide, loading bool) bool {
	if !q.active || entryID != q.entryID {
		return false
	}
	hasNew := false
	for _, side := range sides {
		if !q.observed[side] {
			q.observed[side] = true
			hasNew = true
		}
	}
	if !hasNew {
		return false
	}
	if loading {
		q.pending = true
		return false
	}
	return true
}

// CompleteLoad reports whether a refresh was deferred until this load ended.
func (q *TerminalBodyRefreshQueue) CompleteLoad(entryID uint64) bool {
	if !q.active || entryID != q.entryID || !q.pending {
		return false
	}
	q.pending = false
	return true
}

func withMetrics(message *proxyservice.HTTPMessage, metrics *proxyservice.HTTPMessageMetrics) *proxyservice.HTTPMessage {
	if metrics == nil {
		return message
	}
	copied := copyMessage(message)
	copied.Metrics = metrics
	return copied
}

func copyMessage(message *proxyservice.HTTPMessage) *proxyservice.HTTPMessage {
	if message == nil {
		return &proxyservice.HTTPMessage{HeaderFields: []HeaderField{}, TrailerFields: []HeaderField{}}
	}
	copied := *message
	return &copied
}

// ApplyTrafficPatch returns a new entry with the patch applied, or entry itself
// when the patch is stale or for another entry. Entries are never changed in
// place because other windows and exports may hold them.
func ApplyTrafficPatch(entry *TrafficEntry, patch *TrafficPatch) *TrafficEntry {
	if entry.ID != patch.TrafficID || patch.Revision <= entry.Revision {
		return entry
	}
	next := *entry
	next.Revision = patch.Revision

	if headers := patch.ResponseHeaders; headers != nil {
		response := copyMessage(next.Response)
		response.Proto = headers.Proto
		response.HeaderFields = headers.HeaderFields
		response.HeadersTruncated = headers.HeadersTruncated
		response.HeaderOrderUnavailable = headers.HeaderOrderUnavailable
		next.Response = response
		next.StatusCode = headers.StatusCode
		next.Status = headers.Status
	}
	if trailers := patch.ResponseTrailers; trailers != nil {
		response := copyMessage(next.Response)
		response.TrailerFields = trailers.TrailerFields
		response.TrailersTruncated = trailers.TrailersTruncated
		response.TrailerOrderUnavailable = trailers.TrailerOrderUnavailable
		next.Response = response
	}
	if metrics := patch.Metrics; metrics != nil {
		next.Request = withMetrics(next.Request, metrics.Request)
		next.Response = withMetrics(next.Response, metrics.Response)
		if metrics.Connection != nil {
			metadata := copyMetadata(next.Metadata)
			metadata.ConnectionTimings = metrics.Connection
			next.Metadata = metadata
		}
	}
	if patch.Process != nil {
		metadata := copyMetadata(next.Metadata)
		metadata.Process = patch.Process
		next.Metadata = metadata
	}
	if patch.StartedAt != nil {
		next.StartedAt = *patch.StartedAt
	}
	if patch.Error != nil {
		next.Error = patch.Error
	}
	if patch.RewriteExecutions != nil {
		next.RewriteExecutions = patch.RewriteExecutions
	}
	if patch.ResponseMetricsSource != "" {
		next.ResponseMetricsSource = patch.ResponseMetricsSource
	}
	return &next
}

func copyMetadata(metadata *proxyservice.Metadata) *proxyservice.Metadata {
	if metadata == nil {
		return &proxyservice.Metadata{}
	}
	copied := *metadata
	return &copied
}
