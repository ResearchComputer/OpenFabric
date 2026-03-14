#!/bin/bash
# Clean up and restart the relay
pkill -f "entry start" 2>/dev/null || true
sleep 2
rm -rf /tmp/opentela-relay-*
rm -rf "$HOME/.ocfcore/ocfcore.QmNTv9"*.db 2>/dev/null || true
echo "Cleaned old data"
bash "$HOME/opentela/start-relay.sh"
