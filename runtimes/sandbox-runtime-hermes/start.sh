#!/bin/sh
set -eu
# The runtime calls the stage-scoped model endpoint directly.  In particular,
# it does not start Hermes gateway or any platform/scheduler/desktop surface.
exec /app/bin/runtime --serve --port 8888
