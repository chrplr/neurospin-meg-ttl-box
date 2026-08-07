#!/bin/bash
# run-session1.sh — run one measurement block inside a BBTKv3 capture.
#
# Session 1 is the arm where the box emits and the instrument observes. The
# BBTK owns the whole run: bbtk-capture sets the device up and starts the block
# at the instant recording begins, through its `-- command` form, so the
# sequence and the recording cannot get out of step and there is no way to emit
# pulses into a capture that never started.
#
# Usage:
#   ./run-session1.sh widths                 # record to measurements/<date>-s1-widths/
#   ./run-session1.sh -n codechange          # dry run: print the plan, record nothing
#   ./run-session1.sh latency --pulses 2000  # extra flags go to ttlbox-timing
#
# Blocks worth running here: widths, codechange, latency, drift.
#
# Environment overrides:
#   TTLBOX_PORT       TTL box serial port     (default: /dev/ttyACM0)
#   BBTK_PORT         BBTK serial port        (default: bbtk-capture's own)
#   BBTK_CAPTURE_BIN  path to bbtk-capture    (default: bbtk-capture on PATH)
#   BBTK_MARGIN_S     recorded margin either side of the block (default: 10)
#   OUTDIR            session directory       (default: measurements/<date>-s1-<block>)
#
# WIRING (see measurements/README.md — the same harness serves every block):
#     box D30 (line 0) -> BBTK TTLin2  and  box D22 (input 0)
#     box D31 (line 1) -> BBTK TTLin1  and  box D23 (input 1)
#     GND              -> GND     <- required; TTL without a shared reference
#                                    gives unreliable edges
#
# The capture window is computed from the block itself (--seconds) rather than
# hard-coded, because the device enforces the window and cannot be asked to stop
# early and hand back what it has. A window that turns out too short means
# re-running the whole thing.

set -u
set -o pipefail

TTLBOX_PORT="${TTLBOX_PORT:-/dev/ttyACM0}"
BBTK_CAPTURE_BIN="${BBTK_CAPTURE_BIN:-bbtk-capture}"
BBTK_MARGIN_S="${BBTK_MARGIN_S:-10}"

DRY_RUN=0
if [ "${1:-}" = "-n" ]; then
	DRY_RUN=1
	shift
fi

BLOCK="${1:-}"
if [ -z "$BLOCK" ]; then
	sed -n '2,32p' "$0" >&2
	exit 1
fi
shift

cd "$(dirname "$0")/.." || exit 1
OUTDIR="${OUTDIR:-measurements/$(date +%F)-s1-${BLOCK}}"

# Build rather than `go run`: inside a capture window the compile would burn
# recording time before the first pulse.
BIN="$PWD/measurements/.bin/ttlbox-timing"
mkdir -p "$(dirname "$BIN")"
echo "+ go build -o ${BIN#"$PWD"/} ./cmd/ttlbox-timing"
go build -o "$BIN" ./cmd/ttlbox-timing || exit 1

# Ask the block how long it is, so the two can never drift apart.
SEQ_S=$("$BIN" "$BLOCK" --seconds "$@") || exit 1
REC_S=$((SEQ_S + 2 * BBTK_MARGIN_S))

echo
echo "Block:    $BLOCK $*"
echo "Session:  $OUTDIR/"
echo "TTL box:  $TTLBOX_PORT"
echo "BBTK:     ${BBTK_PORT:-auto (bbtk-capture default)}"
echo "Window:   ${SEQ_S}s + 2x${BBTK_MARGIN_S}s margin = ${REC_S}s recorded"
echo
echo "Wiring:   line 0 -> D30 -> TTLin2 (and -> D22)"
echo "          line 1 -> D31 -> TTLin1 (and -> D23)"
echo "          GND    -> GND  (required)"
echo

BASE="$OUTDIR/bbtk-$BLOCK"
CMD=("$BIN" "$BLOCK" --port "$TTLBOX_PORT" --out "$OUTDIR" "$@")

if [ "$DRY_RUN" = 1 ]; then
	echo "+ $BBTK_CAPTURE_BIN -d $REC_S $BASE -- ${CMD[*]}"
	echo
	"${CMD[@]}" -n
	exit 0
fi

if [ ! -e "$TTLBOX_PORT" ]; then
	echo "error: $TTLBOX_PORT not found — is the box plugged in?" >&2
	exit 1
fi
if ! command -v "$BBTK_CAPTURE_BIN" >/dev/null 2>&1 && [ ! -x "$BBTK_CAPTURE_BIN" ]; then
	echo "error: '$BBTK_CAPTURE_BIN' is not executable or not on PATH" >&2
	echo "       set BBTK_CAPTURE_BIN to its full path" >&2
	exit 1
fi
# Check for -- support before touching the device: a bbtk-capture built before
# it would read "--" as a stray argument and reject the whole command line.
if ! "$BBTK_CAPTURE_BIN" -h 2>&1 | grep -q -- '-- command'; then
	echo "error: '$BBTK_CAPTURE_BIN' cannot launch the block itself" >&2
	echo "       its usage does not mention '-- command'. Rebuild bbtkv3 and" >&2
	echo "       point BBTK_CAPTURE_BIN at the new binary." >&2
	exit 1
fi

mkdir -p "$OUTDIR"
LOG="$OUTDIR/capture.log"

PORT_ARGS=""
[ -n "${BBTK_PORT:-}" ] && PORT_ARGS="-p $BBTK_PORT"

echo "+ $BBTK_CAPTURE_BIN $PORT_ARGS -d $REC_S $BASE -- ${CMD[*]}"
# Both streams are teed: the device spends 11-40 s setting up before the first
# pulse, and a silent terminal through that is indistinguishable from a hang.
# shellcheck disable=SC2086
"$BBTK_CAPTURE_BIN" $PORT_ARGS -d "$REC_S" "$BASE" \
	-- "${CMD[@]}" \
	2> >(tee "$LOG" >&2) | tee "$OUTDIR/$BLOCK-stimulus.log"
rc=$?

if [ "$rc" -ne 0 ]; then
	printf '\n!! capture FAILED (exit %d) — see %s\n' "$rc" "$LOG" >&2
	printf '   A non-zero status means the block failed or was aborted, or the\n' >&2
	printf '   capture could not reach the device. The recording cannot be\n' >&2
	printf '   salvaged either way — re-run. If the device could not be opened,\n' >&2
	printf '   set BBTK_PORT (bbtk-detect-port will find it).\n' >&2
	exit 1
fi

printf '\nDone. Files in %s/\n' "$OUTDIR"
printf '\nRead the numbers with:\n'
printf '  ./measurements/analyse-timing.py %s          # the box'"'"'s own view\n' "$OUTDIR"
printf '  events-stats -event1 TTLin2 %s-001-events.csv  # the BBTK'"'"'s view\n' "$BASE"
