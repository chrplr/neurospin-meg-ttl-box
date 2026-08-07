#!/bin/bash
# run-session2.sh — sweep the BBTK's response delay and measure the input path.
#
# Session 2 reverses the roles of session 1: the BBTK emits and the box
# timestamps. The BBTK runs a Digital Stimulus Response Echo program, which
# answers a trigger on TTLin1 with a pulse on TTLout1 after a programmed delay,
# entirely inside its own firmware. It records nothing itself — DSRE and capture
# mode are mutually exclusive — so the box is the only timestamper, which is
# what makes this a measurement OF the box.
#
# Sweeping the delay and regressing the interval the box measured on the delay
# the BBTK was told to use gives a slope (the box's clock rate against the
# BBTK's, over a 500x lever arm), an intercept (the fixed latency of the pair)
# and a residual spread (an upper bound on the firmware's input detection).
#
# Usage:
#   ./run-session2.sh              # full sweep, then the polling comparison
#   ./run-session2.sh -n           # dry run: print every step, touch nothing
#
# Environment overrides:
#   TTLBOX_PORT   TTL box serial port   (default: /dev/ttyACM0)
#   BBTK_PORT     BBTK serial port      (passed through to bbtk-trigger-response)
#   BBTK_DSRE_BIN path to the tool      (default: bbtk-trigger-response on PATH)
#   RTS           delays to sweep, ms   (default: "0 10 20 50 100 200 500")
#   POLLS         poll intervals, ms    (default: "0 1 2 5 10")
#   TRIALS        trials per delay      (default: 100)
#   OUTDIR        session directory     (default: measurements/<date>-s2-respond)
#
# WIRING:
#     box D31 (line 1) -> BBTK TTLin1  and  box D23 (input 1)
#     BBTK TTLout1     -> box D24 (input 2)
#     GND              -> GND     <- required
#
# Two things cost a wasted session if you get them wrong, both handled below:
# -any is mandatory (the default pattern match wants an exact match of the whole
# input port and otherwise never fires, silently), and the program must be
# stopped with a keystroke rather than a kill, or the device is left mid-stream
# and needs bbtk-send-break.

set -u
set -o pipefail

TTLBOX_PORT="${TTLBOX_PORT:-/dev/ttyACM0}"
BBTK_DSRE_BIN="${BBTK_DSRE_BIN:-bbtk-trigger-response}"
RTS="${RTS:-0 10 20 50 100 200 500}"
POLLS="${POLLS:-0 1 2 5 10}"
TRIALS="${TRIALS:-100}"
PULSE_MS="${PULSE_MS:-20}"

DRY_RUN=0
[ "${1:-}" = "-n" ] && DRY_RUN=1

cd "$(dirname "$0")/.." || exit 1
OUTDIR="${OUTDIR:-measurements/$(date +%F)-s2-respond}"

BIN="$PWD/measurements/.bin/ttlbox-timing"
mkdir -p "$(dirname "$BIN")"
echo "+ go build -o ${BIN#"$PWD"/} ./cmd/ttlbox-timing"
go build -o "$BIN" ./cmd/ttlbox-timing || exit 1

PORT_ARGS=""
[ -n "${BBTK_PORT:-}" ] && PORT_ARGS="-p $BBTK_PORT"

echo
echo "Session:  $OUTDIR/"
echo "TTL box:  $TTLBOX_PORT"
echo "Delays:   $RTS ms, $TRIALS trials each"
echo "Polls:    $POLLS ms (at the middle delay)"
echo
echo "Wiring:   line 1 -> D31 -> TTLin1 (and -> D23)"
echo "          TTLout1 -> D24"
echo "          GND     -> GND  (required)"
echo

# dsre_start programs the BBTK and leaves it running in the background.
#
# Its stdin is a pipe we hold open: with stdin not a terminal the tool waits for
# Enter rather than Esc, so writing a newline stops it the documented way and it
# sends the break character on the way out. Killing it instead would leave the
# device mid-stream.
DSRE_PID=""
FIFO=""
dsre_start() {
	local rt="$1"
	FIFO="$(mktemp -u)"
	mkfifo "$FIFO" || return 1
	# shellcheck disable=SC2086
	"$BBTK_DSRE_BIN" $PORT_ARGS -any -i TTLin1 -o TTLout1 -rt "$rt" -d "$PULSE_MS" \
		<"$FIFO" >>"$OUTDIR/dsre.log" 2>&1 &
	DSRE_PID=$!
	exec 9>"$FIFO"
	rm -f "$FIFO"
	# Programming is one command per second plus a pause before the run command.
	echo "  programming the BBTK for rt=${rt} ms (about 10 s)..."
	sleep 12
	if ! kill -0 "$DSRE_PID" 2>/dev/null; then
		echo "error: bbtk-trigger-response exited during programming — see $OUTDIR/dsre.log" >&2
		exec 9>&- || true
		return 1
	fi
}

