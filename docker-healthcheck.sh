#!/bin/sh
# Health check of the container: requests /-/healthy on the address vitrine
# listens on, which follows VITRINE_LISTEN and VITRINE_BASE_PATH (settings by
# flag can't be seen here).
port="${VITRINE_LISTEN:-:8080}"
port="${port##*:}"
base="${VITRINE_BASE_PATH%/}"
case "$base" in
    "" | /*) ;;
    *) base="/$base" ;;
esac
exec wget -q --spider "http://127.0.0.1:${port}${base}/-/healthy"
