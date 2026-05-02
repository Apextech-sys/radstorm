// Package collector — end-of-test aggregation into a Summary document.
//
// Purpose:
//
//	Walks the per-shard counters and the SubscriberOutcome list captured
//	by the collector and builds the Summary struct that marshals to
//	summary.json. Includes percentile math (min/p50/p95/p99/p999/max),
//	the establishment-over-time curve, and threshold evaluation.
//
// Related files:
//   - pkg/collector/collector.go (owns the shard counters this consumes)
//   - pkg/collector/summary.go (output type)
//   - .orchestration/contracts/results-schema.md (FROZEN — output shape)
//
// Briefing: .orchestration/briefings/1c-events.md
//
// Contract: Public — the Aggregate API and the Summary it produces are
// the stable contract consumed by apps/api and apps/web.
package collector

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/Apextech-sys/radstorm/pkg/events"
)

// shardAggregation is the per-shard counter set updated as events stream
// in (cheap) and read by Aggregate at the end (cheap reduction). One per
// shard ⇒ no contention while the test runs.
//
// Counters are intentionally typed as the same width they appear in the
// Summary so the final reduction is plain addition with no overflow risk
// at our event volumes (~10M/run).
type shardAggregation struct {
	mu sync.Mutex

	totalEvents int64

	// Establishment curve: for each subscriber observed by this shard,
	// when did it activate / establish? Stored as offset_ms keyed by
	// sub_id. seenSubs is the union of all subscribers we've ever seen
	// any event for (lets us count subs that were created but never
	// activated as "still in flight").
	seenSubs             map[uint32]struct{}
	activatedOffsetMs    map[uint32]int64
	establishedOffsetMs  map[uint32]int64
	terminalState        map[uint32]string
	authRetransmits      map[uint32]int32
	acctRetransmits      map[uint32]int32
	coaReceivedPerSub    map[uint32]int32
	disconnectReceivedAt map[uint32]int64

	// Aggregate rollups visible to the summary directly.
	totalRetransmits int64

	// CoA / Disconnect roll-ups (server listener events arrive on the
	// shard determined by the target subscriber's sub_id).
	coaReceived          int64
	coaAcked             int64
	coaNaked             int64
	coaDropped           int64
	coaResponseLatencies []int64 // microseconds

	disconnectReceived          int64
	disconnectAcked             int64
	disconnectNaked             int64
	disconnectResponseLatencies []int64 // microseconds

	// Errors recorded by the writer (file IO failures during flush).
	writeErrors []string
}

func newShardAggregation() *shardAggregation {
	return &shardAggregation{
		seenSubs:             make(map[uint32]struct{}),
		activatedOffsetMs:    make(map[uint32]int64),
		establishedOffsetMs:  make(map[uint32]int64),
		terminalState:        make(map[uint32]string),
		authRetransmits:      make(map[uint32]int32),
		acctRetransmits:      make(map[uint32]int32),
		coaReceivedPerSub:    make(map[uint32]int32),
		disconnectReceivedAt: make(map[uint32]int64),
	}
}

func (a *shardAggregation) recordWriteError(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.writeErrors = append(a.writeErrors, err.Error())
}

