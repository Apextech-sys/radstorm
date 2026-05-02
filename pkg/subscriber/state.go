// Package subscriber — per-virtual-subscriber FSM that drives one
// simulated client through the full RADIUS Access + Accounting flow.
//
// Purpose:
//
//	Defines the State enum exactly as documented in spec §2.3 and
//	docs/ARCHITECTURE.md, plus a few helpers for classifying a state
//	(terminal vs in-flight) used by the FSM and the test outcome
//	emission path.
//
// Related files:
//   - pkg/subscriber/subscriber.go (Subscriber struct + Run lifecycle)
//   - pkg/subscriber/pool.go       (Pool of Subscribers + lookup indexes)
//   - pkg/subscriber/lookup.go     (server-listener lookup interface)
//   - pkg/events/outcome.go        (FinalState* constants mirror these strings)
//   - .orchestration/contracts/event-schema.md (state names appear in the
//     `state` Parquet column)
//
// Briefing: .orchestration/briefings/2b-subscriber.md
//
// Contract: State string values are observable in the Parquet event log
// and the per-subscriber outcome record. They MUST stay in lockstep with
// the FinalState* constants in pkg/events.
package subscriber

// State is the current FSM state of a single subscriber.
//
// Lifecycle (per spec §2.3):
//
//	idle
//	  └─ activate ──► auth_sent
//	                     ├─ auth_retry (on retransmit) ──► auth_sent
//	                     ├─ auth_failed (terminal)
//	                     └─ Access-Accept ──► acct_sent (if IncludeAcctStart)
//	                                              ├─ acct_retry ──► acct_sent
//	                                              ├─ acct_failed (terminal)
//	                                              └─ Accounting-Response ──► established
//	                          (if IncludeAcctStart=false, Access-Accept ──► established directly)
//	  established ──► terminated (on inbound Disconnect-Request)
type State string

// Subscriber FSM states. The string values are the canonical labels used
// in the Parquet `state` column (event-schema.md) and the `final_state`
// column of subscribers.parquet (results-schema.md).
const (
	StateIdle        State = "idle"
	StateAuthSent    State = "auth_sent"
	StateAuthRetry   State = "auth_retry"
	StateAuthFailed  State = "auth_failed"
	StateAcctSent    State = "acct_sent"
	StateAcctRetry   State = "acct_retry"
	StateAcctFailed  State = "acct_failed"
	StateEstablished State = "established"
	StateTerminated  State = "terminated"
)

// String returns the state label (so State satisfies fmt.Stringer
// implicitly via the underlying string conversion).
func (s State) String() string { return string(s) }

// IsTerminal reports whether the state is a final FSM state — the
// subscriber has emitted (or is about to emit) its SubscriberOutcome
// and will not transition further (except established → terminated
// via inbound Disconnect-Request, which is treated as a fresh terminal
// transition).
func (s State) IsTerminal() bool {
	switch s {
	case StateAuthFailed, StateAcctFailed, StateEstablished, StateTerminated:
		return true
	default:
		return false
	}
}

// IsFailure reports whether the state is a failed terminal state. Used
// by the outcome emitter to decide whether to populate FailureReason.
func (s State) IsFailure() bool {
	switch s {
	case StateAuthFailed, StateAcctFailed:
		return true
	default:
		return false
	}
}
