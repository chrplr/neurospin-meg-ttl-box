#!/bin/bash
# run-session3.sh — the blocks that need no instrument, only the loopback.
#
# Session 3 uses the box as its own witness: an output line jumpered to an input
# line means the firmware timestamps the edge its own command produced. That is
# enough for absolute host->device latency at any sample size, for the
# split-write stall, and for sustained-load behaviour — none of which need a
# BBTK, and all of which are worth re-running on any stimulus PC before it is
# used for an experiment.
#
# Usage:
#   ./run-session3.sh              # everything
#   ./run-session3.sh -n           # dry run
#   ./run-session3.sh latency      # one block
#
# Environment overrides:
#   TTLBOX_PORT  serial port        (default: /dev/ttyACM0)
#   PULSES       latency trials     (default: 10000)
#   LOAD_CMD     load generator     (default: stress-ng if present, else skipped)
#   OVERFLOW_MIN sustained run, min (default: 60)
#   OUTDIR       session directory  (default: measurements/<date>-s3-loopback)
#
# WIRING: box D30 -> box D22, and GND common. That is the whole requirement.
# The full harness of session 1 works too; nothing here uses the BBTK.
#
# The latency block is run three times, because the number that matters for an
# experiment is not the median on an idle machine: it is what happens when the
# stimulus PC is also decoding video. If the three conditions differ, the
# conclusion is about the host, not the box.

set -u
set -o pipefail

TTLBOX_PORT="${TTLBOX_PORT:-/dev/ttyACM0}"
PULSES="${PULSES:-10000}"
OVERFLOW_MIN="${OVERFLOW_MIN:-60}"

DRY_RUN=0
if [ "${1:-}" = "-n" ]; then
	DRY_RUN=1
	shift
fi
ONLY="${1:-all}"

cd "$(dirname "$0")/.." || exit 1
OUTDIR="${OUTDIR:-measurements/$(date +%F)-s3-loopback}"

BIN="$PWD/measurements/.bin/ttlbox-timing"
mkdir -p "$(dirname "$BIN")"
echo "+ go build -o ${BIN#"$PWD"/} ./cmd/ttlbox-timing"
go build -o "$BIN" ./cmd/ttlbox-timing || exit 1

# want reports whether a block was asked for. Everything a block needs — a load
# generator, a privilege change — has to be gated on this too, or selecting one
# block still drags in the setup for the others.
want() { [ "$ONLY" = "all" ] || [ "$ONLY" = "$1" ]; }

run() { # run <block> <flags...>
	local block="$1"
	shift
	want "$block" || return 0
	echo
	echo "=== $block $* ==="
	if [ "$DRY_RUN" = 1 ]; then
		"$BIN" "$block" -n "$@"
		return 0
	fi
	"$BIN" "$block" --port "$TTLBOX_PORT" --out "$OUTDIR" "$@" |
		tee -a "$OUTDIR/session3.log"
}

if [ "$DRY_RUN" != 1 ]; then
	if [ ! -e "$TTLBOX_PORT" ]; then
		echo "error: $TTLBOX_PORT not found — is the box plugged in?" >&2
		exit 1
	fi
	mkdir -p "$OUTDIR"
fi

echo
echo "Session:  $OUTDIR/"
echo "TTL box:  $TTLBOX_PORT"
echo "Wiring:   D30 -> D22, common GND"

# --- absolute host->device latency, three host conditions ------------------
run latency --pulses "$PULSES" --isi 20 --condition idle --tag -idle

LOAD_CMD="${LOAD_CMD:-}"
if [ -z "$LOAD_CMD" ] && command -v stress-ng >/dev/null 2>&1; then
	LOAD_CMD="stress-ng --cpu 0 --timeout 1h"
fi
if ! want latency; then
	LOAD_CMD=""
fi
if [ -n "$LOAD_CMD" ]; then
	if [ "$DRY_RUN" = 1 ]; then
		echo
		echo "+ $LOAD_CMD &   (background load during the next block)"
		run latency --pulses "$PULSES" --isi 20 --condition load --tag -load
	else
		echo
		echo "+ $LOAD_CMD &"
		$LOAD_CMD >/dev/null 2>&1 &
		LOAD_PID=$!
		trap 'kill "$LOAD_PID" 2>/dev/null' EXIT
		run latency --pulses "$PULSES" --isi 20 --condition load --tag -load
		kill "$LOAD_PID" 2>/dev/null
		wait "$LOAD_PID" 2>/dev/null
		trap - EXIT
	fi
elif want latency; then
	echo
	echo "note: no load generator found — install stress-ng or set LOAD_CMD to"
	echo "      measure the loaded condition. Skipping it."
fi

if want latency && command -v chrt >/dev/null 2>&1; then
	echo
	echo "=== latency at real-time priority ==="
	if [ "$DRY_RUN" = 1 ]; then
		echo "+ chrt -f 50 $BIN latency --condition rt --tag -rt ..."
	elif [ "$(ulimit -r)" = "0" ] && [ "$(id -u)" != "0" ]; then
		# Check before running rather than after: chrt failing leaves a
		# half-open serial port and a confusing error, and the limit is
		# knowable up front.
		echo "  skipped: RLIMIT_RTPRIO is 0 for this user, so no real-time"
		echo "  priority is obtainable. To measure this condition, either run"
		echo "  the block under sudo, or raise the limit in"
		echo "  /etc/security/limits.conf:  $(id -un)  -  rtprio  50"
	else
		# chrt can still be refused for reasons the check above misses; a
		# failure here is not fatal to the rest of the session.
		chrt -f 50 "$BIN" latency --port "$TTLBOX_PORT" --out "$OUTDIR" \
			--pulses "$PULSES" --isi 20 --condition rt --tag -rt |
			tee -a "$OUTDIR/session3.log" ||
			echo "  (chrt failed — skipped)"
	fi
fi

# --- the split-write stall -------------------------------------------------
run splitwrite

# --- sustained load and queue overflow -------------------------------------
run overflow --duration "${OVERFLOW_MIN}m"

if [ "$DRY_RUN" = 1 ]; then
	echo
	echo "(dry run — nothing recorded)"
	exit 0
fi

printf '\nDone. Files in %s/\n' "$OUTDIR"
printf '\nRead the numbers with:\n  ./measurements/analyse-timing.py %s\n' "$OUTDIR"