// observe consumes one event and updates this shard's counters. Called
// once per Submit, so it MUST be cheap. Called only from the owning
// shard goroutine — single-writer per map, no locking overhead vs. the
// hot path. Mu is taken only to protect against Aggregate reading
// mid-stream (which only happens after Stop ⇒ no contention then).
func (a *shardAggregation) observe(e events.Event) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.totalEvents++
	if e.SubscriberID != 0 {
		a.seenSubs[e.SubscriberID] = struct{}{}
	}

	switch e.Category {
	case events.CategorySubscriberLifecycle:
		switch e.EventType {
		case events.EventTypeSubscriberActivated:
			if _, ok := a.activatedOffsetMs[e.SubscriberID]; !ok {
				a.activatedOffsetMs[e.SubscriberID] = e.OffsetMs
			}
		case events.EventTypeSubscriberTerminal:
			a.terminalState[e.SubscriberID] = e.State
			if e.State == events.FinalStateEstablished {
				if _, ok := a.establishedOffsetMs[e.SubscriberID]; !ok {
					a.establishedOffsetMs[e.SubscriberID] = e.OffsetMs
				}
			}
		}
	case events.CategoryPacketOutbound:
		if e.EventType == events.EventTypeRequestRetransmitted {
			a.totalRetransmits++
			// Auth (Access-Request, code 1) vs Acct (Accounting-Request,
			// code 4). Anything else is unaccounted for.
			switch e.RadiusCode {
			case 1:
				a.authRetransmits[e.SubscriberID]++
			case 4:
				a.acctRetransmits[e.SubscriberID]++
			}
		}
	case events.CategoryCoAInbound:
		switch e.EventType {
		case events.EventTypeCoAReceived:
			a.coaReceived++
			a.coaReceivedPerSub[e.SubscriberID]++
		case events.EventTypeCoAAcked:
			a.coaAcked++
			if e.LatencyUs > 0 {
				a.coaResponseLatencies = append(a.coaResponseLatencies, e.LatencyUs)
			}
		case events.EventTypeCoANaked:
			a.coaNaked++
			if e.LatencyUs > 0 {
				a.coaResponseLatencies = append(a.coaResponseLatencies, e.LatencyUs)
			}
		case events.EventTypeCoADropped:
			a.coaDropped++
		}
	case events.CategoryDisconnectInbound:
		switch e.EventType {
		case events.EventTypeDisconnectReceived:
			a.disconnectReceived++
			a.disconnectReceivedAt[e.SubscriberID] = e.OffsetMs
		case events.EventTypeDisconnectAcked:
			a.disconnectAcked++
			if e.LatencyUs > 0 {
				a.disconnectResponseLatencies = append(a.disconnectResponseLatencies, e.LatencyUs)
			}
		case events.EventTypeDisconnectNaked:
			a.disconnectNaked++
			if e.LatencyUs > 0 {
				a.disconnectResponseLatencies = append(a.disconnectResponseLatencies, e.LatencyUs)
			}
		}
	}
}

// AggregateOpts controls how Aggregate constructs its Summary.
type AggregateOpts struct {
	RunID        string
	ConfigHash   string
	ScenarioType string
	StartedAt    time.Time
	FinishedAt   time.Time
	Outcome      string // OutcomeSucceeded | OutcomeFailed | OutcomeCancelled
	Thresholds   []Threshold
	// CurveBucketMs controls the establishment-curve sample width.
	// Default 1000 (1 second).
	CurveBucketMs int64
}

