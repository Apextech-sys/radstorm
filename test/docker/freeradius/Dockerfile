# FreeRADIUS test-rig image
#
# Purpose:
#   Builds a FreeRADIUS 3.2 image pre-loaded with radstorm's test configuration.
#   Overlays only clients.conf and users on top of the stock FreeRADIUS defaults.
#   The stock default site already handles PAP + CHAP via the "files" + "pap" +
#   "chap" modules, which is all we need for a stress-test target.
#
# Related: test/docker/docker-compose.yml, test/docker/freeradius/clients.conf,
#          test/docker/freeradius/users
# Briefing: .orchestration/briefings/1e-docker-rig.md
# Contract: Exposes UDP 1812 (auth) and UDP 1813 (acct); shared secret testing123.
#           Note: freeradius/freeradius-server:latest == FreeRADIUS 3.2.x (Ubuntu).
#           The 3.2-alpine tag does not exist on Docker Hub as of 2026-05.

FROM freeradius/freeradius-server:latest

# Overlay client authorisation and seeded test users.
# The image config root is /etc/freeradius.
COPY clients.conf    /etc/freeradius/clients.conf
COPY users           /etc/freeradius/users

EXPOSE 1812/udp 1813/udp

# Run in foreground so Docker logs capture FreeRADIUS output.
CMD ["freeradius", "-f", "-l", "stdout"]
