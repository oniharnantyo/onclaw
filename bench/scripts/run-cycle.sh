#!/usr/bin/env bash
# run-cycle.sh — flap-tolerant bench cycle driver.
#
# Relaunches the run with --resume until it completes: the circuit breaker
# stops the run on a provider flap (exit 1, journal preserved), this script
# sleeps through the outage and resumes from the journal, so a 108-run cycle
# survives the provider flapping every few hours instead of restarting from
# zero. Results are identical to one uninterrupted run: sequential seeds,
# fresh session per (task, seed), same checks.
#
# Usage (from bench/): scripts/run-cycle.sh --out <scoreboard.json> [extra run flags]
# Requires ONCLAW_BENCH_ADMIN_EMAIL / ONCLAW_BENCH_ADMIN_PASSWORD (or flags).

set -uo pipefail

OUT="scoreboard.json"
ARGS=()
while [ $# -gt 0 ]; do
  case "$1" in
    --out) OUT="$2"; shift 2 ;;
    --out=*) OUT="${1#--out=}"; shift ;;
    *) ARGS+=("$1"); shift ;;
  esac
done

ATTEMPT=0
while true; do
  ATTEMPT=$((ATTEMPT + 1))
  echo "=== cycle attempt $ATTEMPT ($(date '+%H:%M:%S'))"
  if go run . run --resume --out "$OUT" "${ARGS[@]}"; then
    echo "=== cycle complete after $ATTEMPT attempt(s)"
    exit 0
  fi
  echo "=== attempt $ATTEMPT aborted (breaker or operational failure); resuming in 180s"
  sleep 180
done
