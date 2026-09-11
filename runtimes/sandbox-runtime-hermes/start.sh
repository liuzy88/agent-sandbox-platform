#!/bin/sh
set -eu
# Gateway uses the normal Hermes provider configuration in /opt/data.
: "${API_SERVER_KEY:=agent-platform-local}"
export API_SERVER_ENABLED API_SERVER_KEY
hermes gateway &
gateway_pid=$!
trap 'kill "$gateway_pid" 2>/dev/null || true' EXIT INT TERM

# Do not accept platform executions before the delegated Hermes API is ready.
ready=0
for attempt in $(seq 1 60); do
  if curl --fail --silent --show-error \
      -H "Authorization: Bearer $API_SERVER_KEY" \
      http://127.0.0.1:8642/health >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 1
done
if [ "$ready" -ne 1 ]; then
  echo "Hermes gateway did not become ready on port 8642" >&2
  exit 1
fi
exec /app/bin/runtime --serve --port 8888
