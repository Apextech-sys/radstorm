// Package events — tests for Event constructors and Parquet schema mapping.
//
// Purpose:
//
//	Pin the Parquet schema (column names + logical types) so accidental
//	field renames or tag changes break the build before they break
//	downstream Parquet readers. Also exercise every constructor to keep
//	coverage high.
//
// Related files:
//   - pkg/events/event.go (subject under test)
//   - pkg/events/category.go (constants)
//   - .orchestration/contracts/event-schema.md (the schema being pinned)
//
// Briefing: .orchestration/briefings/1c-events.md
//
// Contract: internal — test-only.
package events

import (
	"strings"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// schemaSnapshot is the exact rendering of the Event Parquet schema as of
// the frozen contract. If any field name or logical type drifts, this
// snapshot must change AND the contract must be amended in the same PR.
const schemaSnapshot = `message Event {
	required int64 ts (TIMESTAMP(isAdjustedToUTC=true,unit=MICROS));
	required int64 mono_ns (INT(64,true));
	required int64 offset_ms (INT(64,true));
	required int32 sub_id (INT(32,false));
	required binary category (STRING);
	required binary event_type (STRING);
	required binary state (STRING);
	required int32 radius_code (INT(8,true));
	required int32 identifier (INT(16,true));
	required binary local_addr (STRING);
	required binary remote_addr (STRING);
	required int32 packet_bytes (INT(32,true));
	required int64 latency_us (INT(64,true));
	required int32 retransmit_n (INT(32,true));
	required int32 error_cause (INT(32,true));
	required binary error_message (STRING);
	required binary tags (JSON);
}`

const outcomeSchemaSnapshot = `message SubscriberOutcome {
	required int32 sub_id (INT(32,false));
	required binary username (STRING);
	required binary auth_method (STRING);
	required binary sub_type (STRING);
	required binary final_state (STRING);
	required int64 activated_offset_ms (INT(64,true));
	required int64 established_offset_ms (INT(64,true));
	required int64 establishment_latency_ms (INT(64,true));
	required int32 auth_retransmits (INT(32,true));
	required int32 acct_retransmits (INT(32,true));
	required int32 coa_received (INT(32,true));
	required int64 disconnect_offset_ms (INT(64,true));
	required binary failure_reason (STRING);
}`

func TestEventSchemaMatchesContract(t *testing.T) {
	got := strings.TrimSpace(parquet.SchemaOf(Event{}).String())
	want := strings.TrimSpace(schemaSnapshot)
	assert.Equal(t, want, got, "Event Parquet schema drifted from frozen contract")
}

func TestSubscriberOutcomeSchemaMatchesContract(t *testing.T) {
	got := strings.TrimSpace(parquet.SchemaOf(SubscriberOutcome{}).String())
	want := strings.TrimSpace(outcomeSchemaSnapshot)
	assert.Equal(t, want, got, "SubscriberOutcome Parquet schema drifted from frozen contract")
}

func TestAllCategoriesUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range AllCategories() {
		assert.False(t, seen[c], "duplicate category %q", c)
		seen[c] = true
	}
	assert.Len(t, AllCategories(), 6)
}

func TestNewSubscriberCreated(t *testing.T) {
	e := NewSubscriberCreated(42, 1234, "idle")
	assert.Equal(t, uint32(42), e.SubscriberID)
	assert.Equal(t, int64(1234), e.OffsetMs)
	assert.Equal(t, CategorySubscriberLifecycle, e.Category)
	assert.Equal(t, EventTypeSubscriberCreated, e.EventType)
	assert.Equal(t, "idle", e.State)
	assert.Equal(t, NoIdentifier, e.Identifier)
	assert.False(t, e.Timestamp.IsZero())
}

func TestNewSubscriberActivated(t *testing.T) {
	e := NewSubscriberActivated(7, 0, "auth_sent")
	assert.Equal(t, EventTypeSubscriberActivated, e.EventType)
	assert.Equal(t, "auth_sent", e.State)
}

func TestNewStateChanged(t *testing.T) {
	e := NewStateChanged(7, 100, "established")
	assert.Equal(t, EventTypeStateChanged, e.EventType)
	assert.Equal(t, "established", e.State)
}

func TestNewTerminal(t *testing.T) {
	e := NewTerminal(7, 200, FinalStateEstablished)
	assert.Equal(t, EventTypeSubscriberTerminal, e.EventType)
	assert.Equal(t, FinalStateEstablished, e.State)
}

func TestNewRequestSentOriginal(t *testing.T) {
	e := NewRequestSent(1, 50, "auth_sent", 1, 12, "10.0.0.1:1812", "10.0.0.2:1812", 80, 0)
	assert.Equal(t, EventTypeRequestSent, e.EventType)
	assert.Equal(t, int32(0), e.RetransmitN)
	assert.Equal(t, int8(1), e.RadiusCode)
	assert.Equal(t, int16(12), e.Identifier)
}

func TestNewRequestSentRetry(t *testing.T) {
	e := NewRequestSent(1, 50, "auth_retry", 1, 12, "", "", 80, 2)
	assert.Equal(t, EventTypeRequestRetransmitted, e.EventType)
	assert.Equal(t, int32(2), e.RetransmitN)
}

func TestNewReplyReceived(t *testing.T) {
	e := NewReplyReceived(1, 60, "auth_sent", 2, 12, "", "", 40, 12345)
	assert.Equal(t, CategoryPacketInbound, e.Category)
	assert.Equal(t, EventTypeReplyReceived, e.EventType)
	assert.Equal(t, int64(12345), e.LatencyUs)
}

func TestNewDuplicateReply(t *testing.T) {
	e := NewDuplicateReply(1, 70, 2, 12, "", "", 40)
	assert.Equal(t, EventTypeDuplicateReply, e.EventType)
}

func TestNewUnmatchedReply(t *testing.T) {
	e := NewUnmatchedReply(70, 2, 99, "", "", 40)
	assert.Equal(t, EventTypeUnmatchedReply, e.EventType)
	assert.Equal(t, uint32(0), e.SubscriberID)
}

func TestNewCoAEvent(t *testing.T) {
	e := NewCoAEvent(5, 1000, EventTypeCoAAcked, 100, "", "", 60, 250, 0)
	assert.Equal(t, CategoryCoAInbound, e.Category)
	assert.Equal(t, EventTypeCoAAcked, e.EventType)
	assert.Equal(t, int64(250), e.LatencyUs)
}

func TestNewDisconnectEvent(t *testing.T) {
	e := NewDisconnectEvent(5, 1000, EventTypeDisconnectAcked, 100, "", "", 60, 300, 0)
	assert.Equal(t, CategoryDisconnectInbound, e.Category)
	assert.Equal(t, EventTypeDisconnectAcked, e.EventType)
}

func TestNewError(t *testing.T) {
	e := NewError(0, 9999, "", 502, "boom")
	assert.Equal(t, CategoryError, e.Category)
	assert.Equal(t, EventTypeInternalError, e.EventType, "empty event type defaults to internal_error")
	assert.Equal(t, int32(502), e.ErrorCause)
	assert.Equal(t, "boom", e.ErrorMessage)
}

func TestNewErrorWithExplicitType(t *testing.T) {
	e := NewError(7, 0, EventTypeValidationFailed, 0, "bad attr")
	assert.Equal(t, EventTypeValidationFailed, e.EventType)
}

func TestMonotonicNowMonotonic(t *testing.T) {
	a := monotonicNow()
	time.Sleep(2 * time.Millisecond)
	b := monotonicNow()
	require.Greater(t, b, a)
}
