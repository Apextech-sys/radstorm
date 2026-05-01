# Contract: Event schema

**Owners:** `pkg/events` package (Go struct + Parquet schema), all subscriber/io/server emitters, `pkg/collector` consumer.

**Stability:** Frozen after Wave 1. Changes require a contract-amendment PR that updates this file, the Go struct, the Parquet schema, and any frontend consumers.

---

## Purpose

Every observable thing that happens during a test run is recorded as one `Event`. The collector aggregates these into Parquet files and into the final `summary.json`.

## Event categories

| Category | Emitted by | Examples |
|---|---|---|
| `subscriber_lifecycle` | subscriber FSM | `created`, `activated`, `state_changed`, `terminal` |
| `packet_outbound` | sender | `request_sent`, `request_retransmitted` |
| `packet_inbound` | receiver | `reply_received`, `duplicate_reply`, `unmatched_reply` |
| `coa_inbound` | server listener | `coa_received`, `coa_acked`, `coa_naked`, `coa_dropped` |
| `disconnect_inbound` | server listener | `disconnect_received`, `disconnect_acked`, `disconnect_naked` |
| `error` | any | `validation_failed`, `internal_error` |

## Go struct

```go
package events

import "time"

type Event struct {
    // Timing
    Timestamp     time.Time `parquet:"name=ts, type=INT64, convertedtype=TIMESTAMP_MICROS"`
    MonotonicNs   int64     `parquet:"name=mono_ns, type=INT64"`             // monotonic clock for latency math
    OffsetMs      int64     `parquet:"name=offset_ms, type=INT64"`           // ms since test T0

    // Source
    SubscriberID  uint32    `parquet:"name=sub_id, type=INT32, convertedtype=UINT_32"`
    Category      string    `parquet:"name=category, type=BYTE_ARRAY, convertedtype=UTF8"`   // see categories above
    EventType     string    `parquet:"name=event_type, type=BYTE_ARRAY, convertedtype=UTF8"` // e.g. "request_sent"
    State         string    `parquet:"name=state, type=BYTE_ARRAY, convertedtype=UTF8"`      // current FSM state

    // Packet metadata (for packet events)
    RadiusCode    int8      `parquet:"name=radius_code, type=INT32, convertedtype=INT_8"`    // 0 if N/A
    Identifier    int16     `parquet:"name=identifier, type=INT32, convertedtype=INT_16"`    // -1 if N/A
    LocalAddr     string    `parquet:"name=local_addr, type=BYTE_ARRAY, convertedtype=UTF8"`
    RemoteAddr    string    `parquet:"name=remote_addr, type=BYTE_ARRAY, convertedtype=UTF8"`
    PacketBytes   int32     `parquet:"name=packet_bytes, type=INT32"`

    // For replies — match latency to original request
    LatencyUs     int64     `parquet:"name=latency_us, type=INT64"`          // 0 if N/A

    // Retransmit/error
    RetransmitN   int32     `parquet:"name=retransmit_n, type=INT32"`        // 0 = original, 1+ = retry number
    ErrorCause    int32     `parquet:"name=error_cause, type=INT32"`         // RFC 5176 codes
    ErrorMessage  string    `parquet:"name=error_message, type=BYTE_ARRAY, convertedtype=UTF8"`

    // Extra
    Tags          map[string]string  // serialized to JSON in Parquet `tags` column
}
```

## Subscriber outcome (one row per subscriber, written at end of test)

```go
type SubscriberOutcome struct {
    SubscriberID         uint32    `parquet:"name=sub_id, type=INT32, convertedtype=UINT_32"`
    Username             string    `parquet:"name=username, type=BYTE_ARRAY, convertedtype=UTF8"`
    AuthMethod           string    `parquet:"name=auth_method, type=BYTE_ARRAY, convertedtype=UTF8"` // "pap" | "chap"
    SubType              string    `parquet:"name=sub_type, type=BYTE_ARRAY, convertedtype=UTF8"`    // "pppoe" | "mac"
    FinalState           string    `parquet:"name=final_state, type=BYTE_ARRAY, convertedtype=UTF8"`
    ActivatedAtOffsetMs  int64     `parquet:"name=activated_offset_ms, type=INT64"`
    EstablishedAtOffsetMs int64    `parquet:"name=established_offset_ms, type=INT64"` // -1 if not established
    EstablishmentLatencyMs int64   `parquet:"name=establishment_latency_ms, type=INT64"`  // -1 if N/A
    AuthRetransmits      int32     `parquet:"name=auth_retransmits, type=INT32"`
    AcctRetransmits      int32     `parquet:"name=acct_retransmits, type=INT32"`
    CoaReceivedCount     int32     `parquet:"name=coa_received, type=INT32"`
    DisconnectReceivedAt int64     `parquet:"name=disconnect_offset_ms, type=INT64"`  // -1 if not disconnected
    FailureReason        string    `parquet:"name=failure_reason, type=BYTE_ARRAY, convertedtype=UTF8"` // "" if no failure
}
```

## Cardinality and volume estimates

At 1M subscribers in cold-start scenario:
- ~5–10M events total (per-subscriber: created, activated, request_sent, reply_received, state_changed × 2, request_sent acct, reply_received acct, terminal — plus retransmits in the tail)
- 1M outcomes
- ~1.5GB Parquet uncompressed, ~200–400MB with snappy compression

## Channel sizing

Collector channels: sharded N=runtime.NumCPU(). Per-shard buffer ≥ 16384. Subscribers hash to a shard via `(sub_id % N)`. Shard receivers each own their own Parquet writer to avoid contention.

## Periodic flush

Default interval 30s. On flush: write all buffered events to Parquet, rotate writer if file size exceeds 256MB, fsync. Final flush on test end.