// Aggregate reduces every shard's counters into one Summary. Callable
// only after Stop; does not synchronise with running shard goroutines.
func (c *Collector) Aggregate(opts AggregateOpts) (*Summary, error) {
	if !c.stopped.Load() {
		return nil, fmt.Errorf("collector aggregate: must call Stop before Aggregate")
	}
	if opts.CurveBucketMs <= 0 {
		opts.CurveBucketMs = 1000
	}
	if opts.StartedAt.IsZero() {
		opts.StartedAt = c.startedAt
	}
	if opts.FinishedAt.IsZero() {
		opts.FinishedAt = time.Now().UTC()
	}
	if opts.Outcome == "" {
		opts.Outcome = OutcomeSucceeded
	}

	// Merge per-shard counters.
	var (
		totalEvents                            int64
		totalRetransmits                       int64
		coaRX, coaACK, coaNAK, coaDROP         int64
		discRX, discACK, discNAK               int64
		coaLatUs, discLatUs                    []int64
		writeErrs                              []string
		totalActivated, totalEstablished       int64
		earliestActivated, latestActivated     int64
		earliestEstablished, latestEstablished int64
		activatedSeen, establishedSeen         bool
	)

	merged := make(map[uint32]*subAgg)
	terminalCounts := map[string]int64{}

	for _, agg := range c.aggregations {
		agg.mu.Lock()
		totalEvents += agg.totalEvents
		totalRetransmits += agg.totalRetransmits
		coaRX += agg.coaReceived
		coaACK += agg.coaAcked
		coaNAK += agg.coaNaked
		coaDROP += agg.coaDropped
		coaLatUs = append(coaLatUs, agg.coaResponseLatencies...)
		discRX += agg.disconnectReceived
		discACK += agg.disconnectAcked
		discNAK += agg.disconnectNaked
		discLatUs = append(discLatUs, agg.disconnectResponseLatencies...)
		writeErrs = append(writeErrs, agg.writeErrors...)

		for subID, off := range agg.activatedOffsetMs {
			s := merged[subID]
			if s == nil {
				s = &subAgg{activatedOffsetMs: events.NotApplicableMs, establishedOffsetMs: events.NotApplicableMs}
				merged[subID] = s
			}
			s.activatedOffsetMs = off
			totalActivated++
			if !activatedSeen {
				earliestActivated = off
				latestActivated = off
				activatedSeen = true
			} else {
				if off < earliestActivated {
					earliestActivated = off
				}
				if off > latestActivated {
					latestActivated = off
				}
			}
		}
		for subID, off := range agg.establishedOffsetMs {
			s := merged[subID]
			if s == nil {
				s = &subAgg{activatedOffsetMs: events.NotApplicableMs, establishedOffsetMs: events.NotApplicableMs}
				merged[subID] = s
			}
			s.establishedOffsetMs = off
			totalEstablished++
			if !establishedSeen {
				earliestEstablished = off
				latestEstablished = off
				establishedSeen = true
			} else {
				if off < earliestEstablished {
					earliestEstablished = off
				}
				if off > latestEstablished {
					latestEstablished = off
				}
			}
		}
		for subID, st := range agg.terminalState {
			s := merged[subID]
			if s == nil {
				s = &subAgg{activatedOffsetMs: events.NotApplicableMs, establishedOffsetMs: events.NotApplicableMs}
				merged[subID] = s
			}
			s.terminalState = st
			terminalCounts[st]++
		}
		for subID, n := range agg.authRetransmits {
			if s, ok := merged[subID]; ok {
				s.authRetransmits = n
			} else {
				merged[subID] = &subAgg{activatedOffsetMs: events.NotApplicableMs, establishedOffsetMs: events.NotApplicableMs, authRetransmits: n}
			}
		}
		for subID, n := range agg.acctRetransmits {
			if s, ok := merged[subID]; ok {
				s.acctRetransmits = n
			} else {
				merged[subID] = &subAgg{activatedOffsetMs: events.NotApplicableMs, establishedOffsetMs: events.NotApplicableMs, acctRetransmits: n}
			}
		}
		// Ensure every subscriber that appeared in any event of this
		// shard has a row in `merged` — otherwise "created but never
		// activated" subscribers would vanish from Subscribers.Total.
		for subID := range agg.seenSubs {
			if _, ok := merged[subID]; !ok {
				merged[subID] = &subAgg{activatedOffsetMs: events.NotApplicableMs, establishedOffsetMs: events.NotApplicableMs}
			}
		}
		agg.mu.Unlock()
	}

	// Establishment latency distribution: established_offset - activated_offset for each subscriber that established.
	estLatencies := make([]int64, 0, totalEstablished)
	for _, s := range merged {
		if s.establishedOffsetMs >= 0 && s.activatedOffsetMs >= 0 {
			estLatencies = append(estLatencies, s.establishedOffsetMs-s.activatedOffsetMs)
		}
	}

	// Per-subscriber retransmit counts (sum of auth+acct).
	retxPerSub := make([]int64, 0, len(merged))
	subsWithRetx := int64(0)
	for _, s := range merged {
		total := int64(s.authRetransmits) + int64(s.acctRetransmits)
		retxPerSub = append(retxPerSub, total)
		if total > 0 {
			subsWithRetx++
		}
	}

	// Curve. Bucket activated and established events by CurveBucketMs,
	// then take a running sum so each row is a cumulative count.
	curve := buildEstablishmentCurve(merged, opts.CurveBucketMs)

	// Subscribers section.
	subsTotal := int64(len(merged))
	established := terminalCounts[events.FinalStateEstablished]
	authFailed := terminalCounts[events.FinalStateAuthFailed]
	acctFailed := terminalCounts[events.FinalStateAcctFailed]
	terminated := terminalCounts[events.FinalStateTerminated]
	stillInFlight := subsTotal
	for _, c := range terminalCounts {
		stillInFlight -= c
	}
	if stillInFlight < 0 {
		stillInFlight = 0
	}

	// Latency distributions.
	estDist := computeDistribution(estLatencies)
	retxDist := computeRetransmitDistribution(retxPerSub)

	// CoA / Disconnect latency distributions (nil when no traffic).
	var coaLatPtr, discLatPtr *LatencyDistribution
	if len(coaLatUs) > 0 {
		d := computeDistribution(coaLatUs)
		coaLatPtr = &d
	}
	if len(discLatUs) > 0 {
		d := computeDistribution(discLatUs)
		discLatPtr = &d
	}

	// Time-to-first / time-to-full establishment.
	timeToFirst := int64(0)
	timeToFull := int64(0)
	if establishedSeen {
		timeToFirst = earliestEstablished
		timeToFull = latestEstablished
	}

	// Threshold rollup.
	overall := evalThresholdsOverall(opts.Thresholds)

	sum := &Summary{
		RunID:        opts.RunID,
		ConfigHash:   opts.ConfigHash,
		StartedAt:    opts.StartedAt,
		FinishedAt:   opts.FinishedAt,
		DurationMs:   opts.FinishedAt.Sub(opts.StartedAt).Milliseconds(),
		ScenarioType: opts.ScenarioType,
		Outcome:      opts.Outcome,
		Subscribers: SubscribersSection{
			Total:              subsTotal,
			Established:        established,
			AuthFailed:         authFailed,
			AcctFailed:         acctFailed,
			Terminated:         terminated,
			StillInFlightAtEnd: stillInFlight,
		},
		Establishment: EstablishmentSection{
			TimeToFirstMs: timeToFirst,
			TimeToFullMs:  timeToFull,
			LatencyMs:     estDist,
			Curve:         curve,
		},
		Retransmits: RetransmitsSection{
			Total:                     totalRetransmits,
			SubscribersWithRetransmit: subsWithRetx,
			PerSubscriberDistribution: retxDist,
		},
		CoA: CoASection{
			Received:          coaRX,
			Acked:             coaACK,
			Naked:             coaNAK,
			Dropped:           coaDROP,
			ResponseLatencyUs: coaLatPtr,
		},
		Disconnect: DisconnectSection{
			Received:          discRX,
			Acked:             discACK,
			Naked:             discNAK,
			ResponseLatencyUs: discLatPtr,
		},
		ServerHealth: ServerHealthSection{
			UnresponsivePeriods: []UnresponsivePeriod{},
			ErrorResponses:      int64(len(writeErrs)),
		},
		Thresholds: ThresholdsSection{
			Evaluated: opts.Thresholds,
			Overall:   overall,
		},
		Artifacts: ArtifactsSection{
			EventsParquet:      "events-shard-*.parquet",
			SubscribersParquet: filepath.Base(c.outcomeWrite.path),
			RunLog:             "run.log",
		},
		MeasurementIntegrity: c.measurementIntegrity(opts.StartedAt),
	}

	// Suppress nil slices in JSON (the contract shows []).
	if sum.Thresholds.Evaluated == nil {
		sum.Thresholds.Evaluated = []Threshold{}
	}
	if sum.Establishment.Curve == nil {
		sum.Establishment.Curve = []CurvePoint{}
	}

	return sum, nil
}

