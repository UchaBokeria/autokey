#!/usr/bin/env bash
# autokey-tunnel: persistent quick tunnel + worker repoint.
# On every (re)start: launch cloudflared quick tunnel to the local hook,
# extract the public URL, redeploy the inbox worker to it, then wait.
# systemd restarts this on reboot/failure -> ingress self-heals.
set -u
HOME_DIR="${HOME:-/home/scriptkid}"
HOOK_PORT="$(grep -A2 '^hook:' "$HOME_DIR/.autoApiKeys/config.yaml" | grep 'port:' | awk '{print $2}')"
HOOK_PORT="${HOOK_PORT:-18765}"
AUTOKEY_BIN="$(command -v autokey || echo "$HOME_DIR/go/bin/autokey")"
CLOUDFLARED="$HOME_DIR/.local/bin/cloudflared"
LOG="$HOME_DIR/.autoApiKeys/logs/tunnel.log"

mkdir -p "$(dirname "$LOG")"
echo "$(date -u +%FT%TZ) starting tunnel -> 127.0.0.1:$HOOK_PORT" >> "$LOG"

"$CLOUDFLARED" tunnel --url "http://127.0.0.1:$HOOK_PORT" > /tmp/opencode-autokey-tunnel.log 2>&1 &
CFD_PID=$!
cleanup() { kill "$CFD_PID" 2>/dev/null; }
trap cleanup EXIT

URL=""
for i in $(seq 1 30); do
  URL=$(grep -o -m1 "https://[a-z0-9-]*\.trycloudflare\.com" /tmp/opencode-autokey-tunnel.log 2>/dev/null || true)
  if [ -n "$URL" ]; then break; fi
  sleep 2
done
if [ -z "$URL" ]; then
  echo "$(date -u +%FT%TZ) ERROR: no tunnel URL after 60s" >> "$LOG"
  tail -n 5 /tmp/opencode-autokey-tunnel.log >> "$LOG"
  exit 1
fi
echo "$(date -u +%FT%TZ) tunnel up: $URL" >> "$LOG"

# Repoint the inbox worker at the fresh URL (idempotent).
if "$AUTOKEY_BIN" worker deploy --inbox-url "$URL/v1/inbox" >> "$LOG" 2>&1; then
  echo "$(date -u +%FT%TZ) worker repointed to $URL/v1/inbox" >> "$LOG"
else
  echo "$(date -u +%FT%TZ) WARN: worker deploy failed (will retry next restart)" >> "$LOG"
fi

wait "$CFD_PID"
