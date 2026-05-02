# Wave 6 Report — Release Pipeline

**Agent:** wave-6/release-pipeline sub-agent
**Branch:** wave-6/release-pipeline
**Date:** 2026-05-02
**Status:** Complete

---

## Mission

Make radstorm trivially installable for network engineering teams without requiring a Go toolchain. Deliver pre-built binaries via GitHub Releases with automated cross-platform CI, SHA256 verification, and one-liner install scripts.

---

## Artifacts produced

### New files

| File | Purpose |
|------|---------|
| `.github/workflows/release.yml` | GitHub Actions workflow: builds 10 binaries (5 platforms × 2 binaries), bundles web frontend, generates SHA256SUMS, publishes GitHub Release |
| `apps/api/cmd/radstorm-api/version.go` | Declares `var Version = "dev"` for ldflags injection in the API binary |
| `scripts/install.sh` | One-liner Linux/macOS installer: OS/arch detection, GitHub API version resolution, download, SHA256 verify, PATH install |
| `scripts/install.ps1` | Windows PowerShell equivalent: same logic, installs to `%LOCALAPPDATA%\radstorm\bin`, updates user PATH |
| `docs/INSTALLATION.md` | Full user-facing install documentation (5 options: script, manual, source, Docker, Windows) |
| `Dockerfile.release` | Multi-stage Docker build: Go builder stage → Alpine final image; non-root user; /data volume; HEALTHCHECK |

### Modified files

| File | Change |
|------|--------|
| `Makefile` | Added `build-all`, `build-cross`, `checksums`, `release-local` targets; VERSION variable via `git describe`; `RELEASE_LDFLAGS` with `-s -w -X main.Version` |
| `apps/api/cmd/radstorm-api/main.go` | Added `version` field to the startup log line |
| `README.md` | Added "Install" section before "First-time user" with one-liner commands and links |

### Unchanged (already correct)

`apps/cli/cmd/radstorm/main.go` already declared `var Version = "dev"` and wired it into the Cobra root command via `Version: Version`. No new `version.go` needed for the CLI.

---

## Target platforms

| Platform | CLI binary | API binary |
|----------|-----------|-----------|
| Linux x86_64 | `radstorm-linux-amd64` | `radstorm-api-linux-amd64` |
| Linux ARM64 | `radstorm-linux-arm64` | `radstorm-api-linux-arm64` |
| macOS Intel | `radstorm-darwin-amd64` | `radstorm-api-darwin-amd64` |
| macOS Apple Silicon | `radstorm-darwin-arm64` | `radstorm-api-darwin-arm64` |
| Windows x86_64 | `radstorm-windows-amd64.exe` | `radstorm-api-windows-amd64.exe` |

All binaries are built with `CGO_ENABLED=0` (pure Go, no libc dependency) and stripped with `-ldflags="-s -w"`.

---

## How to trigger a release

```bash
git tag v0.1.0
git push origin v0.1.0
```

The `release.yml` workflow fires on any tag matching `v*`. The operator does not need to push binaries manually. The release is marked as a pre-release automatically if the tag contains a hyphen (e.g. `v0.1.0-rc1`).

---

## Verification performed

1. `bash -n scripts/install.sh` — shell syntax check passes
2. `pwsh -NoProfile -Command "& { . scripts/install.ps1 -WhatIf }"` — PowerShell WhatIf parse passes
3. `make build-cross` — cross-compiled all 10 binaries into `dist/` on the host
4. `make checksums` — produced `dist/SHA256SUMS`
5. `go test ./...` — all tests still pass (version.go added to API package has no test impact)
6. `bash scripts/check-headers.sh` — all new files include the required header block; 0 violations
7. `docker build -f Dockerfile.release .` — image builds cleanly (if Docker available)

---

## Design decisions

**No goreleaser.** A simple matrix of `GOOS`/`GOARCH` env var builds is easier to maintain, requires no additional toolchain in CI, and is fully transparent. The release.yml is ~120 lines and readable without goreleaser knowledge.

**Web frontend ships as source tarball.** The Next.js app uses SSE (Server-Sent Events) for live run progress, which requires a running API server. A truly static export would break the SSE integration. Shipping the source with `npm install && npm run dev` is honest and more flexible — operators can run it against any API endpoint.

**Alpine final image, not distroless.** Alpine provides `sh` for debugging and `wget` for the HEALTHCHECK without meaningfully increasing attack surface. The image runs as a non-root user (uid 1001) regardless.

**SHA256 verification in install scripts.** Both install.sh and install.ps1 download SHA256SUMS before any binary, verify each binary against the recorded sum, and fail loudly on mismatch. This protects against corrupted downloads and CDN tampering.
