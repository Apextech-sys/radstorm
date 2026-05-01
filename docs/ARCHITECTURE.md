# Architecture

## Goal

Simulate up to 1M virtual RADIUS subscribers from a single host. Measure how a target RADIUS server behaves under realistic ISP-scale load (cold-start outage recovery, CoA storms).

This document is the canonical architecture reference. Contracts and schemas are kept in `.orchestration/contracts/`.

## High-level shape

```
┌────────────────────────┐   REST    ┌─────────────────────────┐  spawn  ┌─────────────────────────┐
│  Next.js frontend       │◀────────▶│  Go HTTP API server      │────────▶│  Go CLI (radstorm)       │
│  (apps/web)             │   SSE    │  (apps/api)              │         │  (apps/cli)              │
│  - config form          │          │  - run lifecycle         │         │  - scenario driver       │
│  - run trigger          │          │  - stream progress (SSE) │         │  - subscriber pool       │
│  - results dashboard    │          │  - serve results         │         │  - I/O + collector       │
└────────────────────────┘          └─────────────────────────┘         └────────────┬────────────┘
                                                                                      │ UDP RADIUS
                                                                                      ▼
                                                                        ┌─────────────────────────┐
                                                                        │  RADIUS server under     │
                                                                        │  test (Docker FreeRADIUS │
                                                                        │  for local E2E)          │
                                                                        └─────────────────────────┘
```

The CLI is the actual test engine. The API server is a thin process supervisor that runs the CLI as a subprocess (stdio for control, file watching for results), exposing REST + SSE to the frontend. The frontend never invokes the binary directly.

## Component boundaries

### `pkg/radius` — Protocol layer
Owns RFC 2865/2866/3576/5176 packet construction and parsing, shared secret authentication, Message-Authenticator computation, dictionary loading (RFC + Huawei VSA, vendor 2011). Exposes `Packet`, `EncodePacket`, `DecodePacket`, attribute helpers. Wraps `layeh.com/radius` where useful.

### `pkg/config` — Configuration
Loads TOML, validates against schema in `.orchestration/contracts/config-schema.md`, returns typed `*Config`. Loads credentials from CSV.

### `pkg/events` — Shared event types
Defines the `Event` struct and Parquet schema emitted by subscribers/IO/server listener and consumed by the collector. Lives in its own package so all components depend on the same shape.

### `pkg/io` — Packet I/O
- `SocketPool`: bind UDP sockets across configured source IPs
- `IDAllocator`: per-`(srcIP, srcPort, dstIP, dstPort)` 8-bit ID bitmap
- `ReplyMatcher`: correlation table; matches replies to outstanding requests by `(localAddr, identifier, requestAuthenticator)`; detects duplicates
- `Sender`: sends with retransmit policy; releases ID on reply or final timeout
- `Receiver`: parses inbound; routes replies to ReplyMatcher, server-initiated packets to `pkg/server`

### `pkg/subscriber` — Per-subscriber state machine
One instance per virtual subscriber. State: `idle | auth_sent | auth_retry | auth_failed | acct_sent | acct_retry | acct_failed | established | terminated`. Handles `activate`, `reply_received`, `timeout`, `coa_received`, `disconnect_received` events.

### `pkg/server` — CoA/Disconnect listener
UDP server. Validates Message-Authenticator. Decodes attributes incl. Huawei VSAs. Looks up target subscriber. Sends ACK/NAK. Records latency.

### `pkg/scenario` — Scenario driver
Loads config, computes activation schedule per ramp curve, manages lifecycle (`warmup → ramp → drain → finalize`), coordinates shutdown.

### `pkg/collector` — Result collection
Sharded channels (one per CPU core), in-memory ring buffer, periodic Parquet flush, end-of-test summary aggregation.

### `apps/cli/cmd/radstorm` — CLI entry point
Subcommands:
- `run-scenario --config <path> --out <dir>` — runs a scenario
- `validate-config <path>` — schema check + dry-run summary
- `analyze-results <dir>` — reads Parquet, prints summary
- `single-auth --config <path> --user <u> --pass <p>` — Phase 1 dev tool

### `apps/api/cmd/radstorm-api` — HTTP API
REST endpoints per `.orchestration/contracts/rest-api.md`. Spawns the CLI as a subprocess for each run. Watches the output directory for results. Streams progress via SSE.

### `apps/web` — Next.js frontend
Pages: Dashboard (recent runs), New Run (config form), Run Detail (live progress), Results (dashboard with charts).

## The hard parts

### RADIUS Identifier exhaustion
8-bit Identifier limits in-flight requests per `(srcIP, srcPort, dstIP, dstPort)` tuple to 256. To support 1M concurrent in-flight, need ≥4000 distinct `(srcIP, srcPort)` combinations. Configured source IPs (e.g. 32 aliases) × ~125 source ports each. The allocator must:
- Maintain per-tuple bitmap of 256 IDs
- Block (or fail-fast) when full
- Free IDs on reply receipt or final timeout

### Reply matching under retransmit
On retransmit, a NEW Identifier is allocated (RFC-correct: same ID would race with possibly-late original reply). If both replies eventually arrive, second is detected as duplicate (same Authenticator) and logged but not re-triggering state.

### Goroutine model
1M subscribers each as a goroutine (Go can handle this; ~2KB stack each ⇒ ~2GB at scale). I/O workers are a *bounded* pool sized to CPU count to avoid stampeding the network stack. Subscribers and workers communicate via channels.

### Collector backpressure
At pessimal (1M Access-Requests in <1s), event emission can spike to ~5M/sec briefly. Sharded channels (N collector goroutines, subscribers hash to a shard) absorb the burst without subscribers blocking on emission (which would skew latency measurements).

### Cancellation
Single root `context.Context` propagates cancel. On Ctrl-C: stop new activations, drain in-flight (configurable timeout), flush collector buffers, write final summary. Shutdown sequence is coordinated.

## Why this layering

The CLI is the engine because the test must be runnable headlessly on a dedicated host (1M subs needs serious hardware, not always co-located with the UI host). The API server exists so the frontend can drive runs without invoking binaries from a browser process. The frontend is a separate concern entirely — config and visualization, not test execution.

## Cross-cutting

- **Logging:** `slog` JSON to stdout for the operational log; subscribed events go to the collector, NOT to slog
- **Metrics:** None internal — the test report IS the output
- **Errors:** Wrapped with context; never panic in production paths
- **Config secrets:** RADIUS shared secret read from config file; never logged

## Local dev rig

Docker compose at `test/docker/docker-compose.yml` brings up FreeRADIUS with seeded test users. The E2E suite at `test/e2e/` runs the CLI against this rig at small scale (1k–5k subscribers). This is what gates `Wave 4`.
