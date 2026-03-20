#!/bin/bash
# Start the OpenTela relay node on Clariden login node (clariden-ln003)
set -euo pipefail

REAL_HOME="$HOME"
RELAY_HOME="/tmp/opentela-relay"
CONFIG="$REAL_HOME/opentela/relay.cfg.yaml"
LOGFILE="$REAL_HOME/opentela/relay.log"
BINARY="$REAL_HOME/opentela/otela"

pkill -f "opentela/otela.*relay" 2>/dev/null || true
sleep 1

# Use /tmp for BadgerDB to avoid filesystem issues
rm -rf "$RELAY_HOME"
mkdir -p "$RELAY_HOME"

# Start relay with HOME overridden to /tmp
nohup env HOME="$RELAY_HOME" "$BINARY" start --config "$CONFIG" > "$LOGFILE" 2>&1 &
echo "Relay started with PID=$! (data dir: $RELAY_HOME)"
sleep 5
tail -30 "$LOGFILE"
