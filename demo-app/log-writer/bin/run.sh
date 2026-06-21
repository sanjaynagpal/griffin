#!/usr/bin/env bash
# Griffin run command for log-writer.
# Usage: run.sh START | STOP | STATUS
#
# Writes a stream of realistic-looking structured log lines every second,
# cycling through INFO / WARN / ERROR levels. Useful for demonstrating
# Griffin's Log View and live log-tail feature.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
COMPONENT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
SERVICE_NAME="$(basename "$COMPONENT_ROOT")"
PID_FILE="$COMPONENT_ROOT/${SERVICE_NAME}.pid"
LOG_DIR="$COMPONENT_ROOT/logs"
STDOUT_LOG="$LOG_DIR/stdout.log"
STDERR_LOG="$LOG_DIR/stderr.log"

is_running() {
    [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null
}

# ---------------------------------------------------------------------------
# Worker — runs in the background after daemonisation.
# ---------------------------------------------------------------------------
_worker() {
    local tick=0
    local -a levels=(INFO INFO INFO INFO WARN ERROR)
    local -a messages=(
        "request processed       path=/api/health        latency=2ms"
        "cache hit               key=user:session:4821   ratio=0.94"
        "queue flushed           items=17                elapsed=3ms"
        "background task done    job=cleanup             duration=120ms"
        "latency spike detected  endpoint=/api/data      latency=248ms"
        "connection timeout      host=db.internal:5432   attempt=3"
        "request processed       path=/api/metrics       latency=1ms"
        "index rebuilt           docs=14821              elapsed=4.2s"
        "rate limit approached   client=10.0.1.4         usage=87%"
        "retry succeeded         host=cache.internal     attempt=2"
    )
    local msg_count=${#messages[@]}
    local lvl_count=${#levels[@]}

    while true; do
        local ts
        ts="$(date -Iseconds)"
        local lvl="${levels[$((tick % lvl_count))]}"
        local msg="${messages[$((tick % msg_count))]}"
        printf '%s %-5s %s\n' "$ts" "$lvl" "$msg"
        tick=$((tick + 1))
        sleep 1
    done
}

export -f _worker

cmd_start() {
    if is_running; then
        echo "log-writer: already running (pid $(cat "$PID_FILE"))"
        exit 0
    fi

    mkdir -p "$LOG_DIR"

    # Run the worker in a subshell so it can be killed by PID.
    nohup bash -c '_worker' >> "$STDOUT_LOG" 2>> "$STDERR_LOG" &

    echo $! > "$PID_FILE"
    echo "log-writer: started (pid $(cat "$PID_FILE"))"
}

cmd_stop() {
    if ! is_running; then
        echo "log-writer: not running"
        rm -f "$PID_FILE"
        exit 0
    fi

    PID=$(cat "$PID_FILE")
    kill "$PID"

    for _ in $(seq 1 10); do
        sleep 0.5
        kill -0 "$PID" 2>/dev/null || break
    done

    rm -f "$PID_FILE"
    echo "log-writer: stopped (pid $PID)"
}

cmd_status() {
    if is_running; then
        PID=$(cat "$PID_FILE")
        # Count lines written since the log file was created.
        local lines=0
        [ -f "$STDOUT_LOG" ] && lines=$(wc -l < "$STDOUT_LOG")
        echo "Status:     RUNNING"
        echo "PID:        $PID"
        echo "Log lines:  $lines"
        echo "Log file:   $STDOUT_LOG"
    else
        echo "Status:     STOPPED"
    fi
}

case "${1:-}" in
    START)  cmd_start  ;;
    STOP)   cmd_stop   ;;
    STATUS) cmd_status ;;
    *)
        echo "usage: $(basename "$0") START | STOP | STATUS" >&2
        exit 1
        ;;
esac
