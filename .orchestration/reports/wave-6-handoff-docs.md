# Wave 6 — Handoff Documentation Report

**Branch:** wave-6/handoff-docs
**Date:** 2026-05-02
**Agent:** Claude Sonnet 4.6 (documentation sub-agent)

---

## Files authored (new)

| File | Lines | Summary |
|---|---|---|
| `docs/EVALUATION-GUIDE.md` | 399 | End-to-end evaluation workflow: pre-evaluation prep, 1k→1M test plan, per-scenario procedures, comparison instructions, decision matrix, acceptance criteria, and reporting guidance |
| `docs/PRODUCTION-DEPLOYMENT.md` | 514 | Linux production deployment: hardware requirements per scale tier, OS packages, binary install, sysctl tuning, source IP alias setup (ip and ifconfig), firewall rules, file descriptor limits, systemd service unit file, log rotation, backup, and upgrade procedure |
| `docs/CUSTOM-SCENARIOS.md` | 512 | Custom scenario authoring: full annotated TOML field reference, scenario type selection guide, cold_start μ/σ sizing, source IP capacity math, NAS attribute configuration, Huawei VSA setup, threshold customization, validate-config usage with error examples, four worked examples |
| `docs/RESULTS-INTERPRETATION.md` | 386 | Results interpretation: annotated healthy vs concerning summary.json examples, establishment curve patterns, latency distribution guidance, retransmit analysis, CoA latency, unresponsive_periods, threshold rollup, 5 DuckDB queries for subscribers.parquet, 3 DuckDB queries for events.parquet shards |
| `docs/COMPARISON-WORKFLOW.md` | 261 | Comparison workflow: naming conventions, matched campaign procedure, compare-runs.sh usage, stakeholder report template |
| `docs/GLOSSARY.md` | 94 | 25 definitions covering BNG, NAS, CoA, Disconnect, VSA, AVP, PAP, CHAP, Identifier, Authenticator types, Acct-Status-Type, Acct-Session-Id, Framed-IP-Address, cold start, pessimal, establishment, in-flight, drain, p50/p99/p999, tail, cascade failure, source IP alias, ephemeral port, 4-tuple, sharded collector, Parquet |
| `scripts/compare-runs.sh` | 247 | Working bash script: compares two run directories via jq, prints markdown delta table with per-metric winner column; prints usage when called with no args |

**Total new lines:** 2,413

---

## Files modified (existing)

| File | Summary of changes |
|---|---|
| `docs/TROUBLESHOOTING.md` | Added "Production failure modes" section with 8 new entries: identifier exhaustion capacity math, source IP alias verification, port range exhaustion, kernel UDP buffer drops (netstat -su detection), file descriptor limits, conntrack table fills (with NOTRACK fix), bad Response Authenticator under LB fleet, CPU saturation detection, and still_in_flight_at_end drain timeout fix |
| `docs/QUICKSTART.md` | Added "Next: real evaluation work → EVALUATION-GUIDE.md" link at end |
| `docs/RUNBOOK.md` | Added cross-link block at top of table of contents pointing to all 6 new docs; added inline cross-links in Production deployment and Output artifacts sections |
| `README.md` | Added "For network engineers" callout near the top; expanded Documentation table with all 6 new docs |

---

## Verification

- `bash scripts/check-headers.sh`: 148 files checked, 0 violations
- `bash scripts/compare-runs.sh` (no args): prints usage, exits 1
- All cross-links verified against actual file paths in the repo
- All CLI commands verified against `apps/cli/cmd/radstorm/*.go` source

---

## Gaps and callouts

1. **Custom threshold TOML config:** The `[thresholds]` block in CUSTOM-SCENARIOS.md documents the intended future API and provides a jq workaround. As of the current codebase, thresholds are hardcoded in the collector — the TOML block does not yet exist. This is documented honestly in the file.

2. **1M scale validation:** All 1M-scale guidance (hardware requirements, sysctl values, source IP counts) is based on the architecture's design assumptions and extrapolated from the 100-subscriber validated run. The doc is explicit that these are "architectural targets" requiring on-hardware validation before drawing conclusions.

3. **Interstellar-specific VSAs:** If Interstellar uses VSA dictionaries beyond Huawei vendor 2011, those would need to be added to `pkg/radius/dictionaries/` and the CUSTOM-SCENARIOS.md Huawei section would need extension. The current codebase has only the Huawei dictionary.

4. **GitHub Releases URL:** PRODUCTION-DEPLOYMENT.md points to `https://github.com/Apextech-sys/reflex-radstorm/releases` which does not yet have published releases. The section notes this and provides the build-from-source fallback.
