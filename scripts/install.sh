#!/usr/bin/env bash
# radstorm one-liner installer for Linux and macOS.
#
# Purpose:
#   Detects the current OS and CPU architecture, fetches the latest release
#   tag from the GitHub API, downloads the matching radstorm and radstorm-api
#   binaries, verifies SHA256 checksums, and installs them into either
#   /usr/local/bin (when running as root) or $HOME/.local/bin (non-root).
#   Prints next-step instructions on success.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Apextech-sys/reflex-radstorm/main/scripts/install.sh | bash
#   Or with a specific version:
#   RADSTORM_VERSION=v0.1.0 bash scripts/install.sh
#
# Related:
#   - scripts/install.ps1  (Windows equivalent)
#   - docs/INSTALLATION.md (full installation documentation)
#   - .github/workflows/release.yml (builds and publishes the binaries)
#
# Briefing: .orchestration/briefings/wave-6-release-pipeline.md
#
# Contract:
#   Exit 0 on success. Exit 1 on any error (with a clear message to stderr).
#   Env vars:
#     RADSTORM_VERSION  — pin a specific tag (e.g. v0.1.0); default: latest
#     RADSTORM_INSTALL_DIR — override install directory

set -euo pipefail

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
REPO="Apextech-sys/reflex-radstorm"
GITHUB_API="https://api.github.com/repos/${REPO}/releases/latest"
GITHUB_RELEASES="https://github.com/${REPO}/releases/download"
QUICKSTART_URL="https://github.com/${REPO}/blob/main/docs/QUICKSTART.md"
BINARIES=("radstorm" "radstorm-api")

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
info()  { printf '\033[1;32m[radstorm-install]\033[0m %s\n' "$*"; }
warn()  { printf '\033[1;33m[radstorm-install] WARNING:\033[0m %s\n' "$*" >&2; }
error() { printf '\033[1;31m[radstorm-install] ERROR:\033[0m %s\n' "$*" >&2; exit 1; }

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || error "Required command not found: $1. Install it and retry."
}

# ---------------------------------------------------------------------------
# Dependency check
# ---------------------------------------------------------------------------
require_cmd curl
require_cmd sha256sum 2>/dev/null || require_cmd shasum   # macOS uses shasum

# ---------------------------------------------------------------------------
# Detect OS
# ---------------------------------------------------------------------------
OS="$(uname -s)"
case "${OS}" in
  Linux)   GOOS="linux"  ;;
  Darwin)  GOOS="darwin" ;;
  *)       error "Unsupported OS: ${OS}. Use scripts/install.ps1 on Windows." ;;
esac

# ---------------------------------------------------------------------------
# Detect architecture
# ---------------------------------------------------------------------------
ARCH="$(uname -m)"
case "${ARCH}" in
  x86_64 | amd64)  GOARCH="amd64" ;;
  aarch64 | arm64) GOARCH="arm64" ;;
  *)                error "Unsupported architecture: ${ARCH}." ;;
esac

info "Detected platform: ${GOOS}/${GOARCH}"

# ---------------------------------------------------------------------------
# Resolve version (latest or pinned)
# ---------------------------------------------------------------------------
if [ -n "${RADSTORM_VERSION:-}" ]; then
  VERSION="${RADSTORM_VERSION}"
  info "Using pinned version: ${VERSION}"
else
  info "Fetching latest release from GitHub..."
  VERSION="$(curl -fsSL "${GITHUB_API}" | grep '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
  [ -n "${VERSION}" ] || error "Could not determine latest release. Check your internet connection."
  info "Latest release: ${VERSION}"
fi

# ---------------------------------------------------------------------------
# Determine install directory
# ---------------------------------------------------------------------------
if [ -n "${RADSTORM_INSTALL_DIR:-}" ]; then
  INSTALL_DIR="${RADSTORM_INSTALL_DIR}"
elif [ "$(id -u)" -eq 0 ]; then
  INSTALL_DIR="/usr/local/bin"
else
  INSTALL_DIR="${HOME}/.local/bin"
fi

info "Install directory: ${INSTALL_DIR}"
mkdir -p "${INSTALL_DIR}"

