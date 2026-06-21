#!/usr/bin/env bash
# Griffin run command for http-server.
# Usage: run.sh START | STOP | STATUS
#
# Starts a Python HTTP server on port 8080, serving files from cfg/.
# Requires: python3

set -euo pipefail

# ---------------------------------------------------------------------------
# Paths — derived from this script's location so the service is relocatable.
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
COMPONENT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
SERVICE_NAME="$(basename "$COMPONENT_ROOT")"
PID_FILE="$COMPONENT_ROOT/${SERVICE_NAME}.pid"
LOG_DIR="$COMPONENT_ROOT/logs"
STDOUT_LOG="$LOG_DIR/stdout.log"
STDERR_LOG="$LOG_DIR/stderr.log"
SERVE_DIR="$COMPONENT_ROOT/cfg"
PORT=8080

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
is_running() {
    [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null
}

# ---------------------------------------------------------------------------
# Commands
# ---------------------------------------------------------------------------
cmd_start() {
    if is_running; then
        echo "http-server: already running (pid $(cat "$PID_FILE"))"
        exit 0
    fi

    if ! command -v python3 >/dev/null 2>&1; then
        echo "http-server: python3 is required but not found in PATH" >&2
        exit 1
    fi

    mkdir -p "$LOG_DIR"

    nohup python3 -u -m http.server "$PORT" \
        --directory "$SERVE_DIR" \
        >> "$STDOUT_LOG" 2>> "$STDERR_LOG" &

    echo $! > "$PID_FILE"
    echo "http-server: started (pid $(cat "$PID_FILE")) on port $PORT"
}

cmd_stop() {
    if ! is_running; then
        echo "http-server: not running"
        rm -f "$PID_FILE"
        exit 0
    fi

    PID=$(cat "$PID_FILE")
    kill "$PID"

    # Wait up to 5 s for the process to exit.
    for _ in $(seq 1 10); do
        sleep 0.5
        kill -0 "$PID" 2>/dev/null || break
    done

    rm -f "$PID_FILE"
    echo "http-server: stopped (pid $PID)"
}

cmd_status() {
    if is_running; then
        PID=$(cat "$PID_FILE")
        echo "Status:  RUNNING"
        echo "PID:     $PID"
        echo "Port:    $PORT"
        echo "Serving: $SERVE_DIR"
        echo "Logs:    $LOG_DIR"
    else
        echo "Status:  STOPPED"
    fi
}

# ---------------------------------------------------------------------------
# Dispatch
# ---------------------------------------------------------------------------
case "${1:-}" in
    START)  cmd_start  ;;
    STOP)   cmd_stop   ;;
    STATUS) cmd_status ;;
    *)
        echo "usage: $(basename "$0") START | STOP | STATUS" >&2
        exit 1
        ;;
esac
