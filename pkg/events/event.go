// Package events — Event struct and helper constructors.
//
// Purpose:
//
//	Defines the canonical Event record produced by every component of the
//	harness (subscriber FSM, I/O sender/receiver, CoA/Disconnect listener)
//	and consumed by the sharded collector. The struct field names + types +
//	parquet column names are the wire/disk schema; downstream readers
//	(analyze-results CLI, frontend dashboard) depend on this shape.
//
// Related files:
//   - pkg/events/category.go (Category and EventType label constants)
//   - pkg/events/outcome.go (per-subscriber outcome record)
//   - pkg/collector/collector.go (consumer)
//   - pkg/collector/parquet.go (writer)
//   - .orchestration/contracts/event-schema.md (FROZEN contract — fields here
//     mirror the contract one-for-one; column names match)
//
// Briefing: .orchestration/briefings/1c-events.md
//
// Contract: Public — Event is the cross-package shared event type. Field
// types and parquet column names MUST match event-schema.md. Adding fields
// requires a contract amendment.
package events

import "time"

// Event is one observable occurrence in a test run.
//
// Parquet column names (the first token of each `parquet:` tag) are the
// wire-stable names from .orchestration/contracts/event-schema.md. The
// parquet-go/parquet-go logical-type hints (timestamp, uint(N), int(N),
// json) translate the Go types into the LogicalType annotations the
// contract specifies.
//
// Zero values: numeric metadata fields (RadiusCode, Identifier, LatencyUs,
// RetransmitN, ErrorCause) use 0 / -1 sentinels documented in the contract
// rather than nullable parquet columns to keep the schema flat and
// predictable for downstream analytical queries.
type Event struct {
	// Timing
	Timestamp   time.Time `parquet:"ts,timestamp(microsecond)"`
	MonotonicNs int64     `parquet:"mono_ns"`
	OffsetMs    int64     `parquet:"offset_ms"`

	// Source
	SubscriberID uint32 `parquet:"sub_id,uint(32)"`
	Category     string `parquet:"category"`
	EventType    string `parquet:"event_type"`
	State        string `parquet:"state"`

	// Packet metadata
	RadiusCode  int8   `parquet:"radius_code,int(8)"`
	Identifier  int16  `parquet:"identifier,int(16)"`
	LocalAddr   string `parquet:"local_addr"`
	RemoteAddr  string `parquet:"remote_addr"`
	PacketBytes int32  `parquet:"packet_bytes,int(32)"`

	// For replies — match latency to original request (0 if N/A).
	LatencyUs int64 `parquet:"latency_us"`

	// Retransmit / error context
	RetransmitN  int32  `parquet:"retransmit_n,int(32)"`
	ErrorCause   int32  `parquet:"error_cause,int(32)"`
	ErrorMessage string `parquet:"error_message"`

	// Tags is serialized to JSON in the Parquet `tags` column per contract.
	Tags map[string]string `parquet:"tags,json"`
}

// Sentinel values used in the schema where a numeric field "doesn't apply".
// Documented here so emitters and the aggregator agree on the meaning.
const (
	NoLatency    int64 = 0
	NoIdentifier int16 = -1
	NoRadiusCode int8  = 0
)

// NewSubscriberCreated returns a CategorySubscriberLifecycle event with
// EventTypeSubscriberCreated populated. Callers supply the subscriber ID
// and the test-T0-relative offset; the harness clock supplies wall +
// monotonic time.
func NewSubscriberCreated(subID uint32, offsetMs int64, state string) Event {
	return Event{
		Timestamp:    time.Now().UTC(),
		MonotonicNs:  monotonicNow(),
		OffsetMs:     offsetMs,
		SubscriberID: subID,
		Category:     CategorySubscriberLifecycle,
		EventType:    EventTypeSubscriberCreated,
		State:        state,
		Identifier:   NoIdentifier,
	}
}

// NewSubscriberActivated marks the moment a subscriber begins its
// authentication flow. Used to bucket the establishment curve.
func NewSubscriberActivated(subID uint32, offsetMs int64, state string) Event {
	return Event{
		Timestamp:    time.Now().UTC(),
		MonotonicNs:  monotonicNow(),
		OffsetMs:     offsetMs,
		SubscriberID: subID,
		Category:     CategorySubscriberLifecycle,
		EventType:    EventTypeSubscriberActivated,
		State:        state,
		Identifier:   NoIdentifier,
	}
}

// NewStateChanged records an FSM transition. State is the NEW state.
func NewStateChanged(subID uint32, offsetMs int64, newState string) Event {
	return Event{
		Timestamp:    time.Now().UTC(),
		MonotonicNs:  monotonicNow(),
		OffsetMs:     offsetMs,
		SubscriberID: subID,
		Category:     CategorySubscriberLifecycle,
		EventType:    EventTypeStateChanged,
		State:        newState,
		Identifier:   NoIdentifier,
	}
}

// NewTerminal marks the final state of a subscriber (established / failed /
// terminated). The State field is the terminal state.
func NewTerminal(subID uint32, offsetMs int64, terminalState string) Event {
	return Event{
		Timestamp:    time.Now().UTC(),
		MonotonicNs:  monotonicNow(),
		OffsetMs:     offsetMs,
		SubscriberID: subID,
		Category:     CategorySubscriberLifecycle,
		EventType:    EventTypeSubscriberTerminal,
		State:        terminalState,
		Identifier:   NoIdentifier,
	}
}