// measurementIntegrity converts the collector's atomic counters into the
// summary section. Trust heuristic: 0 drops → high; <1% → medium; ≥1% → low.
//
// We deliberately keep the heuristic simple: an operator who sees "medium"
// or "low" should look at the raw counts and decide for themselves. The
// label is a hint, not a verdict.
func (c *Collector) measurementIntegrity(startedAt time.Time) MeasurementIntegritySection {
	submitted := c.totalSubmitted.Load()
	droppedFull := c.eventsDroppedFull.Load()
	droppedOutcomes := c.outcomesDroppedFull.Load()
	droppedAfterStop := c.dropped.Load()
	first := c.firstFullDropNs.Load()
	last := c.lastFullDropNs.Load()

	notes := []string{
		"Latency captured with monotonic clock immediately on packet receipt.",
		"Userspace timestamps via Go net.UDPConn — kernel scheduling jitter floor ≈50–200µs on a clean Linux box.",
	}

	trust := "high"
	if droppedFull > 0 || droppedOutcomes > 0 {
		// Compute drop rate over (successful + dropped) submission attempts.
		denom := submitted + droppedFull
		var rate float64
		if denom > 0 {
			rate = float64(droppedFull) / float64(denom)
		}
		if rate >= 0.01 {
			trust = "low"
			notes = append(notes,
				"Collector saturated: ≥1% of events dropped due to back-pressure. Latency tail percentiles should be treated with significant care; consider faster output disk, more CPU cores, or a smaller subscriber count.",
			)
		} else {
			trust = "medium"
			notes = append(notes,
				"Collector experienced brief back-pressure: a small number of events dropped. Latency aggregations are usable but tail percentiles may be slightly understated.",
			)
		}
	}

	out := MeasurementIntegritySection{
		Trust:                       trust,
		EventsSubmitted:             submitted,
		EventsDroppedBackPressure:   droppedFull,
		OutcomesDroppedBackPressure: droppedOutcomes,
		EventsDroppedAfterStop:      droppedAfterStop,
		Notes:                       notes,
	}
	if first > 0 {
		off := time.Unix(0, first).Sub(startedAt).Milliseconds()
		out.FirstDropOffsetMs = &off
	}
	if last > 0 {
		off := time.Unix(0, last).Sub(startedAt).Milliseconds()
		out.LastDropOffsetMs = &off
	}
	return out
}

