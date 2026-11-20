#!/usr/bin/env bash
# Manage the local development background worker (outbox dispatch, audit sealing, privacy monitor).
# Usage: ./scripts/dev-worker.sh [start|stop|status|once]
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PIDFILE="$ROOT/.local/worker.pid"
LOGFILE="$ROOT/.local/worker.log"
BIN="$ROOT/.local/worker"

cd "$ROOT"
mkdir -p "$ROOT/.local"

if [ -f "$ROOT/.env" ]; then
  set -a
  # shellcheck disable=SC1091
  . "$ROOT/.env"
  set +a
fi

cmd="${1:-status}"
case "$cmd" in
  once)
    exec go run ./cmd/worker -once
    ;;
  start)
    if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
      echo "worker already running (pid $(cat "$PIDFILE"))"
      exit 0
    fi
    go build -o "$BIN" ./cmd/worker
    nohup "$BIN" >>"$LOGFILE" 2>&1 &
    echo $! >"$PIDFILE"
    echo "worker started (pid $(cat "$PIDFILE")), log: $LOGFILE"
    ;;
  stop)
    if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
      kill "$(cat "$PIDFILE")"
      rm -f "$PIDFILE"
      echo "worker stopped"
    else
      rm -f "$PIDFILE"
      echo "worker not running"
    fi
    ;;
  status)
    if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
      echo "worker running (pid $(cat "$PIDFILE"))"
    else
      echo "worker stopped"
      exit 1
    fi
    ;;
  *)
    echo "usage: $0 {start|stop|status|once}" >&2
    exit 2
    ;;
esac