dsre_stop() {
	[ -z "$DSRE_PID" ] && return 0
	printf '\n' >&9 2>/dev/null
	exec 9>&- 2>/dev/null
	for _ in $(seq 1 50); do
		kill -0 "$DSRE_PID" 2>/dev/null || break
		sleep 0.2
	done
	if kill -0 "$DSRE_PID" 2>/dev/null; then
		echo "  warning: the DSRE program did not stop on its own; sending SIGTERM." >&2
		echo "           run bbtk-send-break before the next session." >&2
		kill "$DSRE_PID" 2>/dev/null
	fi
	wait "$DSRE_PID" 2>/dev/null
	DSRE_PID=""
}
trap 'dsre_stop' EXIT

if [ "$DRY_RUN" = 1 ]; then
	for rt in $RTS; do
		echo "+ $BBTK_DSRE_BIN $PORT_ARGS -any -i TTLin1 -o TTLout1 -rt $rt -d $PULSE_MS"
		echo "+ $BIN respond --rt $rt --trials $TRIALS --tag -rt$rt --out $OUTDIR"
	done
	for poll in $POLLS; do
		echo "+ $BIN respond --rt 100 --poll $poll --trials $TRIALS --tag -poll$poll --out $OUTDIR"
	done
	for poll in $POLLS; do
		echo "+ $BIN respond --rt 100 --poll $poll --legacy --trials $TRIALS --tag -poll$poll --out $OUTDIR"
	done
	echo
	"$BIN" respond -n
	echo "(dry run — nothing recorded)"
	trap - EXIT
	exit 0
fi

if [ ! -e "$TTLBOX_PORT" ]; then
	echo "error: $TTLBOX_PORT not found — is the box plugged in?" >&2
	exit 1
fi
if ! command -v "$BBTK_DSRE_BIN" >/dev/null 2>&1 && [ ! -x "$BBTK_DSRE_BIN" ]; then
	echo "error: '$BBTK_DSRE_BIN' is not executable or not on PATH" >&2
	exit 1
fi

mkdir -p "$OUTDIR"

# --- the delay sweep: slope, intercept and detection bound ------------------
for rt in $RTS; do
	echo
	echo "=== delay ${rt} ms ==="
	dsre_start "$rt" || exit 1
	"$BIN" respond --port "$TTLBOX_PORT" --out "$OUTDIR" \
		--rt "$rt" --trials "$TRIALS" --tag "-rt$rt" | tee -a "$OUTDIR/respond.log"
	dsre_stop
done

# --- the poll sweep: what the host's polling costs on top -------------------
MID_RT=100
echo
echo "=== poll-interval sweep at ${MID_RT} ms ==="
dsre_start "$MID_RT" || exit 1
for poll in $POLLS; do
	echo "--- poll ${poll} ms ---"
	"$BIN" respond --port "$TTLBOX_PORT" --out "$OUTDIR" \
		--rt "$MID_RT" --poll "$poll" --trials "$TRIALS" --tag "-poll$poll" |
		tee -a "$OUTDIR/respond.log"
done

# --- the same stimuli through the pre-v1 path, for the comparison ----------
echo
echo "=== legacy button-poll path at ${MID_RT} ms ==="
for poll in $POLLS; do
	"$BIN" respond --port "$TTLBOX_PORT" --out "$OUTDIR" --legacy \
		--rt "$MID_RT" --poll "$poll" --trials "$TRIALS" --tag "-poll$poll" |
		tee -a "$OUTDIR/respond.log"
done
dsre_stop

printf '\nDone. Files in %s/\n' "$OUTDIR"
printf '\nRead the numbers with:\n  ./measurements/analyse-timing.py %s\n' "$OUTDIR"
