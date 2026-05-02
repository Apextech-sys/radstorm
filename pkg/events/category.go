// Package events — shared event types emitted by the test harness components.
//
// Purpose:
//   Defines the canonical Category constants used to classify events emitted
//   by the subscriber FSM, the I/O layer, and the server (CoA/Disconnect)
//   listener. Constants exist so producers and the collector agree on labels.
//
// Related files:
//   - pkg/events/event.go (Event struct that uses these constants)
//   - pkg/events/outcome.go (per-subscriber outcome record)
//   - .orchestration/contracts/event-schema.md (frozen contract — categories listed here)
//
// Briefing: .orchestration/briefings/1c-events.md
//
// Contract: Public — the string values are part of the Parquet event log
// schema and are consumed by analyze-results and the frontend dashboard.
package events

// Category labels classify events by the subsystem that emitted them.
// Values must remain stable (they appear in Parquet output and the UI).
const (
	CategorySubscriberLifecycle = "subscriber_lifecycle"
	CategoryPacketOutbound      = "packet_outbound"
	CategoryPacketInbound       = "packet_inbound"
	CategoryCoAInbound          = "coa_inbound"
	CategoryDisconnectInbound   = "disconnect_inbound"
	CategoryError               = "error"
)

// AllCategories returns every defined Category in declaration order.
// Useful for validation and tests.
func AllCategories() []string {
	return []string{
		CategorySubscriberLifecycle,
		CategoryPacketOutbound,
		CategoryPacketInbound,
		CategoryCoAInbound,
		CategoryDisconnectInbound,
		CategoryError,
	}
}

// EventType labels — the specific event within a Category. Producers MUST
// use one of these constants so the collector and downstream tooling can
// rely on a closed enumeration when grouping/filtering events.
const (
	// subscriber_lifecycle
	EventTypeSubscriberCreated   = "created"
	EventTypeSubscriberActivated = "activated"
	EventTypeStateChanged        = "state_changed"
	EventTypeSubscriberTerminal  = "terminal"

	// packet_outbound
	EventTypeRequestSent          = "request_sent"
	EventTypeRequestRetransmitted = "request_retransmitted"

	// packet_inbound
	EventTypeReplyReceived   = "reply_received"
	EventTypeDuplicateReply  = "duplicate_reply"
	EventTypeUnmatchedReply  = "unmatched_reply"

	// coa_inbound
	EventTypeCoAReceived = "coa_received"
	EventTypeCoAAcked    = "coa_acked"
	EventTypeCoANaked    = "coa_naked"
	EventTypeCoADropped  = "coa_dropped"

	// disconnect_inbound
	EventTypeDisconnectReceived = "disconnect_received"
	EventTypeDisconnectAcked    = "disconnect_acked"
	EventTypeDisconnectNaked    = "disconnect_naked"

	// error
	EventTypeValidationFailed = "validation_failed"
	EventTypeInternalError    = "internal_error"
)