// WriteSummary writes summary.json + summary.txt to the collector's
// output directory.
func (c *Collector) WriteSummary(sum *Summary) error {
	if sum == nil {
		return fmt.Errorf("write summary: nil summary")
	}
	jsonPath := filepath.Join(c.opts.Dir, "summary.json")
	jb, err := json.MarshalIndent(sum, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal summary.json: %w", err)
	}
	if err := os.WriteFile(jsonPath, jb, 0o644); err != nil {
		return fmt.Errorf("write summary.json: %w", err)
	}
	txtPath := filepath.Join(c.opts.Dir, "summary.txt")
	if err := os.WriteFile(txtPath, []byte(renderSummaryText(sum)), 0o644); err != nil {
		return fmt.Errorf("write summary.txt: %w", err)
	}
	return nil
}

// evalThresholdsOverall returns the rollup string for a list of
// individually-evaluated thresholds.
func evalThresholdsOverall(ts []Threshold) string {
	if len(ts) == 0 {
		return ThresholdsOverallNone
	}
	for _, t := range ts {
		if !t.Pass {
			return ThresholdsOverallFail
		}
	}
	return ThresholdsOverallPass
}

// computeDistribution returns the standard min/p50/p95/p99/p999/max
// bundle for a slice of int64 samples. Empty input ⇒ all zeroes.
func computeDistribution(samples []int64) LatencyDistribution {
	if len(samples) == 0 {
		return LatencyDistribution{}
	}
	cp := make([]int64, len(samples))
	copy(cp, samples)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	return LatencyDistribution{
		Min:  cp[0],
		P50:  percentile(cp, 0.50),
		P95:  percentile(cp, 0.95),
		P99:  percentile(cp, 0.99),
		P999: percentile(cp, 0.999),
		Max:  cp[len(cp)-1],
	}
}

// computeRetransmitDistribution is the int32-typed sibling of
// computeDistribution. Used for retransmits-per-subscriber where the
// counts are small ints.
func computeRetransmitDistribution(samples []int64) RetransmitDistribution {
	d := computeDistribution(samples)
	return RetransmitDistribution{
		P50: int32(d.P50),
		P95: int32(d.P95),
		P99: int32(d.P99),
		Max: int32(d.Max),
	}
}

