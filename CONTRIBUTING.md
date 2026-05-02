<!-- Purpose: contributor guidelines for radstorm; what's in scope, how to propose changes, how to run tests, what we will and won't merge. -->

# Contributing to radstorm

radstorm is a focused tool: a protocol-level RADIUS load tester for ISP-scale subscriber simulation. We welcome contributions that align with that focus, and we'll politely decline ones that don't.

## In scope

- New scenario types (e.g. session-churn, mixed PAP/CHAP/EAP-MD5 patterns)
- Additional vendor VSA dictionaries (Cisco, Juniper, Nokia, etc.)
- Performance improvements to the I/O layer, collector, or scenario driver
- Better operator UX in the frontend
- More E2E test coverage
- Documentation improvements
- Bug fixes with reproduction steps and a regression test

## Out of scope (we'll close politely)

- General RADIUS server features (this is a tester, not a server)
- EAP, RadSec, DTLS support (no roadmap for these)
- Hosted SaaS features (auth, multi-tenancy, billing, etc.)
- Major architectural rewrites without prior discussion
- Vendor advocacy (we don't endorse Interstellar over FreeRADIUS or vice versa — radstorm produces measurements, operators decide)

## Before you start

For anything beyond a typo or one-line bug fix, **open an issue first** describing the problem and proposed approach. This avoids wasted work on PRs we can't accept.

## Development setup

See [docs/QUICKSTART.md](docs/QUICKSTART.md) for the 5-minute build path.

You'll need:
- Go 1.22+
- Node 20+ (only for the frontend)
- Docker (only for the local FreeRADIUS test rig)
- jq (for E2E assertions)

## Project conventions

Every code file in this repo starts with a header comment block documenting purpose, related files, and contract ownership. See [docs/CONVENTIONS.md](docs/CONVENTIONS.md). The CI gate enforces this — your PR will fail if you add a code file without a header.

For Go: gofmt + standard lint. For TypeScript: strict mode, ESLint rules from the existing config. Tests required for any non-trivial logic.

## Pull request checklist

Before opening a PR:

- [ ] `make build` succeeds
- [ ] `go test ./...` passes
- [ ] `cd apps/web && npm test -- --run` passes (if you touched the frontend)
- [ ] `bash scripts/check-headers.sh` clean
- [ ] `bash test/e2e/run.sh` passes against the local Docker rig (if you touched anything in the runtime path)
- [ ] You've updated relevant docs (RUNBOOK, CONFIG, contracts) if behaviour changed
- [ ] Commit messages follow conventional commits (`feat:`, `fix:`, `docs:`, `chore:`, etc.)

## Triage and review

This is a focused tool maintained on a best-effort basis. We aim to triage issues within a couple of weeks and merge straightforward PRs faster, but there's no SLA. If something is urgent for you, fork freely (Apache 2.0).

## Responsible use

radstorm is a **load testing tool**. Pointing it at a RADIUS server you don't own or have explicit permission to test is unauthorized access in most jurisdictions. Don't.

The tool's results are only as good as the test conditions. We don't validate vendor performance claims on anyone's behalf, and a `succeeded` outcome from radstorm is a measurement, not an endorsement.

## Code of conduct

Be kind, be technical, be specific. We don't have a long CoC document; the standard expectations apply.

## License

By contributing, you agree your contributions are licensed under the Apache License 2.0, the same license as radstorm itself. See [LICENSE](LICENSE).