// NewRequestSent records a RADIUS request being placed on the wire. retryN
// is 0 for the original request and N>=1 for retransmits.
func NewRequestSent(subID uint32, offsetMs int64, state string, code int8, identifier int16, local, remote string, bytes int32, retryN int32) Event {
	et := EventTypeRequestSent
	if retryN > 0 {
		et = EventTypeRequestRetransmitted
	}
	return Event{
		Timestamp:    time.Now().UTC(),
		MonotonicNs:  monotonicNow(),
		OffsetMs:     offsetMs,
		SubscriberID: subID,
		Category:     CategoryPacketOutbound,
		EventType:    et,
		State:        state,
		RadiusCode:   code,
		Identifier:   identifier,
		LocalAddr:    local,
		RemoteAddr:   remote,
		PacketBytes:  bytes,
		RetransmitN:  retryN,
	}
}

// NewReplyReceived records a matched reply. latencyUs is the time from the
// matching request being sent to the reply being parsed.
func NewReplyReceived(subID uint32, offsetMs int64, state string, code int8, identifier int16, local, remote string, bytes int32, latencyUs int64) Event {
	return Event{
		Timestamp:    time.Now().UTC(),
		MonotonicNs:  monotonicNow(),
		OffsetMs:     offsetMs,
		SubscriberID: subID,
		Category:     CategoryPacketInbound,
		EventType:    EventTypeReplyReceived,
		State:        state,
		RadiusCode:   code,
		Identifier:   identifier,
		LocalAddr:    local,
		RemoteAddr:   remote,
		PacketBytes:  bytes,
		LatencyUs:    latencyUs,
	}
}

// NewDuplicateReply records a second reply for an already-completed
// request (typical when a retransmit raced with a slow original).
func NewDuplicateReply(subID uint32, offsetMs int64, code int8, identifier int16, local, remote string, bytes int32) Event {
	return Event{
		Timestamp:    time.Now().UTC(),
		MonotonicNs:  monotonicNow(),
		OffsetMs:     offsetMs,
		SubscriberID: subID,
		Category:     CategoryPacketInbound,
		EventType:    EventTypeDuplicateReply,
		RadiusCode:   code,
		Identifier:   identifier,
		LocalAddr:    local,
		RemoteAddr:   remote,
		PacketBytes:  bytes,
	}
}

// NewUnmatchedReply records a reply with no outstanding request. Sub ID
// is 0 because there's no owner. Useful for diagnosing server bugs or
// rogue traffic.
func NewUnmatchedReply(offsetMs int64, code int8, identifier int16, local, remote string, bytes int32) Event {
	return Event{
		Timestamp:   time.Now().UTC(),
		MonotonicNs: monotonicNow(),
		OffsetMs:    offsetMs,
		Category:    CategoryPacketInbound,
		EventType:   EventTypeUnmatchedReply,
		RadiusCode:  code,
		Identifier:  identifier,
		LocalAddr:   local,
		RemoteAddr:  remote,
		PacketBytes: bytes,
	}
}

// NewCoAEvent constructs an event for a server-initiated CoA. eventType
// must be one of EventTypeCoAReceived/Acked/Naked/Dropped. responseLatencyUs
// is filled for *Acked / *Naked, 0 otherwise.
func NewCoAEvent(subID uint32, offsetMs int64, eventType string, identifier int16, local, remote string, bytes int32, responseLatencyUs int64, errorCause int32) Event {
	return Event{
		Timestamp:    time.Now().UTC(),
		MonotonicNs:  monotonicNow(),
		OffsetMs:     offsetMs,
		SubscriberID: subID,
		Category:     CategoryCoAInbound,
		EventType:    eventType,
		Identifier:   identifier,
		LocalAddr:    local,
		RemoteAddr:   remote,
		PacketBytes:  bytes,
		LatencyUs:    responseLatencyUs,
		ErrorCause:   errorCause,
	}
}

// NewDisconnectEvent — sibling of NewCoAEvent for Disconnect-Request.
func NewDisconnectEvent(subID uint32, offsetMs int64, eventType string, identifier int16, local, remote string, bytes int32, responseLatencyUs int64, errorCause int32) Event {
	return Event{
		Timestamp:    time.Now().UTC(),
		MonotonicNs:  monotonicNow(),
		OffsetMs:     offsetMs,
		SubscriberID: subID,
		Category:     CategoryDisconnectInbound,
		EventType:    eventType,
		Identifier:   identifier,
		LocalAddr:    local,
		RemoteAddr:   remote,
		PacketBytes:  bytes,
		LatencyUs:    responseLatencyUs,
		ErrorCause:   errorCause,
	}
}

// NewError records an error attributable to a subscriber (subID==0 for
// system-wide errors). cause may be 0 if the error has no RFC 5176 code.
func NewError(subID uint32, offsetMs int64, eventType string, cause int32, message string) Event {
	if eventType == "" {
		eventType = EventTypeInternalError
	}
	return Event{
		Timestamp:    time.Now().UTC(),
		MonotonicNs:  monotonicNow(),
		OffsetMs:     offsetMs,
		SubscriberID: subID,
		Category:     CategoryError,
		EventType:    eventType,
		ErrorCause:   cause,
		ErrorMessage: message,
		Identifier:   NoIdentifier,
	}
}
