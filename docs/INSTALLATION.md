# radstorm Installation

<!-- Purpose: End-user installation guide for radstorm and radstorm-api binaries. Covers one-liner scripts, manual download, source builds, Docker, checksum verification, and uninstallation. -->

This page covers all installation paths for `radstorm` and `radstorm-api`.

---

## Option A: One-liner install script (Linux and macOS)

The fastest path for network engineers on Linux or macOS.

```bash
curl -fsSL https://raw.githubusercontent.com/Apextech-sys/radstorm/main/scripts/install.sh | bash
```

What the script does:

1. Detects your OS and CPU architecture (`linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`)
2. Fetches the latest release tag from the GitHub API
3. Downloads `radstorm` and `radstorm-api` for your platform
4. Verifies SHA256 checksums before writing anything
5. Installs to `/usr/local/bin/` (when run as root) or `$HOME/.local/bin/` (non-root)

To install a specific version:

```bash
RADSTORM_VERSION=v0.1.0 curl -fsSL \
  https://raw.githubusercontent.com/Apextech-sys/radstorm/main/scripts/install.sh | bash
```

To install to a custom directory:

```bash
RADSTORM_INSTALL_DIR=/opt/radstorm/bin curl -fsSL \
  https://raw.githubusercontent.com/Apextech-sys/radstorm/main/scripts/install.sh | bash
```

---

## Option B: One-liner install script (Windows PowerShell)

```powershell
iwr -useb https://raw.githubusercontent.com/Apextech-sys/radstorm/main/scripts/install.ps1 | iex
```

Or run the script directly from a cloned repo:

```powershell
.\scripts\install.ps1
```

To install a specific version:

```powershell
$env:RADSTORM_VERSION = "v0.1.0"
.\scripts\install.ps1
```

Binaries are placed in `%LOCALAPPDATA%\radstorm\bin\`. The installer adds this directory to your user PATH automatically.

---

## Option C: Manual download from GitHub Releases

1. Go to [https://github.com/Apextech-sys/radstorm/releases](https://github.com/Apextech-sys/radstorm/releases)
2. Download the binaries for your platform:

| File | Platform |
|------|----------|
| `radstorm-linux-amd64` | Linux x86_64 |
| `radstorm-linux-arm64` | Linux ARM64 (AWS Graviton, Ampere, etc.) |
| `radstorm-darwin-amd64` | macOS Intel |
| `radstorm-darwin-arm64` | macOS Apple Silicon (M1/M2/M3/M4) |
| `radstorm-windows-amd64.exe` | Windows x86_64 |
| `radstorm-api-linux-amd64` | API server — Linux x86_64 |
| `radstorm-api-linux-arm64` | API server — Linux ARM64 |
| `radstorm-api-darwin-amd64` | API server — macOS Intel |
| `radstorm-api-darwin-arm64` | API server — macOS Apple Silicon |
| `radstorm-api-windows-amd64.exe` | API server — Windows x86_64 |
| `radstorm-web-static.tar.gz` | Web frontend source (all platforms) |
| `SHA256SUMS` | Checksums for all files above |

3. Verify checksums (Linux/macOS):

```bash
# Download the binary and the checksum file into the same directory, then:
sha256sum --check --ignore-missing SHA256SUMS
```

On macOS:

```bash
shasum -a 256 -c SHA256SUMS --ignore-missing 2>/dev/null
```

4. Make the binary executable and move it to your PATH:

```bash
chmod +x radstorm-linux-amd64
sudo mv radstorm-linux-amd64 /usr/local/bin/radstorm
```

---

## Option D: Build from source

Requires Go 1.22+ (tested on 1.26.2) and Git.

```bash
git clone https://github.com/Apextech-sys/radstorm.git
cd radstorm
make build
# Binaries land in bin/radstorm and bin/radstorm-api
```

See [docs/QUICKSTART.md](QUICKSTART.md) for the full 5-minute walkthrough.

### Cross-compile for all platforms

```bash
make build-cross      # builds to dist/ for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64
make checksums        # generates dist/SHA256SUMS
make release-local    # both of the above + a summary listing
```

---

## Option E: Docker

The `Dockerfile.release` at the repo root runs the API server in a minimal container. It builds the binaries from source in a builder stage and produces a small final image using Alpine.

```bash
# Build the image
docker build -f Dockerfile.release -t radstorm-api:latest .

# Run the API server (exposes port 8080)
docker run -d \
  -p 8080:8080 \
  -e RADSTORM_DATA_DIR=/data \
  -v $(pwd)/data:/data \
  --name radstorm-api \
  radstorm-api:latest
```

Environment variables accepted by the container:

| Variable | Default | Description |
|----------|---------|-------------|
| `RADSTORM_API_ADDR` | `:8080` | TCP bind address for the HTTP API |
| `RADSTORM_DATA_DIR` | `/data` | Directory for SQLite database and run output |
| `RADSTORM_CLI_BIN` | auto | Absolute path to the radstorm CLI binary (set automatically inside the image) |

The CLI binary is available inside the container at `/usr/local/bin/radstorm`. To run a scenario directly:

```bash
docker exec radstorm-api \
  radstorm run-scenario \
    --config /data/scenarios/my-scenario.toml \
    --out /data/results/run-1
```

---

## Verifying the installation

After any install method:

```bash
radstorm --version
# Should print: radstorm v0.1.0 (or the installed version)

radstorm-api --version
# Should print: radstorm-api v0.1.0
```

Run a quick validation (no network access needed):

```bash
radstorm validate-config --config /path/to/your-scenario.toml
```

---

## The web frontend

The `radstorm-web-static.tar.gz` release asset contains the Next.js frontend source. To run it:

```bash
tar -xzf radstorm-web-static.tar.gz
cd apps/web
npm install
npm run dev
# Open http://localhost:3000
```

The frontend connects to the API server at `http://localhost:8080` by default. Ensure `radstorm-api` is running first.

---

## Uninstalling

### Linux/macOS (one-liner install)

```bash
# Non-root install:
rm -f "$HOME/.local/bin/radstorm" "$HOME/.local/bin/radstorm-api"

# Root install:
sudo rm -f /usr/local/bin/radstorm /usr/local/bin/radstorm-api
```

### Windows

```powershell
Remove-Item -Path "$env:LOCALAPPDATA\radstorm\bin\radstorm.exe",
                  "$env:LOCALAPPDATA\radstorm\bin\radstorm-api.exe"
```

Then remove `%LOCALAPPDATA%\radstorm\bin` from your user PATH in System Properties > Environment Variables.

### Docker

```bash
docker rm -f radstorm-api
docker rmi radstorm-api:latest
```
