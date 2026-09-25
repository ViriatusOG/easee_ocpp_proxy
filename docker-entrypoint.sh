#!/bin/sh
# Runs the proxy as the non-root "proxy" user.
# The /config directory is bind-mounted from the host (see docker-compose.yml);
# it may be root-owned, so fix ownership before dropping privileges.
set -e

mkdir -p /config
# First boot: seed a working config from the bundled example so the proxy
# starts and the /admin UI can finish first-run setup (admin password,
# chargepoint IDs). Existing configs are never touched.
if [ ! -f /config/config.yaml ]; then
  cp /usr/local/share/easee-proxy/config.example.yaml /config/config.yaml
  echo "Seeded /config/config.yaml from the bundled example; configure via /admin."
fi
chown -R proxy:proxy /config

FLAGS="-config /config/config.yaml"
if [ -n "$DEBUG" ]; then
  FLAGS="$FLAGS -debug"
fi

exec su -s /bin/sh proxy -c "/usr/local/bin/easee-proxy $FLAGS"
