<!-- Purpose: Linux production deployment guide for running radstorm on dedicated hardware at ISP scale. -->

# radstorm Production Deployment

Audience: operator deploying radstorm on a dedicated Linux host to run evaluation or production stress tests. Assumes Ubuntu 22.04 LTS or RHEL 9. Covers everything from bare OS to a running test.

The CLI binary runs standalone. No Docker, no Node.js, no database required on the test host for CLI-only runs. The API server and frontend are optional; they add a web UI but do not change what the CLI does.

---

## Table of contents

1. [Hardware requirements](#1-hardware-requirements)
2. [OS packages](#2-os-packages)
3. [Getting the binary](#3-getting-the-binary)
4. [Sysctl tuning](#4-sysctl-tuning)
5. [Source IP alias setup](#5-source-ip-alias-setup)
6. [Firewall considerations](#6-firewall-considerations)
7. [File descriptor limits](#7-file-descriptor-limits)
8. [Running as a systemd service (API server)](#8-running-as-a-systemd-service)
9. [Log rotation](#9-log-rotation)
10. [Backup](#10-backup)
11. [Upgrade procedure](#11-upgrade-procedure)

---

## 1. Hardware requirements

| Scale tier | Subscribers | CPU | RAM | Disk (results) | NIC |
|---|---|---|---|---|---|
| 1k | 1,000 | 2 cores | 4 GB | 2 GB | 1 GbE |
| 10k | 10,000 | 4 cores | 8 GB | 10 GB | 1 GbE |
| 100k | 100,000 | 8 cores | 16 GB | 50 GB | 10 GbE |
| 1M | 1,000,000 | 16+ cores | 64 GB | 500 GB | 10 GbE |

**Notes:**

- CPU: The sharded event collector and socket pool scale with core count. Fewer cores create a bottleneck in the collector before the RADIUS server becomes the bottleneck.
- RAM: At 1M subscribers, goroutine stacks alone consume ~2 GB. The Parquet write buffers add another 1–2 GB peak. 64 GB has headroom; 32 GB is a hard minimum for 1M.
- Disk: Parquet files at 1M subscribers produce ~300–500 GB of event shards. Use a local NVMe or fast SAS; network-attached storage will not keep up with the flush rate.
- NIC: At 1M subscribers in a cold-start scenario, outbound UDP throughput peaks at several hundred Mbps. A 1 GbE NIC is the ceiling for that tier — use 10 GbE for 100k+.

The 1M scale tier is the **architectural target** validated by design. Verify on your specific hardware before treating 1M numbers as definitive.

---

## 2. OS packages

### Ubuntu 22.04

```bash
sudo apt-get update
sudo apt-get install -y \
  curl \
  jq \
  iproute2 \
  net-tools \
  duckdb \
  logrotate
```

`duckdb` is not in the standard Ubuntu repositories. Install from the DuckDB releases page:

```bash
curl -L https://github.com/duckdb/duckdb/releases/latest/download/duckdb_cli-linux-amd64.zip \
  -o /tmp/duckdb.zip
unzip /tmp/duckdb.zip -d /usr/local/bin/
chmod +x /usr/local/bin/duckdb
duckdb --version
```

### RHEL 9

```bash
sudo dnf install -y \
  curl \
  jq \
  iproute \
  net-tools \
  logrotate

# DuckDB: same curl install as Ubuntu above
```

### Optional — build from source

If you need to build from source (not typical for evaluation runs — use the pre-built binary):

```bash
# Ubuntu only
sudo apt-get install -y build-essential golang-go

# Or install Go from go.dev if the distro version is too old (need 1.22+)
curl -L https://go.dev/dl/go1.22.0.linux-amd64.tar.gz | sudo tar -C /usr/local -xz
export PATH=$PATH:/usr/local/go/bin
```

---

## 3. Getting the binary

### From GitHub Releases (recommended)

Once GitHub Releases are published, download the pre-built Linux amd64 binary:

```bash
# Check https://github.com/Apextech-sys/radstorm/releases for the latest version
VERSION=v0.1.0
curl -L https://github.com/Apextech-sys/radstorm/releases/download/${VERSION}/radstorm-linux-amd64.tar.gz \
  -o /tmp/radstorm.tar.gz
tar -xzf /tmp/radstorm.tar.gz -C /tmp/
sudo install -m 755 /tmp/radstorm /usr/local/bin/radstorm
sudo install -m 755 /tmp/radstorm-api /usr/local/bin/radstorm-api

# Verify
radstorm --version
```

The binaries are statically linked — no runtime dependencies. Copy them anywhere in `PATH`.

### From source (if releases are not yet published)

```bash
git clone https://github.com/Apextech-sys/radstorm.git
cd radstorm
make build

# Copy to production path
sudo install -m 755 bin/radstorm /usr/local/bin/radstorm
sudo install -m 755 bin/radstorm-api /usr/local/bin/radstorm-api
```

---

## 4. Sysctl tuning

Apply these settings before any test at 10k subscribers or above. They are safe to apply permanently.

```bash
# Ephemeral port range: widen for large tests
# Default is typically 32768-60999 (~28k ports). Widening to 10000-65000 gives ~55k.
sudo sysctl -w net.ipv4.ip_local_port_range="10000 65000"

# UDP receive and send buffers: allow OS to buffer large bursts
sudo sysctl -w net.core.rmem_max=134217728
sudo sysctl -w net.core.wmem_max=134217728
sudo sysctl -w net.core.rmem_default=33554432
sudo sysctl -w net.core.wmem_default=33554432

# Socket backlog (for the CoA listener)
sudo sysctl -w net.core.netdev_max_backlog=65536

# Make persistent across reboots
cat <<'EOF' | sudo tee /etc/sysctl.d/99-radstorm.conf
net.ipv4.ip_local_port_range = 10000 65000
net.core.rmem_max = 134217728
net.core.wmem_max = 134217728
net.core.rmem_default = 33554432
net.core.wmem_default = 33554432
net.core.netdev_max_backlog = 65536
EOF

sudo sysctl -p /etc/sysctl.d/99-radstorm.conf
```

Verify:

```bash
sysctl net.ipv4.ip_local_port_range net.core.rmem_max
```

---

## 5. Source IP alias setup

RADIUS Identifier is 8 bits: 256 unique IDs per `(srcIP, srcPort, dstIP, dstPort)` tuple. To support large concurrent inflight counts, you need multiple source IPs.

Capacity calculation:

```
total_capacity = num_ips × num_ports × 256

Example: 8 IPs, port_range [10000, 20000] (10,000 ports each)
= 8 × 10,000 × 256 = 20,480,000 maximum inflight IDs
```

For realistic cold_start at 1M with ~10% concurrency: 8 IPs with a 10k-port range is a comfortable floor.

### Using `ip` (modern, preferred)

```bash
# Assign aliases on eth0 (replace with your interface name)
IFACE=eth0
BASE_IP=192.168.100

for i in $(seq 1 8); do
    sudo ip addr add ${BASE_IP}.${i}/24 dev ${IFACE} label ${IFACE}:${i}
done

# Verify
ip addr show ${IFACE} | grep ${BASE_IP}
```

To make persistent (Ubuntu/Netplan):

```yaml
# /etc/netplan/99-radstorm-aliases.yaml
network:
  version: 2
  ethernets:
    eth0:
      addresses:
        - 192.168.100.1/24
        - 192.168.100.2/24
        - 192.168.100.3/24
        - 192.168.100.4/24
        - 192.168.100.5/24
        - 192.168.100.6/24
        - 192.168.100.7/24
        - 192.168.100.8/24
```

```bash
sudo netplan apply
```

### Using `ifconfig` (legacy)

```bash
IFACE=eth0
BASE_IP=192.168.100

for i in $(seq 1 8); do
    sudo ifconfig ${IFACE}:${i} ${BASE_IP}.${i} netmask 255.255.255.0 up
done

# Verify
ifconfig | grep ${BASE_IP}
```

### Scenario config

After adding aliases, update your TOML config to list all source IPs:

```toml
[source]
ips = [
  "192.168.100.1",
  "192.168.100.2",
  "192.168.100.3",
  "192.168.100.4",
  "192.168.100.5",
  "192.168.100.6",
  "192.168.100.7",
  "192.168.100.8",
]
port_range = [10000, 20000]
```

**Aliases do not survive a reboot unless made persistent.** Always verify with `ip addr show` before starting a test campaign.

---

## 6. Firewall considerations

### Outbound (radstorm → RADIUS server)

radstorm sends UDP from the source IPs and port range configured in `[source]`. The RADIUS server replies to the same tuple. Ensure:

- Outbound UDP from the test host to `target.auth_address` and `target.acct_address` is unrestricted
- Return UDP (server replies) is allowed back to all source IPs on all ports in `source.port_range`

If using `iptables`:

```bash
# Allow outbound UDP to RADIUS server on auth + acct ports
sudo iptables -A OUTPUT -p udp -d 10.0.0.10 --dport 1812 -j ACCEPT
sudo iptables -A OUTPUT -p udp -d 10.0.0.10 --dport 1813 -j ACCEPT

# Allow replies back
sudo iptables -A INPUT -p udp -s 10.0.0.10 --sport 1812 -j ACCEPT
sudo iptables -A INPUT -p udp -s 10.0.0.10 --sport 1813 -j ACCEPT
```

### Inbound (RADIUS server → radstorm CoA listener)

radstorm's CoA listener binds to `coa_listener.bind_address` (default `0.0.0.0:3799`). The RADIUS server sends CoA-Request and Disconnect-Request packets to this port. Ensure:

- Inbound UDP on port 3799 (or whatever `coa_listener.bind_address` specifies) from the RADIUS server's IP is allowed

```bash
sudo iptables -A INPUT -p udp -s 10.0.0.10 --dport 3799 -j ACCEPT
```

### conntrack (if iptables/nftables in path with connection tracking)

At large scale, connection tracking can fill the conntrack table. Since this is UDP (stateless from conntrack's perspective), disable tracking for the RADIUS flows:

```bash
# Check current conntrack table size
cat /proc/sys/net/netfilter/nf_conntrack_max

# Increase if needed (1M subscribers → increase substantially)
sudo sysctl -w net.netfilter.nf_conntrack_max=2000000

# Or disable conntrack for the RADIUS port range entirely
sudo iptables -t raw -A PREROUTING -p udp --dport 1812 -j NOTRACK
sudo iptables -t raw -A PREROUTING -p udp --dport 1813 -j NOTRACK
sudo iptables -t raw -A OUTPUT -p udp --dport 1812 -j NOTRACK
sudo iptables -t raw -A OUTPUT -p udp --dport 1813 -j NOTRACK
```

See [docs/TROUBLESHOOTING.md](TROUBLESHOOTING.md#conntrack-table-fills) for detecting and diagnosing conntrack exhaustion.

---

## 7. File descriptor limits

Each bound UDP socket consumes one file descriptor. At large scale with many source IPs and ports, the default system limit (1024 per process) is too low.

### Check current limits

```bash
ulimit -n
# Default: 1024 — too low for large tests
```

### Raise for the current session

```bash
ulimit -n 1048576
```

### Raise permanently

```bash
# /etc/security/limits.conf (applies after next login or service restart)
cat <<'EOF' | sudo tee -a /etc/security/limits.conf
*    soft    nofile    1048576
*    hard    nofile    1048576
root soft    nofile    1048576
root hard    nofile    1048576
EOF
```

For a systemd service, set `LimitNOFILE` in the unit file (see section 8).

Verify after applying:

```bash
# After re-login or in a new shell
ulimit -n
# Should show 1048576
```

---

## 8. Running as a systemd service

The CLI is designed for interactive use on the test box. The API server (`radstorm-api`) is a long-running daemon that benefits from systemd management.

### Create the service user

```bash
sudo useradd -r -s /sbin/nologin -d /opt/radstorm radstorm
sudo mkdir -p /opt/radstorm/data
sudo chown radstorm:radstorm /opt/radstorm/data
```

### Create the unit file

```bash
sudo tee /etc/systemd/system/radstorm-api.service <<'EOF'
[Unit]
Description=radstorm HTTP API server
Documentation=https://github.com/Apextech-sys/radstorm
After=network.target
Wants=network.target

[Service]
Type=simple
User=radstorm
Group=radstorm
WorkingDirectory=/opt/radstorm

ExecStart=/usr/local/bin/radstorm-api

Environment=RADSTORM_API_ADDR=:8080
Environment=RADSTORM_DATA_DIR=/opt/radstorm/data
Environment=RADSTORM_CLI_BIN=/usr/local/bin/radstorm

# Restart on failure, but not if we exit cleanly
Restart=on-failure
RestartSec=5s

# File descriptor limit (needs to accommodate all source IP sockets)
LimitNOFILE=1048576

# Standard output goes to journald
StandardOutput=journal
StandardError=journal
SyslogIdentifier=radstorm-api

[Install]
WantedBy=multi-user.target
EOF
```

### Enable and start

```bash
sudo systemctl daemon-reload
sudo systemctl enable radstorm-api
sudo systemctl start radstorm-api

# Verify
sudo systemctl status radstorm-api
curl -s http://localhost:8080/api/v1/health
# Expected: {"status":"ok","version":"..."}
```

### View logs

```bash
sudo journalctl -u radstorm-api -f
```

---

## 9. Log rotation

Run output directories grow large. The API server's SQLite run store stays small. Rotate run artifacts on a schedule.

### logrotate config

```bash
sudo tee /etc/logrotate.d/radstorm <<'EOF'
/opt/radstorm/data/runs/*/run.log {
    daily
    rotate 14
    compress
    delaycompress
    missingok
    notifempty
    sharedscripts
}
EOF
```

For Parquet files and full run directories, use a cron job rather than logrotate (logrotate is not designed for directory trees):

```bash
# /etc/cron.d/radstorm-cleanup
# Delete run directories older than 30 days
0 3 * * * radstorm find /opt/radstorm/data/runs -maxdepth 1 -type d -mtime +30 -exec rm -rf {} \;
```

---

## 10. Backup

### SQLite run store

The run store at `$RADSTORM_DATA_DIR/runs.db` contains run metadata (IDs, status, timestamps). Back it up while the server is running using SQLite's online backup:

```bash
sqlite3 /opt/radstorm/data/runs.db "VACUUM INTO '/backup/runs-$(date +%Y%m%d).db'"
```

Or stop the server, copy the file, and restart:

```bash
sudo systemctl stop radstorm-api
cp /opt/radstorm/data/runs.db /backup/runs-$(date +%Y%m%d).db
sudo systemctl start radstorm-api
```

### Run output directories

Parquet files and summary.json are the primary evaluation artifacts. Back them up to object storage or a network share after each test campaign:

```bash
# Example: rsync to a remote archive host
rsync -avz --progress /opt/radstorm/data/runs/ archive-host:/data/radstorm-runs/
```

---

## 11. Upgrade procedure

Upgrades are a binary swap. No database migration is required between minor versions unless the release notes say otherwise.

```bash
# 1. Download the new binary
curl -L https://github.com/Apextech-sys/radstorm/releases/download/v0.2.0/radstorm-linux-amd64.tar.gz \
  -o /tmp/radstorm-new.tar.gz
tar -xzf /tmp/radstorm-new.tar.gz -C /tmp/

# 2. Stop the API server if running
sudo systemctl stop radstorm-api

# 3. Replace the binary
sudo install -m 755 /tmp/radstorm /usr/local/bin/radstorm
sudo install -m 755 /tmp/radstorm-api /usr/local/bin/radstorm-api

# 4. Verify version
radstorm --version

# 5. Restart the API server
sudo systemctl start radstorm-api
sudo systemctl status radstorm-api
```

The CLI binary is discovered by the API server at startup. No configuration change is needed if both binaries are in the same location.