# ---------------------------------------------------------------------------
# Portable sha256 check: Linux uses sha256sum, macOS uses shasum -a 256
# ---------------------------------------------------------------------------
sha256_check() {
  local file="$1"
  local expected="$2"
  local actual
  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "${file}" | awk '{print $1}')"
  else
    actual="$(shasum -a 256 "${file}" | awk '{print $1}')"
  fi
  if [ "${actual}" != "${expected}" ]; then
    error "Checksum mismatch for ${file}!
  Expected: ${expected}
  Got:      ${actual}
  The download may be corrupted or tampered with."
  fi
}

# ---------------------------------------------------------------------------
# Download, verify, and install
# ---------------------------------------------------------------------------
TMPDIR_WORK="$(mktemp -d)"
trap 'rm -rf "${TMPDIR_WORK}"' EXIT

# Fetch the SHA256SUMS file first
CHECKSUMS_URL="${GITHUB_RELEASES}/${VERSION}/SHA256SUMS"
info "Downloading checksums..."
curl -fsSL -o "${TMPDIR_WORK}/SHA256SUMS" "${CHECKSUMS_URL}" \
  || error "Failed to download SHA256SUMS from ${CHECKSUMS_URL}"

for BIN in "${BINARIES[@]}"; do
  ARTIFACT="${BIN}-${GOOS}-${GOARCH}"
  DOWNLOAD_URL="${GITHUB_RELEASES}/${VERSION}/${ARTIFACT}"
  DEST="${INSTALL_DIR}/${BIN}"

  info "Downloading ${ARTIFACT}..."
  curl -fsSL -o "${TMPDIR_WORK}/${ARTIFACT}" "${DOWNLOAD_URL}" \
    || error "Failed to download ${ARTIFACT} from ${DOWNLOAD_URL}"

  # Extract expected checksum for this artifact
  EXPECTED_SUM="$(grep "${ARTIFACT}" "${TMPDIR_WORK}/SHA256SUMS" | awk '{print $1}')"
  if [ -z "${EXPECTED_SUM}" ]; then
    warn "No checksum entry found for ${ARTIFACT} in SHA256SUMS. Skipping verification."
  else
    info "Verifying checksum for ${ARTIFACT}..."
    sha256_check "${TMPDIR_WORK}/${ARTIFACT}" "${EXPECTED_SUM}"
    info "Checksum OK."
  fi

  # Install
  install -m 0755 "${TMPDIR_WORK}/${ARTIFACT}" "${DEST}"
  info "Installed: ${DEST}"
done

# ---------------------------------------------------------------------------
# PATH reminder for non-root installs
# ---------------------------------------------------------------------------
if [ "${INSTALL_DIR}" = "${HOME}/.local/bin" ]; then
  if ! echo "${PATH}" | grep -q "${HOME}/.local/bin"; then
    warn "${INSTALL_DIR} is not in your PATH."
    warn "Add the following to your shell profile (~/.bashrc, ~/.zshrc, etc.):"
    warn "  export PATH=\"\$HOME/.local/bin:\$PATH\""
  fi
fi

# ---------------------------------------------------------------------------
# Verify installation
# ---------------------------------------------------------------------------
info "Verifying installation..."
if command -v radstorm >/dev/null 2>&1; then
  INSTALLED_VERSION="$(radstorm --version 2>&1 || true)"
  info "radstorm version: ${INSTALLED_VERSION}"
else
  info "radstorm installed at ${INSTALL_DIR}/radstorm"
  info "Run: ${INSTALL_DIR}/radstorm --version"
fi

# ---------------------------------------------------------------------------
# Next steps
# ---------------------------------------------------------------------------
cat <<EOF

Installation complete. radstorm ${VERSION} is ready.

Next steps:
  1. Start a FreeRADIUS test rig:
       docker compose -f test/docker/docker-compose.yml up -d
     (Or target your own RADIUS server — see docs/CONFIG.md)

  2. Run your first test:
       radstorm run-scenario \\
         --config your-scenario.toml \\
         --out results/my-first-run

  3. Inspect results:
       radstorm analyze-results results/my-first-run

  4. Full documentation:
       ${QUICKSTART_URL}

To uninstall:
  rm -f ${INSTALL_DIR}/radstorm ${INSTALL_DIR}/radstorm-api

EOF
