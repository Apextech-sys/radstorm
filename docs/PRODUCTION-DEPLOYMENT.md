<!-- Purpose: Linux production deployment guide for running radstorm on dedicated hardware at ISP scale. -->

# radstorm Production Deployment

Audience: operator deploying radstorm on a dedicated Linux host to run evaluation or production stress tests. Assumes Ubuntu 22.04 LTS or RHEL 9. Covers everything from bare OS to a running test.

The CLI binary runs standalone. No Docker, no Node.js, no database required on the test host for CLI-only runs. The API server and frontend are optional; they add a web UI but do not change what the CLI does.

---

## Table of contents

1. [Hardware requirements](#1-hardware-requirements)
2. [NIC tuning and offloading](#2-nic-tuning-and-offloading)
3. [OS packages](#3-os-packages)
4. [Getting the binary](#4-getting-the-binary)
5. [Sysctl tuning](#5-sysctl-tuning)
6. [Source IP alias setup](#6-source-ip-alias-setup)
7. [Firewall considerations](#7-firewall-considerations)
8. [File descriptor limits](#8-file-descriptor-limits)
9. [Running as a systemd service (API server)](#9-running-as-a-systemd-service)
10. [Log rotation](#10-log-rotation)
11. [Backup](#11-backup)
12. [Upgrade procedure](#12-upgrade-procedure)

---

## 1. Hardware requirements

radstorm sizing falls into four tiers based on subscriber count. Pick the tier that bounds your largest planned test.

### Quick reference

| Tier | Subscribers | CPU (physical) | RAM | NIC | Disk | Use case |
|---|---|---|---|---|---|---|
| 1 — Smoke / dev | ≤1,000 | 4 cores | 8 GB | loopback or 1 GbE | any SSD, ≥5 GB | Daily dev, CI, sanity checks |
| 2 — Single-server | 10,000–50,000 | 8 / 16t | 16 GB | 1 GbE (10 GbE preferred) | NVMe, ≥20 GB | Realistic mid-scale tests, integration |
| 3 — Carrier evaluation | 100,000–1,000,000 | 32 cores / 64t | 64 GB | 10 GbE minimum, **25 GbE recommended for 1M** | NVMe local, ≥500 GB | Vendor evaluation; ISP buy-decision validation |
| 4 — Beyond 1M | 3M+ | 64+ cores | 128+ GB | 100 GbE + DPDK/AF_XDP | NVMe RAID | Not currently supported (multi-host coordination needed) |

The 1M tier is the **architectural target** validated by design. Verify on your specific hardware before treating 1M numbers as definitive.

### Tier 1 — Smoke / dev (≤1k subscribers)

Anything modern. A laptop with 4 cores and 8 GB RAM is fine. The Docker FreeRADIUS rig and the `radstorm` binary will both run on the same box. This is the daily-development tier — useful for sanity checks, CI runs, and developing scenarios.

- Loopback only; no NIC tuning relevant
- Results per run: ~50 MB
- Real-world example: an Apple M2 MacBook Air or a 12th-gen Intel i5 desktop are both adequate

### Tier 2 — Realistic single-server tests (10k–50k subscribers)

Mid-range workstation or a small Linux VM. This is where the radstorm-and-target-server-on-different-boxes pattern starts mattering, but you can still get away with a single beefy server running both via Docker for cost.

- **CPU:** 8 cores / 16 threads (e.g. AMD EPYC 7313, Intel Xeon Silver 4310, or modern Ryzen). Fewer cores → the sharded collector becomes the bottleneck before the RADIUS server does.
- **RAM:** 16 GB. Goroutine stacks (~2 KB each × 50k subs ≈ 100 MB) plus Parquet write buffers (~2 GB peak) plus collector channels (~500 MB) plus runtime overhead.
- **NIC:** 1 GbE is the ceiling at this tier — outbound RADIUS for a 50k cold-start can briefly hit ~80–150 Mbps. 10 GbE is overkill but cheap insurance.
- **Disk:** local NVMe; ≥20 GB free. Parquet writes peak at ~50 MB/sec.
- **Source IPs:** 1–2 aliased IPs are sufficient.

### Tier 3 — Carrier-scale evaluation (100k–1M subscribers)

Dedicated Linux server. This is the tier you'd use to actually evaluate a vendor for an ISP buy decision. You want headroom, not bare minimums.

- **CPU:** 32 physical cores (64 vCPU with SMT). AMD EPYC 9354 (32C @ 3.25 GHz) or dual Xeon Gold 6338. The sharded collector creates one Parquet writer per CPU core; with 32 cores you have 32 parallel write streams. Fewer cores → write contention before RADIUS server saturation.
- **RAM:** 64 GB. At 1M subscribers: goroutine stacks ~2 GB, in-flight request tables ~200 MB, Parquet buffers ~3 GB peak (256 MB rotation per shard × N shards mid-flush), runtime overhead. **32 GB is the hard floor**; 64 GB has comfort.
- **NIC:** 10 GbE minimum at 100k. **25 GbE strongly recommended for 1M** — cold-start auth storm can spike to ~1.5 Gbps outbound for the ramp window. See §2 for tuning details.
- **Disk:** Local NVMe. A 1M cold-start produces ~300–500 GB of Parquet shards. Sequential write at peak hits ~500 MB/sec. **Network-attached storage (NFS, iSCSI) will not keep up** — flushes will block the collector and skew your latency measurements.
- **Source IPs:** 32 aliased IPs minimum (1M / 256 IDs per tuple ≈ 4000 tuples needed; 32 IPs × ~125 ports each = 4000 tuples). See §6.

### Tier 4 — Beyond 1M (not currently supported)

radstorm currently runs on a single host. For >1M subscriber tests you'd need either substantial single-host hardware (64+ cores, 128+ GB RAM, 100 GbE with DPDK or AF_XDP kernel bypass) or a multi-host distributed coordinator pattern. The latter is not in the codebase today and would be a substantial architectural addition. File an issue with your use case if you need this.

---

## 2. NIC tuning and offloading

NIC capabilities matter at Tier 3 and above. At lower tiers, defaults are fine.

### Critical (without these, you can't reliably hit Tier 3)

- **RSS (Receive Side Scaling)** — distributes inbound UDP across multiple CPU cores via packet hashing. Without it, all RX softirq work hits one core and that core caps around 200–400k packets/sec. Every modern 10/25/40 GbE NIC supports RSS; just verify it's enabled:
  ```bash
  ethtool -x eth0   # show indirection table
  ethtool -L eth0 combined 16   # set 16 queues
  ```
- **UDP/IP checksum offload (TX + RX)** — NIC computes the L4 checksum in hardware. radstorm uses standard kernel UDP sockets, so this is automatic on capable NICs. Saves 5–15% CPU at scale. Verify:
  ```bash
  ethtool -k eth0 | grep -E "(tx|rx)-checksumming"
  # Expect: "on" for both
  ```

### Helpful but not critical

- **GRO (Generic Receive Offload)** — aggregates incoming packets into larger logical units before handing to userspace. Modest RX-path win (~5%). Should be on by default:
  ```bash
  ethtool -k eth0 | grep generic-receive-offload
  ```
- **Multi-queue + IRQ affinity** — pin NIC queue interrupts to specific CPUs, away from the cores running radstorm goroutines. Reduces context-switch noise and improves the latency tail (~10ms off p99 at 1M scale). Use `set_irq_affinity_cpulist.sh` from your NIC vendor's package, or manually:
  ```bash
  # Pin queue interrupts to cores 0-7
  for irq in $(grep eth0 /proc/interrupts | awk '{print $1}' | tr -d ':'); do
    echo 0-7 > /proc/irq/$irq/smp_affinity_list
  done
  # Then let radstorm run on cores 8-31 via taskset:
  taskset -c 8-31 ./bin/radstorm run-scenario ...
  ```
- **Increased ring buffer sizes** — default RX/TX ring sizes are usually 256 or 512; bump to 4096 to absorb bursts:
  ```bash
  ethtool -g eth0   # show current
  ethtool -G eth0 rx 4096 tx 4096   # set to 4096
  ```

### Doesn't help radstorm

- **TSO / GSO (TCP Segmentation Offload)** — TCP only. radstorm is UDP. No effect.
- **LRO (Large Receive Offload)** — TCP only. No effect.
- **Jumbo frames (MTU 9000)** — RADIUS packets are small (~100–500 bytes, well under 1500 MTU). Stay at 1500.

### Future opportunities (not in the codebase today)

- **Hardware timestamping (PTP-class NICs)** — would let radstorm record packet send/receive times at the wire instead of in the kernel, removing scheduling jitter from latency measurements. Useful if you ever need to distinguish 100µs vs 500µs latencies. Would require switching from `net.UDPConn` to raw sockets with `SO_TIMESTAMPING`.
- **DPDK / AF_XDP kernel bypass** — unlocks 10M+ packets/sec on a single host. Currently radstorm uses the standard kernel UDP stack; bypass would require a substantial rewrite of `pkg/io`. Not needed for 1M-scale work.
- **SR-IOV** — essential if running radstorm in a virtualized environment. Without an SR-IOV VF, the hypervisor's vNIC bottlenecks around Tier 2.

### NIC recommendations for Tier 3 evaluation work

Two solid options at sensible budgets (2026 pricing, indicative):

| NIC | Speed | RSS | Hardware timestamping | Notes |
|---|---|---|---|---|
| **Intel X710-DA2 / X710-DA4** | 10 GbE | Yes (4 queues) | No | Commodity, well-supported on Linux. ~$300–500. Sweet spot for 100k–500k scale. |
| **NVIDIA / Mellanox ConnectX-6 Lx** | 25 GbE | Yes (excellent — many queues) | Yes | Best in class for radstorm at 1M. ~$600–900. Use this if buying new for serious carrier-scale work. |

Avoid consumer-grade 10 GbE cards (Realtek RTL8125 etc.) — limited RSS support, driver quality varies. Stick with Intel or Mellanox/NVIDIA for evaluation work.

### Verifying your NIC is correctly tuned

Quick diagnostic before a big run:

```bash
NIC=eth0
echo "=== Speed and link ==="
ethtool $NIC | grep -E "(Speed|Duplex|Link)"
echo
echo "=== Offloads ==="
ethtool -k $NIC | grep -E "(tx|rx)-checksumming|receive-offload"
echo
echo "=== Queues ==="
ethtool -l $NIC
echo
echo "=== Ring sizes ==="
ethtool -g $NIC
echo
echo "=== Drops (should be 0 after a fresh boot) ==="
ethtool -S $NIC | grep -iE "drop|miss|err"
```

If you see non-zero drops or errors after a clean test run, your NIC is the bottleneck — increase ring sizes, enable more RSS queues, or move to a better NIC.

---

## 3. OS packages

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

## 4. Getting the binary

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

## 5. Sysctl tuning

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

## 6. Source IP alias setup

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

## 7. Firewall considerations

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

## 8. File descriptor limits

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

For a systemd service, set `LimitNOFILE` in the unit file (see section 9).

Verify after applying:

```bash
# After re-login or in a new shell
ulimit -n
# Should show 1048576
```

---

## 9. Running as a systemd service

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

## 10. Log rotation

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

## 11. Backup

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

## 12. Upgrade procedure

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
