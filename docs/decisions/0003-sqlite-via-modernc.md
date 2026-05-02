# ADR 0003 — SQLite via modernc.org/sqlite (pure Go) instead of mattn/go-sqlite3 (CGO)

<!-- Purpose: Documents the choice of pure-Go SQLite driver for the API server's run store, and why CGO was avoided. -->

**Status:** Accepted

**Date:** 2026-05-02

---

## Context

The API server (`apps/api`) needs persistent storage for run metadata: run IDs, statuses, timestamps, and references to result directories. The storage requirements are simple — one table, single writer, infrequent reads, must survive API server restarts.

SQLite is a natural fit: no separate database process, single file, ACID transactions, well-understood schema migration path.

Two Go SQLite drivers exist:

- **`mattn/go-sqlite3`**: the most widely used Go SQLite binding. Wraps the SQLite C library via CGO. Requires a C compiler at build time.
- **`modernc.org/sqlite`**: a pure-Go port of SQLite (the SQLite C source is mechanically translated to Go by the `ccgo` toolchain). No CGO, no C compiler required.

The development environment for this autonomous build is Windows, and the Windows host does not have `gcc`, `clang`, or any C compiler installed. This was confirmed by the Wave 2A sub-agent:

```
$ go test ./pkg/io/... -race
go: -race requires cgo; enable cgo by setting CGO_ENABLED=1
$ CGO_ENABLED=1 go test ./pkg/io/... -race
# runtime/cgo
cgo: C compiler "gcc" not found: exec: "gcc": executable file not found in %PATH%
```

CGO-dependent packages fail to build on this host even for packages that do not strictly need CGO (only the race detector uses it), let alone for a package that is entirely CGO-based.

---

## Decision

Use `modernc.org/sqlite` (pure Go, no CGO) as the SQLite driver for the API server's run store.

The store uses:
- WAL mode (`PRAGMA journal_mode=WAL`) for concurrent read safety
- Busy timeout (`PRAGMA busy_timeout=5000`) to handle brief lock contention
- A single `runs` table with status enum and JSON columns for config and progress

---

## Consequences

**Positive:**

- `make build` and `go build ./...` succeed on the Windows development host without installing a C compiler. The build is fully cross-compilable.
- The resulting binary is self-contained. Deploying to a Linux production host requires no system SQLite library (`libsqlite3-dev` package or equivalent).
- CI on Linux runners works without any additional setup steps. The pure-Go binary cross-compiles cleanly.

**Negative / trade-offs:**

- **Performance**: `modernc.org/sqlite` is consistently slower than `mattn/go-sqlite3` in benchmarks — approximately 2-3x for write-heavy workloads. For the API server's use case (one run at a time, O(1) writes per second), this is irrelevant. No performance requirement exists for the run store.
- **Binary size**: the pure-Go port embeds the translated SQLite source, adding ~4 MB to the API binary. Acceptable given that the binary is a single-purpose server.
- **Ecosystem familiarity**: `mattn/go-sqlite3` is more widely documented. Stack Overflow answers and library integrations (e.g., GORM, sqlc) default to `mattn`. Using `modernc.org/sqlite` requires a slightly different driver registration name (`modernc.org/sqlite` registers as `sqlite`, not `sqlite3`) and occasional driver-specific workarounds.
- **Version lag**: `modernc.org/sqlite` tracks SQLite releases with a delay. As of `v1.50.0`, it ships SQLite 3.49.x. New SQLite features land in `modernc.org/sqlite` weeks to months later than in the upstream C library.

---

## Alternatives considered

### `mattn/go-sqlite3` with CGO

Rejected for this build because the development host has no C compiler. In a production scenario where all build hosts run Linux with `gcc` available, `mattn/go-sqlite3` would be the appropriate choice. If the build environment constraint changes, migrating from `modernc.org/sqlite` to `mattn/go-sqlite3` requires only a driver import swap and the registration name change — no schema or query changes.

### PostgreSQL or MySQL

Rejected. The API server is a local single-tenant tool designed to run on the same host as the CLI. An external database process adds an operational dependency that is disproportionate to the storage need (tens to hundreds of run records). SQLite's "serverless" model is the right fit.

### In-memory store (no persistence)

Rejected. The API server must survive restarts without losing run history. The "orphan reaping" behaviour on restart (transitioning `queued`/`running` rows to `failed`) requires a durable store. An in-memory store was used in the Wave 1F API skeleton and was replaced with SQLite in Wave 3B precisely because restart recovery was identified as a requirement.

### BoltDB / BadgerDB (pure-Go key-value stores)

Considered briefly. These are pure-Go and CGO-free, but they are key-value stores, not relational. The run store benefits from SQL filtering (`WHERE status = 'running'`, `ORDER BY created_at DESC LIMIT 50`). Implementing equivalent query logic on a KV store adds code complexity. SQLite is simpler.
