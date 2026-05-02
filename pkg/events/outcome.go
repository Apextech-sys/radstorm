// Package events — per-subscriber outcome record (one per subscriber, end-of-run).
//
// Purpose:
//
//	At the end of a test the harness writes one SubscriberOutcome per
//	simulated subscriber summarising its journey. This is the canonical
//	long-form per-subscriber record consumed by the analyze-results CLI
//	and the per-subscriber drill-down view in the frontend.
//
// Related files:
//   - pkg/events/event.go (per-occurrence Event records)
//   - pkg/collector/parquet.go (writer)
//   - pkg/collector/aggregate.go (consumer for summary roll-ups)
//   - .orchestration/contracts/event-schema.md (FROZEN — fields and column
//     names mirror the contract one-for-one)
//
// Briefing: .orchestration/briefings/1c-events.md
//
// Contract: Public — SubscriberOutcome is the per-subscriber wire schema.
package events

// AuthMethod values. These appear in the Parquet `auth_method` column.
const (
	AuthMethodPAP  = "pap"
	AuthMethodCHAP = "chap"
)

// SubType values. These appear in the Parquet `sub_type` column.
const (
	SubTypePPPoE = "pppoe"
	SubTypeMAC   = "mac"
)

// FinalState values. The terminal state from the subscriber FSM.
const (
	FinalStateEstablished = "established"
	FinalStateAuthFailed  = "auth_failed"
	FinalStateAcctFailed  = "acct_failed"
	FinalStateTerminated  = "terminated"
	FinalStateInFlight    = "in_flight" // recorded if test ended before subscriber finished
)

// Sentinel value used in *_offset_ms / *_latency_ms columns when the
// underlying milestone never occurred (e.g. subscriber never established).
const NotApplicableMs int64 = -1

// SubscriberOutcome is a single subscriber's end-of-run summary record.
// Field types and parquet column names are frozen by event-schema.md.
type SubscriberOutcome struct {
	SubscriberID           uint32 `parquet:"sub_id,uint(32)"`
	Username               string `parquet:"username"`
	AuthMethod             string `parquet:"auth_method"`
	SubType                string `parquet:"sub_type"`
	FinalState             string `parquet:"final_state"`
	ActivatedAtOffsetMs    int64  `parquet:"activated_offset_ms"`
	EstablishedAtOffsetMs  int64  `parquet:"established_offset_ms"`
	EstablishmentLatencyMs int64  `parquet:"establishment_latency_ms"`
	AuthRetransmits        int32  `parquet:"auth_retransmits,int(32)"`
	AcctRetransmits        int32  `parquet:"acct_retransmits,int(32)"`
	CoaReceivedCount       int32  `parquet:"coa_received,int(32)"`
	DisconnectReceivedAt   int64  `parquet:"disconnect_offset_ms"`
	FailureReason          string `parquet:"failure_reason"`
}
