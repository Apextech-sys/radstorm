#!/usr/bin/env bash
# Smoke test for the radstorm Docker FreeRADIUS test rig
#
# Purpose:
#   Validates that the FreeRADIUS container is up, accepting auth and accounting
#   requests, and returning the correct response codes. Uses radclient from a
#   Docker sidecar (freeradius/freeradius-server:latest) to avoid needing any
#   local RADIUS tooling. Tests PAP auth for sub00000001 and an Accounting-Start.
#
# Related: test/docker/docker-compose.yml, test/docker/freeradius/clients.conf,
#          test/docker/freeradius/users
# Briefing: .orchestration/briefings/1e-docker-rig.md
# Contract:
#   Exit 0 if Access-Accept AND Accounting-Response are both received.
#   Exit 1 on any assertion failure (prints which response was missing/wrong).
#   Shared secret: testing123
#   FreeRADIUS container name: radstorm-freeradius
#   Docker network: docker_radius_net (compose project name "docker")
#
# Usage: bash test/docker/smoke.sh [--skip-up]
#   --skip-up   Skip docker compose up (assume FreeRADIUS is already running)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_FILE="${SCRIPT_DIR}/docker-compose.yml"

SHARED_SECRET="testing123"
TEST_USER="sub00000001"
TEST_PASS="pw00000001"
ACCT_SESSION_ID="smoke-$(date +%s)"
FREERADIUS_IMAGE="freeradius/freeradius-server:latest"

SKIP_UP=false
for arg in "$@"; do
    if [[ "${arg}" == "--skip-up" ]]; then
        SKIP_UP=true
    fi
done

# ─── Colour helpers ───────────────────────────────────────────────────────────
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

pass()  { echo -e "${GREEN}[PASS]${NC} $*"; }
fail()  { echo -e "${RED}[FAIL]${NC} $*"; exit 1; }
info()  { echo -e "${YELLOW}[INFO]${NC} $*"; }

# ─── Bring up FreeRADIUS if not already running ───────────────────────────────
if [[ "${SKIP_UP}" == "false" ]]; then
    info "Bringing up FreeRADIUS test rig..."
    docker compose -f "${COMPOSE_FILE}" up -d --wait freeradius
    info "FreeRADIUS is up and healthy."
fi

# ─── Determine the Docker network FreeRADIUS is attached to ──────────────────
CONTAINER_NAME="radstorm-freeradius"
NETWORK=$(docker inspect "${CONTAINER_NAME}" \
    --format '{{range $k,$v := .NetworkSettings.Networks}}{{$k}}{{end}}' 2>/dev/null | head -1)

if [[ -z "${NETWORK}" ]]; then
    fail "Container ${CONTAINER_NAME} not found or not running."
fi

info "FreeRADIUS network: ${NETWORK}"

# ─── Helper: send a radclient request via a Docker sidecar ────────────────────
# Pipes attribute pairs on stdin into radclient running in a temporary container
# on the same Docker network as FreeRADIUS. Returns the radclient output.
run_radclient() {
    local host="$1"    # hostname (container name or IP)
    local port="$2"    # UDP port
    local type="$3"    # auth | acct
    local attrs="$4"   # newline-separated attribute pairs

    echo "${attrs}" | docker run --rm -i \
        --network "${NETWORK}" \
        "${FREERADIUS_IMAGE}" \
        radclient -x -r 3 -t 5 \
        "${host}:${port}" "${type}" "${SHARED_SECRET}"
}

# ─── Test 1: Access-Request (PAP) → expect Access-Accept ─────────────────────
info "Test 1: Sending Access-Request (PAP) for ${TEST_USER}..."

AUTH_ATTRS="User-Name = \"${TEST_USER}\",
User-Password = \"${TEST_PASS}\",
NAS-IP-Address = 127.0.0.1,
NAS-Identifier = \"radstorm-smoke\",
Service-Type = Framed-User"

AUTH_OUTPUT=$(run_radclient "${CONTAINER_NAME}" "1812" "auth" "${AUTH_ATTRS}" 2>&1)

echo "${AUTH_OUTPUT}"
echo ""

if echo "${AUTH_OUTPUT}" | grep -q "Access-Accept"; then
    pass "Access-Accept received"
else
    fail "Expected Access-Accept but got: ${AUTH_OUTPUT}"
fi

# ─── Test 2: Accounting-Request (Start) → expect Accounting-Response ──────────
info "Test 2: Sending Accounting-Request (Start) for ${TEST_USER}..."

ACCT_ATTRS="User-Name = \"${TEST_USER}\",
NAS-IP-Address = 127.0.0.1,
NAS-Identifier = \"radstorm-smoke\",
Acct-Status-Type = Start,
Acct-Session-Id = \"${ACCT_SESSION_ID}\",
Acct-Authentic = RADIUS,
NAS-Port = 1"

ACCT_OUTPUT=$(run_radclient "${CONTAINER_NAME}" "1813" "acct" "${ACCT_ATTRS}" 2>&1)

echo "${ACCT_OUTPUT}"
echo ""

if echo "${ACCT_OUTPUT}" | grep -q "Accounting-Response"; then
    pass "Accounting-Response received"
else
    fail "Expected Accounting-Response but got: ${ACCT_OUTPUT}"
fi

# ─── All tests passed ─────────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}============================================${NC}"
echo -e "${GREEN} Smoke test PASSED -- FreeRADIUS rig is OK ${NC}"
echo -e "${GREEN}============================================${NC}"
echo ""
echo "  Auth  : ${TEST_USER} @ radstorm-freeradius:1812 -> Access-Accept"
echo "  Acct  : ${TEST_USER} @ radstorm-freeradius:1813 -> Accounting-Response"
echo "  Secret: ${SHARED_SECRET}"
echo "  Image : ${FREERADIUS_IMAGE} (FreeRADIUS 3.2.x)"
