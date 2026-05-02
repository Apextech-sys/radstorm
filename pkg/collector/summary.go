// Package collector — Summary type mirroring summary.json from results-schema.md.
//
// Purpose:
//
//	Defines the Go structs that marshal to the summary.json document
//	produced at the end of every run. The JSON tags pin the wire field
//	names to the frozen contract in .orchestration/contracts/results-schema.md.
//
// Related files:
//   - pkg/collector/aggregate.go (constructs Summary)
//   - pkg/collector/collector.go (orchestrates aggregation)
//   - .orchestration/contracts/results-schema.md (FROZEN contract)
//
// Briefing: .orchestration/briefings/1c-events.md
//
// Contract: Public — Summary marshals to the wire JSON consumed by
// apps/api and apps/web. Field names and shape MUST match the contract.
package collector

import "time"

// Outcome values for the top-level run outcome field.
const (
	OutcomeSucceeded = "succeeded"
	OutcomeFailed    = "failed"
	OutcomeCancelled = "cancelled"

	ThresholdsOverallPass = "pass"
	ThresholdsOverallFail = "fail"
	ThresholdsOverallNone = "none" // when no thresholds were configured
)

// Threshold is one user-configured pass/fail check evaluated at the end
// of a run. Expected and Actual are deliberately free-form (string|number)
// — they appear as JSON values in the document.
type Threshold struct {
	Name     string      `json:"name"`
	Expected interface{} `json:"expected"`
	Actual   interface{} `json:"actual"`
	Pass     bool        `json:"pass"`
}

// LatencyDistribution is the standard min/p50/p95/p99/p999/max bundle.
// Units are determined by the field name in the parent struct (e.g.
// "latency_ms" vs "response_latency_us").
type LatencyDistribution struct {
	Min  int64 `json:"min"`
	P50  int64 `json:"p50"`
	P95  int64 `json:"p95"`
	P99  int64 `json:"p99"`
	P999 int64 `json:"p999"`
	Max  int64 `json:"max"`
}

// RetransmitDistribution reports per-subscriber retransmit counts. Distinct
// from LatencyDistribution: bounds are tighter (typical subscriber: 0).
type RetransmitDistribution struct {
	P50 int32 `json:"p50"`
	P95 int32 `json:"p95"`
	P99 int32 `json:"p99"`
	Max int32 `json:"max"`
}

// CurvePoint is one sample on the establishment-over-time curve.
type CurvePoint struct {
	OffsetMs    int64 `json:"offset_ms"`
	Activated   int64 `json:"activated"`
	Established int64 `json:"established"`
	InFlight    int64 `json:"in_flight"`
}

// SubscribersSection — the "subscribers" block of summary.json.
type SubscribersSection struct {
	Total              int64 `json:"total"`
	Established        int64 `json:"established"`
	AuthFailed         int64 `json:"auth_failed"`
	AcctFailed         int64 `json:"acct_failed"`
	Terminated         int64 `json:"terminated"`
	StillInFlightAtEnd int64 `json:"still_in_flight_at_end"`
}

// EstablishmentSection — the "establishment" block of summary.json.
type EstablishmentSection struct {
	TimeToFirstMs int64               `json:"time_to_first_ms"`
	TimeToFullMs  int64               `json:"time_to_full_ms"`
	LatencyMs     LatencyDistribution `json:"latency_ms"`
	Curve         []CurvePoint        `json:"curve"`
}

// RetransmitsSection — the "retransmits" block of summary.json.
type RetransmitsSection struct {
	Total                     int64                  `json:"total"`
	SubscribersWithRetransmit int64                  `json:"subscribers_with_retransmit"`
	PerSubscriberDistribution RetransmitDistribution `json:"per_subscriber_distribution"`
}

// CoASection — the "coa" block of summary.json.
// ResponseLatencyUs is a *LatencyDistribution so it serializes as null
// when no CoA traffic was exchanged.
type CoASection struct {
	Received          int64                `json:"received"`
	Acked             int64                `json:"acked"`
	Naked             int64                `json:"naked"`
	Dropped           int64                `json:"dropped"`
	ResponseLatencyUs *LatencyDistribution `json:"response_latency_us"`
}

// DisconnectSection — the "disconnect" block of summary.json.
type DisconnectSection struct {
	Received          int64                `json:"received"`
	Acked             int64                `json:"acked"`
	Naked             int64                `json:"naked"`
	ResponseLatencyUs *LatencyDistribution `json:"response_latency_us"`
}

// UnresponsivePeriod marks a window during which the server failed to
// respond to any in-flight request.
type UnresponsivePeriod struct {
	StartOffsetMs int64 `json:"start_offset_ms"`
	EndOffsetMs   int64 `json:"end_offset_ms"`
}

// ServerHealthSection — the "server_health" block of summary.json.
type ServerHealthSection struct {
	UnresponsivePeriods []UnresponsivePeriod `json:"unresponsive_periods"`
	ErrorResponses      int64                `json:"error_responses"`
}

// ThresholdsSection — the "thresholds" block of summary.json.
type ThresholdsSection struct {
	Evaluated []Threshold `json:"evaluated"`
	Overall   string      `json:"overall"`
}

// ArtifactsSection — pointers to the sibling files in the run directory.
type ArtifactsSection struct {
	EventsParquet      string `json:"events_parquet"`
	SubscribersParquet string `json:"subscribers_parquet"`
	RunLog             string `json:"run_log"`
}

// Summary is the root type that marshals to summary.json.
type Summary struct {
	RunID        string    `json:"run_id"`
	ConfigHash   string    `json:"config_hash"`
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at"`
	DurationMs   int64     `json:"duration_ms"`
	ScenarioType string    `json:"scenario_type"`
	Outcome      string    `json:"outcome"`

	Subscribers   SubscribersSection   `json:"subscribers"`
	Establishment EstablishmentSection `json:"establishment"`
	Retransmits   RetransmitsSection   `json:"retransmits"`
	CoA           CoASection           `json:"coa"`
	Disconnect    DisconnectSection    `json:"disconnect"`
	ServerHealth  ServerHealthSection  `json:"server_health"`
	Thresholds    ThresholdsSection    `json:"thresholds"`
	Artifacts     ArtifactsSection     `json:"artifacts"`
}
