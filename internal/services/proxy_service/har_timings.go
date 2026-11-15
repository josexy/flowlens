package proxyservice

import "strings"

type harConnectionPhaseKey struct {
	localConnection string
	phase           int
	start, end      int64
}

type harConnectionTimingOwner struct {
	id      uint64
	start   int64
	written bool
}

func harConnectionPhases(timings *HTTPConnectionTimings) [3][2]int64 {
	return [3][2]int64{
		{timings.DNSStartedAtMicros, timings.DNSEndedAtMicros},
		{timings.ConnectStartedAtMicros, timings.ConnectEndedAtMicros},
		{timings.TLSStartedAtMicros, timings.TLSEndedAtMicros},
	}
}

func harPhaseKey(md *Metadata, phase int, times [2]int64) harConnectionPhaseKey {
	return harConnectionPhaseKey{
		localConnection: harConnectionKey(md.LocalSourceAddr, md.LocalDestinationAddr, md.LocalConnectionEstablishedAt),
		phase:           phase, start: times[0], end: times[1],
	}
}

// ObserveConnectionTimings registers an entry from the source capture/history
// without loading its body or exporting it. Call for all source entries before
// WriteEntry so selections and reverse export order retain the same ownership.
// With no prepass, a streaming caller must supply entries chronologically.
func (w *HARFileWriter) ObserveConnectionTimings(entry *TrafficEntry) {
	if w != nil && !w.finished {
		w.connections.observeConnectionTimings(entry)
	}
}

func (ids *harConnectionIDs) observeConnectionTimings(entry *TrafficEntry) {
	if !harSupports(entry) || entry.Metadata == nil || entry.Metadata.ConnectionTimings == nil {
		return
	}
	start, end := harMessageTimestamps(entry.Request)
	if start < 0 {
		return
	}
	for index, phase := range harConnectionPhases(entry.Metadata.ConnectionTimings) {
		if phase[0] < 0 || phase[1] < phase[0] || (end >= 0 && phase[1] > end) {
			continue
		}
		key := harPhaseKey(entry.Metadata, index, phase)
		owner, exists := ids.timingOwners[key]
		if exists && (owner.written || owner.start < start || (owner.start == start && owner.id <= entry.ID)) {
			continue
		}
		if ids.timingOwners == nil {
			ids.timingOwners = make(map[harConnectionPhaseKey]harConnectionTimingOwner)
		}
		ids.timingOwners[key] = harConnectionTimingOwner{id: entry.ID, start: start}
	}
}

// applyConnectionTimings assigns each observed connection phase once, to the
// earliest recorded request that carries it. Requests reusing that connection
// keep dns/connect/ssl at -1: these phases do not apply to the current request.
// Setup may precede request_started:
// extend the HAR start/total to its actual start and account for intervening
// setup/forwarding gaps in blocked. Only the overlapping part is removed from
// send. HTTP message timestamps and the saved connection metadata stay intact.
func (ids *harConnectionIDs) applyConnectionTimings(timings *harTimings, entry *TrafficEntry, total float64) (int64, float64) {
	start, end := harMessageTimestamps(entry.Request)
	if entry.Metadata == nil || entry.Metadata.ConnectionTimings == nil {
		return start, total
	}
	dns, connect, ssl := float64(-1), float64(-1), float64(-1)
	timings.DNS, timings.Connect, timings.SSL = &dns, &connect, &ssl
	timings.Comment = strings.TrimSpace(timings.Comment + " Connection phases are assigned once to their earliest recorded request. Requests reusing an existing connection keep dns/connect/ssl at -1 because no new connection setup occurs. blocked includes setup/forwarding gaps before the HTTP exchange. Original timestamps are in _connectionTimings (Unix microseconds).")
	if start < 0 {
		return start, total
	}
	phases := harConnectionPhases(entry.Metadata.ConnectionTimings)
	durations := [3]int64{-1, -1, -1}
	harStart, previousEnd := start, int64(-1)
	var beforeRequest, insideRequest int64
	for index, phase := range phases {
		key := harPhaseKey(entry.Metadata, index, phase)
		owner, exists := ids.timingOwners[key]
		if !exists || owner.written || owner.id != entry.ID || owner.start != start {
			continue
		}
		if phase[0] < previousEnd {
			// Preserve raw overlapping observations without inventing a serial
			// HAR waterfall or subtracting the same interval twice.
			return start, total
		}
		duration := phase[1] - phase[0]
		durations[index] = duration
		before := max(int64(0), min(phase[1], start)-phase[0])
		beforeRequest += before
		insideRequest += duration - before
		harStart = min(harStart, phase[0])
		previousEnd = phase[1]
	}
	if durations[0] >= 0 {
		dns = float64(durations[0]) / 1000
	}
	if durations[1] >= 0 {
		connect = float64(durations[1]) / 1000
	}
	if durations[2] >= 0 {
		ssl = float64(durations[2]) / 1000
		connect = max(connect, 0) + ssl // HAR connect includes TLS.
	}
	if harStart < start {
		blocked := float64(start-harStart-beforeRequest) / 1000
		timings.Blocked = &blocked
	}
	if timings.Send >= 0 {
		timings.Send = float64(end-start-insideRequest) / 1000
	}
	if total >= 0 {
		_, responseEnd := harMessageTimestamps(entry.Response)
		total = float64(responseEnd-harStart) / 1000
	}
	for index, duration := range durations {
		if duration >= 0 {
			key := harPhaseKey(entry.Metadata, index, phases[index])
			owner := ids.timingOwners[key]
			owner.written = true
			ids.timingOwners[key] = owner
		}
	}
	return harStart, total
}
