<!-- Purpose: ADR for the future addition of NIC hardware timestamping to lower the measurement-floor below the userspace ~50–200µs ceiling. -->

# ADR 0004 — Hardware timestamping (deferred)

**Status:** Considered, deferred. Not in scope for v0.1. Tracked here as a design note for a future contributor.

**Context date:** 2026-05-02

## Context

radstorm currently uses Go's standard `net.UDPConn` for all RADIUS I/O. This gives userspace timestamps: the kernel timestamps the packet on receipt, then passes it up to Go's runtime, which delivers it to the receive goroutine. The combined kernel scheduling jitter + Go runtime delivery typically caps measurement precision at **50–200µs** on a clean Linux box, worse on a busy machine.

For the current target use case — evaluating ISP RADIUS solutions where p99 establishment latency thresholds are measured in seconds and even p50 is in the tens-of-ms range — this floor is fine. The signal we care about is several orders of magnitude above the noise.

A more demanding use case would be: validating a vendor's claim of "p99 auth latency under 100µs." radstorm cannot reliably verify or falsify such a claim today; the noise floor swamps the signal.

The Linux kernel provides `SO_TIMESTAMPING` socket options that, on a NIC supporting hardware timestamping (PTP-class cards: NVIDIA/Mellanox ConnectX-5/6, Intel X710 series, some Solarflare), record packet timestamps **at the wire** before kernel scheduling enters the picture. Effective precision is sub-microsecond.

## Decision

**Defer hardware timestamping until there is a concrete use case requiring sub-millisecond measurement precision.**

The deferral is principled, not lazy. The reasoning:

1. **No current customer need.** ISP CTOs evaluating RADIUS care about cascading-failure resilience and outage recovery time, both of which live in the seconds range. Sub-millisecond precision is not on the requirements list.
2. **Substantial implementation cost.** Doing hardware timestamping correctly requires:
   - Linux-only build path (Windows/macOS dev environments lose feature parity)
   - Switch from `net.UDPConn` to raw sockets via `golang.org/x/sys/unix`
   - Per-NIC capability detection (`SIOCGHWTSTAMP` ioctl)
   - Graceful fallback to userspace timestamps when HW unavailable
   - PHC (PTP Hardware Clock) discipline to keep the NIC clock in sync with the system clock
   - New code paths in the receiver/sender, increasing maintenance surface
3. **NIC dependency.** Even with the code in place, hardware timestamping requires a capable NIC. We cannot enforce that operators have one. So the feature would be opt-in and best-effort — adding configuration surface and test-matrix complexity for a benefit most operators won't realise.
4. **Marginal returns at our target scale.** At 1M subscribers in a cold-start scenario, the latency tail is dominated by RADIUS server processing, not measurement noise. Lowering our measurement floor doesn't change the p99 number meaningfully.

## Consequences

**Accepted:**
- radstorm cannot defend or refute RADIUS server claims at sub-millisecond precision. The README and `RESULTS-INTERPRETATION.md` document this floor explicitly.
- Operators measuring sub-millisecond systems (e.g. tightly-tuned in-DC RADIUS with no cross-hop traffic) need a different tool.

**Mitigated by:**
- The `measurement_integrity.notes` field in `summary.json` always includes the userspace-timestamp disclaimer, so any consumer of a summary file knows the precision floor.

**Reversal cost:**
- Implementing this later is straightforward but non-trivial. Estimated effort: 2–3 days of focused work for a Go developer comfortable with `golang.org/x/sys/unix`. The receiver/sender abstractions in `pkg/io/` are already factored such that the underlying socket type can be swapped behind the same interface.

## When to revisit

Revisit this decision if any of these become true:

1. A customer (existing or prospective) explicitly asks for sub-millisecond latency validation.
2. radstorm is positioned for a use case beyond ISP RADIUS evaluation — e.g. low-latency trading, telecom 5G core (URLLC), where sub-ms matters.
3. NIC support becomes ubiquitous enough that requiring it stops being a differentiator (currently most commodity 1–10 GbE NICs do not support hardware timestamping).

## Implementation sketch (for the future contributor)

If you're picking this up:

1. **New build tag:** `//go:build linux` for the HW-timestamping code path. Keep `pkg/io/socket.go` (UDPConn-based) as the default.
2. **New file:** `pkg/io/socket_linux_hwts.go` implementing the same `Socket` interface as the default but using `golang.org/x/sys/unix.Socket` + `unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TIMESTAMPING, ...)`.
3. **Capability detection:** at engine startup, attempt to enable HW timestamping on each source-IP socket. If `SIOCGHWTSTAMP` reports the NIC doesn't support it, fall back to userspace silently and emit one INFO log line.
4. **Surface in summary.json:** extend `measurement_integrity.notes` with the actual mode used per source-IP. Operators see "hardware timestamping active on eth0; userspace fallback on eth1" and know what to trust.
5. **Tests:** unit-test the capability detection (with mock socket); integration test only runs on capable hardware (gate with environment variable or build tag).
6. **Docs update:** PRODUCTION-DEPLOYMENT.md gains a "HW timestamping" subsection with a list of known-good NICs and the kernel options required.

## Related

- [`docs/RESULTS-INTERPRETATION.md`](../RESULTS-INTERPRETATION.md) §8 — the operator-facing measurement-floor explanation
- [`docs/PRODUCTION-DEPLOYMENT.md`](../PRODUCTION-DEPLOYMENT.md) §2 — NIC tuning and offloading discussion
- [`pkg/io/`](../../pkg/io/) — the I/O layer that would gain the new build path
