package logic

import (
	"bytes"
	"strings"
	"unicode/utf8"
)

const (
	// SSEPreviewLength is how many characters of data a preview shows.
	SSEPreviewLength = 160

	ssePreviewSourceBytes = (SSEPreviewLength*2 + 2) * utf8.UTFMax
)

var sseByteOrderMark = []byte{0xEF, 0xBB, 0xBF}

// SSEEvent indexes one event in a decoded response body. Its range refers to
// the unchanged body, so payloads are kept once.
type SSEEvent struct {
	Sequence    int
	EventType   string
	LastEventID string
	RawStart    int
	RawEnd      int
	Preview     string
}

// SSEMessageDetail is an event with its payload materialized.
type SSEMessageDetail struct {
	SSEEvent
	Data string
	Raw  string
}

func sseFieldValue(line string) (string, string) {
	colon := strings.IndexByte(line, ':')
	if colon < 0 {
		return line, ""
	}
	start := colon + 1
	if start < len(line) && line[start] == ' ' {
		start++
	}
	return line[:colon], line[start:]
}

func ssePreview(prefix string, dataLength int) string {
	truncated := dataLength > len(prefix)
	if !truncated {
		prefix = prefix[:dataLength]
	}
	characters := []rune(strings.ReplaceAll(prefix, "\n", " ↵ "))
	if truncated || len(characters) > SSEPreviewLength {
		limit := min(len(characters), SSEPreviewLength-1)
		return string(characters[:limit]) + "…"
	}
	return string(characters)
}

// SSEParser is an incremental WHATWG event-stream parser over bytes.
type SSEParser struct {
	offset          int
	rawStart        int
	line            []byte
	skipLF          bool
	crBoundary      bool
	crEvent         *SSEEvent
	blockHasContent bool
	eventType       string
	lastEventID     string
	hasData         bool
	dataLength      int
	preview         []byte
	sequence        int
	bomMatched      int
}

// HasPendingEvent reports whether the stream ended inside an event.
func (p *SSEParser) HasPendingEvent() bool { return p.blockHasContent }

// Append parses a chunk and returns the events it completed.
func (p *SSEParser) Append(chunk string) []*SSEEvent {
	var events []*SSEEvent
	segmentStart := 0
	for index := 0; index < len(chunk); index, p.offset = index+1, p.offset+1 {
		value := chunk[index]
		if p.skipLF {
			p.skipLF = false
			if value == '\n' {
				// CR dispatches at once; a later LF still belongs to its block.
				if p.crBoundary {
					p.rawStart = p.offset + 1
				}
				if p.crEvent != nil {
					p.crEvent.RawEnd = p.offset + 1
				}
				p.crEvent = nil
				segmentStart = index + 1
				continue
			}
			p.crEvent = nil
		}
		if p.offset < len(sseByteOrderMark) && p.bomMatched == p.offset && value == sseByteOrderMark[p.offset] {
			p.bomMatched++
			if p.bomMatched == len(sseByteOrderMark) {
				p.line = p.line[:0]
				segmentStart = index + 1
			}
			continue
		}
		if value != '\n' && value != '\r' {
			p.blockHasContent = true
			continue
		}
		p.line = append(p.line, chunk[segmentStart:index]...)
		line := string(p.line)
		p.line = p.line[:0]
		segmentStart = index + 1
		event := p.processLine(line, p.offset+1)
		if event != nil {
			events = append(events, event)
		}
		p.skipLF = value == '\r'
		p.crBoundary = line == ""
		p.crEvent = nil
		if p.skipLF {
			p.crEvent = event
		}
	}
	if segmentStart < len(chunk) {
		p.line = append(p.line, chunk[segmentStart:]...)
	}
	return events
}

func (p *SSEParser) processLine(line string, rawEnd int) *SSEEvent {
	if line == "" {
		var event *SSEEvent
		if p.hasData {
			p.sequence++
			eventType := p.eventType
			if eventType == "" {
				eventType = "message"
			}
			event = &SSEEvent{
				Sequence: p.sequence, EventType: eventType, LastEventID: p.lastEventID,
				RawStart: p.rawStart, RawEnd: rawEnd, Preview: ssePreview(string(p.preview), p.dataLength-1),
			}
		}
		p.rawStart = rawEnd
		p.blockHasContent, p.hasData = false, false
		p.eventType, p.dataLength = "", 0
		p.preview = p.preview[:0]
		return event
	}
	if line[0] == ':' {
		return nil
	}
	field, value := sseFieldValue(line)
	switch field {
	case "data":
		p.hasData = true
		p.dataLength += len(value) + 1
		if remaining := ssePreviewSourceBytes - len(p.preview); remaining > 0 {
			if len(value) < remaining {
				p.preview = append(append(p.preview, value...), '\n')
				break
			}
			end := remaining
			for end > 0 && !utf8.RuneStart(value[end]) {
				end--
			}
			p.preview = append(p.preview, value[:end]...)
		}
	case "event":
		p.eventType = value
	case "id":
		if !strings.Contains(value, "\x00") {
			p.lastEventID = value
		}
	}
	return nil
}

// SSEEventDetail returns an event's data and its original text.
func SSEEventDetail(body string, event SSEEvent) SSEMessageDetail {
	raw := body[event.RawStart:event.RawEnd]
	rest := raw
	if bytes.HasPrefix([]byte(rest), sseByteOrderMark) {
		rest = rest[len(sseByteOrderMark):]
	}
	var data []string
	for {
		index := strings.IndexAny(rest, "\r\n")
		line := rest
		if index >= 0 {
			line = rest[:index]
		}
		if field, value := sseFieldValue(line); field == "data" {
			data = append(data, value)
		}
		if index < 0 {
			break
		}
		if rest[index] == '\r' && index+1 < len(rest) && rest[index+1] == '\n' {
			index++
		}
		rest = rest[index+1:]
	}
	return SSEMessageDetail{SSEEvent: event, Data: strings.Join(data, "\n"), Raw: raw}
}

// IsEventStream reports whether a content type is text/event-stream.
func IsEventStream(contentType string) bool {
	mediaType, _, _ := strings.Cut(contentType, ";")
	return strings.EqualFold(strings.TrimSpace(mediaType), "text/event-stream")
}