// percentile returns the value at quantile q in the SORTED-ASCENDING
// slice. Uses the "nearest-rank" definition (PERCENTILE.EXC-style):
// the rank is ceil(q * N), the index is rank-1, clamped to the slice.
//
// Rationale: linear-interpolation gives confusing fractional integer
// values for small distributions. Nearest-rank produces stable, easy-
// to-explain results that match what people reading dashboards expect.
//
// Examples for sorted [1..1000]:
//
//	p50  → ceil(500)   → idx 499 → 500
//	p95  → ceil(950)   → idx 949 → 950
//	p99  → ceil(990)   → idx 989 → 990
//	p999 → ceil(999)   → idx 998 → 999
//	p100 → ceil(1000)  → idx 999 → 1000 (== max)
func percentile(sorted []int64, q float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	if q <= 0 {
		return sorted[0]
	}
	if q >= 1 {
		return sorted[len(sorted)-1]
	}
	rank := int(math.Ceil(q * float64(len(sorted))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}

// subAgg is the per-subscriber merge target used by Aggregate to fold
// every shard's per-sub maps into one. Carried at package scope so
// helpers (buildEstablishmentCurve) can reference the type.
type subAgg struct {
	activatedOffsetMs   int64
	establishedOffsetMs int64
	terminalState       string
	authRetransmits     int32
	acctRetransmits     int32
}

// buildEstablishmentCurve produces the over-time activated/established
// in_flight series. Buckets are CurveBucketMs wide.
func buildEstablishmentCurve(subs map[uint32]*subAgg, bucketMs int64) []CurvePoint {
	if len(subs) == 0 || bucketMs <= 0 {
		return []CurvePoint{}
	}
	var maxOff int64
	for _, s := range subs {
		if s.activatedOffsetMs > maxOff {
			maxOff = s.activatedOffsetMs
		}
		if s.establishedOffsetMs > maxOff {
			maxOff = s.establishedOffsetMs
		}
	}
	buckets := int(maxOff/bucketMs) + 1
	if buckets < 1 {
		buckets = 1
	}
	activatedDelta := make([]int64, buckets)
	establishedDelta := make([]int64, buckets)
	for _, s := range subs {
		if s.activatedOffsetMs >= 0 {
			b := int(s.activatedOffsetMs / bucketMs)
			if b >= 0 && b < buckets {
				activatedDelta[b]++
			}
		}
		if s.establishedOffsetMs >= 0 {
			b := int(s.establishedOffsetMs / bucketMs)
			if b >= 0 && b < buckets {
				establishedDelta[b]++
			}
		}
	}
	out := make([]CurvePoint, 0, buckets+1)
	out = append(out, CurvePoint{OffsetMs: 0, Activated: 0, Established: 0, InFlight: 0})
	var cumAct, cumEst int64
	for b := 0; b < buckets; b++ {
		cumAct += activatedDelta[b]
		cumEst += establishedDelta[b]
		out = append(out, CurvePoint{
			OffsetMs:    int64(b+1) * bucketMs,
			Activated:   cumAct,
			Established: cumEst,
			InFlight:    cumAct - cumEst,
		})
	}
	return out
}

// renderSummaryText is a minimal human-readable summary of the JSON
// document. Operators read this when diagnosing a failed run; it must
// fit comfortably in a terminal.
func renderSummaryText(s *Summary) string {
	overall := s.Thresholds.Overall
	if overall == "" {
		overall = "n/a"
	}
	out := fmt.Sprintf(
		"radstorm run summary\n"+
			"  run_id:        %s\n"+
			"  scenario:      %s\n"+
			"  outcome:       %s\n"+
			"  duration:      %d ms\n"+
			"  started:       %s\n"+
			"  finished:      %s\n"+
			"\n"+
			"subscribers\n"+
			"  total:                  %d\n"+
			"  established:            %d\n"+
			"  auth_failed:            %d\n"+
			"  acct_failed:            %d\n"+
			"  terminated:             %d\n"+
			"  still_in_flight_at_end: %d\n"+
			"\n"+
			"establishment\n"+
			"  time_to_first_ms: %d\n"+
			"  time_to_full_ms:  %d\n"+
			"  latency_ms: min=%d p50=%d p95=%d p99=%d p999=%d max=%d\n"+
			"\n"+
			"retransmits\n"+
			"  total:                       %d\n"+
			"  subscribers_with_retransmit: %d\n"+
			"  per_sub:  p50=%d p95=%d p99=%d max=%d\n"+
			"\n"+
			"coa\n"+
			"  received=%d acked=%d naked=%d dropped=%d\n"+
			"\n"+
			"disconnect\n"+
			"  received=%d acked=%d naked=%d\n"+
			"\n"+
			"thresholds (overall: %s)\n",
		s.RunID, s.ScenarioType, s.Outcome, s.DurationMs,
		s.StartedAt.Format(time.RFC3339), s.FinishedAt.Format(time.RFC3339),
		s.Subscribers.Total, s.Subscribers.Established, s.Subscribers.AuthFailed,
		s.Subscribers.AcctFailed, s.Subscribers.Terminated, s.Subscribers.StillInFlightAtEnd,
		s.Establishment.TimeToFirstMs, s.Establishment.TimeToFullMs,
		s.Establishment.LatencyMs.Min, s.Establishment.LatencyMs.P50,
		s.Establishment.LatencyMs.P95, s.Establishment.LatencyMs.P99,
		s.Establishment.LatencyMs.P999, s.Establishment.LatencyMs.Max,
		s.Retransmits.Total, s.Retransmits.SubscribersWithRetransmit,
		s.Retransmits.PerSubscriberDistribution.P50, s.Retransmits.PerSubscriberDistribution.P95,
		s.Retransmits.PerSubscriberDistribution.P99, s.Retransmits.PerSubscriberDistribution.Max,
		s.CoA.Received, s.CoA.Acked, s.CoA.Naked, s.CoA.Dropped,
		s.Disconnect.Received, s.Disconnect.Acked, s.Disconnect.Naked,
		overall,
	)
	for _, t := range s.Thresholds.Evaluated {
		mark := "FAIL"
		if t.Pass {
			mark = "PASS"
		}
		out += fmt.Sprintf("  [%s] %s — expected=%v actual=%v\n", mark, t.Name, t.Expected, t.Actual)
	}
	return out
}
