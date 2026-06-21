#!/usr/bin/env bash
# Griffin run command for counter.
# Usage: run.sh START | STOP | STATUS
#
# Runs a compute loop that burns CPU in short bursts every few seconds,
# then sleeps. This makes CPU% visible and variable in Griffin's Metrics
# Panel and Status View, demonstrating the sparkline behaviour.
#
# Each burst sums integers up to N (pure bash arithmetic — no subprocesses)
# and logs the result along with a running total and elapsed time.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
COMPONENT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
SERVICE_NAME="$(basename "$COMPONENT_ROOT")"
PID_FILE="$COMPONENT_ROOT/${SERVICE_NAME}.pid"
LOG_DIR="$COMPONENT_ROOT/logs"
STDOUT_LOG="$LOG_DIR/stdout.log"
STDERR_LOG="$LOG_DIR/stderr.log"

# Read burst size from config, with a safe default.
CFG="$COMPONENT_ROOT/cfg/config.yaml"
BURST=50000
INTERVAL=3
if [ -f "$CFG" ]; then
    v=$(grep 'iterations_per_burst' "$CFG" | awk '{print $2}')
    [ -n "$v" ] && BURST=$v
    v=$(grep 'burst_interval_seconds' "$CFG" | awk '{print $2}')
    [ -n "$v" ] && INTERVAL=$v
fi

is_running() {
    [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null
}

_worker() {
    local burst="$1"
    local interval="$2"
    local run=0
    local grand_total=0

    while true; do
        local start_ns
        start_ns=$(date +%s%N)

        # CPU burst — pure bash arithmetic, no subprocess overhead.
        local acc=0
        local i
        for ((i = 1; i <= burst; i++)); do
            acc=$((acc + i))
        done

        local end_ns
        end_ns=$(date +%s%N)
        local elapsed_ms=$(( (end_ns - start_ns) / 1000000 ))

        run=$((run + 1))
        grand_total=$((grand_total + acc))

        printf '%s INFO  burst=%-4d  n=%-6d  sum=%-12d  elapsed=%dms  total=%d\n' \
            "$(date -Iseconds)" "$run" "$burst" "$acc" "$elapsed_ms" "$grand_total"

        sleep "$interval"
    done
}

export -f _worker

cmd_start() {
    if is_running; then
        echo "counter: already running (pid $(cat "$PID_FILE"))"
        exit 0
    fi

    mkdir -p "$LOG_DIR"

    nohup bash -c "_worker $BURST $INTERVAL" >> "$STDOUT_LOG" 2>> "$STDERR_LOG" &

    echo $! > "$PID_FILE"
    echo "counter: started (pid $(cat "$PID_FILE")) burst=$BURST interval=${INTERVAL}s"
}

cmd_stop() {
    if ! is_running; then
        echo "counter: not running"
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
    echo "counter: stopped (pid $PID)"
}

cmd_status() {
    if is_running; then
        PID=$(cat "$PID_FILE")
        local bursts=0
        [ -f "$STDOUT_LOG" ] && bursts=$(wc -l < "$STDOUT_LOG")
        # Pull the last logged total from the log file.
        local last_line=""
        [ -f "$STDOUT_LOG" ] && last_line=$(tail -1 "$STDOUT_LOG")
        echo "Status:     RUNNING"
        echo "PID:        $PID"
        echo "Bursts:     $bursts"
        echo "Last entry: ${last_line:-—}"
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
