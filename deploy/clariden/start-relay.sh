#!/bin/bash
# Start the OpenTela relay node on Clariden login node (clariden-ln003)
set -euo pipefail

REAL_HOME="$HOME"
RELAY_HOME="/tmp/opentela-relay"
CONFIG="$REAL_HOME/opentela/relay.cfg.yaml"
LOGFILE="$REAL_HOME/opentela/relay.log"
BINARY="$REAL_HOME/opentela/otela"
RELAY_PORT=18092
TCP_PORT=18905
BOOTSTRAP_URL="https://bootstraps.opentela.ai"

pkill -f "opentela/otela.*relay" 2>/dev/null || true
sleep 1

# Detect the primary IP of this login node.
NODE_IP=$(hostname -I | awk '{print $1}')
echo "Detected login node IP: $NODE_IP ($(hostname))"

# Patch public-addr in config to match this node's actual IP
sed -i "s/^public-addr:.*/public-addr: \"${NODE_IP}\"/" "$CONFIG"

# Use /tmp for BadgerDB to avoid filesystem issues
rm -rf "$RELAY_HOME"
mkdir -p "$RELAY_HOME"

# Start relay with HOME overridden to /tmp
nohup env HOME="$RELAY_HOME" "$BINARY" start --config "$CONFIG" > "$LOGFILE" 2>&1 &
RELAY_PID=$!
echo "Relay started with PID=$RELAY_PID (data dir: $RELAY_HOME)"

# Wait for relay to be healthy
echo "Waiting for relay to be ready..."
for i in $(seq 1 30); do
    curl -sf "http://localhost:${RELAY_PORT}/v1/health" > /dev/null 2>&1 && break
    if ! kill -0 "$RELAY_PID" 2>/dev/null; then
        echo "ERROR: relay process died"
        tail -30 "$LOGFILE"
        exit 1
    fi
    sleep 1
done

tail -30 "$LOGFILE"

# --- Registration with bootstrap service ---

register_relay() {
    # Get self info (includes peer ID, build attestation, etc.)
    SELF=$(curl -sf "http://localhost:${RELAY_PORT}/v1/self")
    if [ -z "$SELF" ]; then
        echo "WARN: could not get self info"
        return 1
    fi
    PEER_ID=$(echo "$SELF" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")

    # Step 1: Get challenge nonce from head node
    CHALLENGE=$(curl -sf "${BOOTSTRAP_URL}/v1/dnt/challenge?peer_id=${PEER_ID}")
    if [ -z "$CHALLENGE" ]; then
        echo "WARN: challenge request failed"
        return 1
    fi
    NONCE=$(echo "$CHALLENGE" | python3 -c "import sys,json; print(json.load(sys.stdin)['nonce'])")

    # Step 2: Sign nonce with libp2p key (via local endpoint)
    SIGNED=$(curl -sf -X POST "http://localhost:${RELAY_PORT}/v1/sign" \
        -H "Content-Type: application/json" \
        -d "{\"data\": \"${NONCE}\"}")
    if [ -z "$SIGNED" ]; then
        echo "WARN: signing failed"
        return 1
    fi
    SIGNATURE=$(echo "$SIGNED" | python3 -c "import sys,json; print(json.load(sys.stdin)['signature'])")
    PUBLIC_KEY=$(echo "$SIGNED" | python3 -c "import sys,json; print(json.load(sys.stdin)['public_key'])")

    # Step 3: Register with bootstrap service
    PAYLOAD=$(echo "$SELF" | python3 -c "
import sys, json
peer = json.load(sys.stdin)
peer['challenge_response'] = {
    'nonce': '${NONCE}',
    'signature': '${SIGNATURE}',
    'peer_id': '${PEER_ID}',
    'public_key': '${PUBLIC_KEY}'
}
json.dump(peer, sys.stdout)
")
    RESULT=$(curl -sf -X POST "${BOOTSTRAP_URL}/v1/dnt/register" \
        -H "Content-Type: application/json" \
        -d "$PAYLOAD")
    if [ -z "$RESULT" ]; then
        echo "WARN: registration POST failed"
        return 1
    fi
    echo "Registered: $RESULT"
    return 0
}

# Registration loop with exponential backoff
BACKOFF=5
while kill -0 "$RELAY_PID" 2>/dev/null; do
    if register_relay; then
        BACKOFF=5
        sleep 300  # re-register every 5 minutes
    else
        echo "Registration failed, retrying in ${BACKOFF}s"
        sleep $BACKOFF
        BACKOFF=$((BACKOFF * 2))
        [ $BACKOFF -gt 120 ] && BACKOFF=120
    fi
done &
REGISTER_PID=$!

echo "Registration loop running (PID=$REGISTER_PID)"
echo "Relay is ready on ${NODE_IP}:${TCP_PORT}"
