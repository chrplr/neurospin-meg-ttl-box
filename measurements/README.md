# Measurements

Raw data and reproduction instructions for the numbers in [`../TIMING.md`](../TIMING.md).

Every figure quoted there comes from a session directory here. Blocks record raw
per-trial rows and summarise nothing; the arithmetic lives in
[`analyse-timing.py`](analyse-timing.py), so a result can be re-cut or argued
with without spending hardware time again.

## The wiring

One harness serves the whole battery. Build it once.

| from | to | why |
|---|---|---|
| box **D30** (output line 0) | BBTK **TTLin2** *and* box **D22** (input 0) | the output, watched from outside *and* timestamped by the box itself |
| box **D31** (output line 1) | BBTK **TTLin1** *and* box **D23** (input 1) | a second line, for code-change and trigger duty |
| BBTK **TTLout1** | box **D24** (input 2) | the instrument's own stimulus, into an input |
| GND | GND | **required** — TTL without a shared reference gives unreliable edges |

Session 3 needs only `D30 → D22` and the common ground.

Two facts about the loopback that the data will otherwise look wrong against:

- Inputs are `INPUT_PULLUP` and reported **inverted**, so a line driven HIGH
  reads as 0 and a pulse appears as a `1 → 0 → 1` excursion. Edge *timing* is
  unaffected, which is all any block uses.
- BBTK smoothing does not apply here at all. It affects only `Opto*` and `Mic*`
  channels, never TTL inputs — worth remembering if a future session ever
  measures a photodiode instead, where it adds ~20 ms to recorded durations.

## Running a session

Build is automatic; each script compiles `cmd/ttlbox-timing` first.

```bash
# Session 1 — the box emits, a BBTKv3 observes (DSC capture mode)
./run-session1.sh -n widths          # dry run first: prints the plan, records nothing
./run-session1.sh widths             # then for real
./run-session1.sh codechange
./run-session1.sh latency
./run-session1.sh drift

# Session 2 — the BBTK answers, the box timestamps (DSRE mode)
./run-session2.sh -n
./run-session2.sh

# Session 3 — no instrument, loopback only
./run-session3.sh -n
./run-session3.sh
```

Then, for each session directory:

```bash
./analyse-timing.py measurements/2026-08-07-s1-widths      # the box's own view
events-stats -event1 TTLin2 <session>/bbtk-widths-001-events.csv   # the BBTK's view
```

`analyse-bbtk.py` remains for captures in the older fixed-block format; the
2026-08-05 session is read with it.

## The blocks

| block | question | needs |
|---|---|---|
| `widths` | realised pulse width vs requested, 1–200 ms | BBTK capture (optional) |
| `codechange` | does changing a trigger code expose an intermediate value? | nothing (BBTK corroborates) |
| `latency` | host write → line moves, with the tail | nothing (BBTK corroborates) |
| `drift` | device clock rate against host and BBTK | nothing (BBTK is the third opinion) |
| `respond` | input path, notification latency, closed loop | BBTK in DSRE mode |
| `splitwrite` | what a command split across two writes does | nothing |
| `overflow` | sustained load and the event-queue overflow flag | nothing |

`ttlbox-timing <block> --help` explains what each one is testing and why.

## Before touching the hardware

- `bbtk-detect-port`, and `bbtkv3/tools/ftdi-check.sh` — a failing USB cable
  still enumerates and looks entirely healthy.
- `bbtk-input-check` — confirm the box's pulses actually reach TTLin1/TTLin2
  before committing to a capture window.
- Continuity on the shared ground.
- `--seconds` sizes the capture window from the block itself. **A BBTK capture
  window is fixed before recording starts and an interrupted capture is
  unrecoverable** — nothing is written and the run is simply lost.

Things that cost a session if you get them wrong:

- `bbtk-trigger-response` needs **`-any`**. The default pattern match wants an
  exact match of the whole input port, every other line low, and otherwise never
  fires — silently. `run-session2.sh` passes it.
- Stop a streaming bbtkv3 tool with the documented keystroke, not a kill, or the
  device is left mid-stream; recover with `bbtk-send-break`.
- `OCHK` (output line check) **wedges a BBTKv3** and only a power cycle
  recovers it. Nothing here uses it; do not reach for it while debugging.

## Reading a result

Two habits the analysis enforces, and which the write-up should keep:

**A block with a positive control is only as good as its control.**
`codechange` runs the legacy two-command path alongside the atomic one
specifically so the apparatus can be seen detecting the artefact opcode 17 is
claimed to prevent. If the legacy arm comes back clean, the atomic arm's clean
result means nothing and the analysis says so instead of reporting a finding.

**Two independent methods agreeing is the standard.** The existing figures were
established that way and the new ones should be too: `latency` (internal,
absolute) against the BBTK's onset-to-onset intervals (external); the box's
`micros()` width against the BBTK's 0.25 ms width; the drift fit against the
`respond` regression slope, which measures the same clock ratio by an unrelated
route.

## Sessions

| directory | date | what |
|---|---|---|
| [`2026-08-05-bbtk/`](2026-08-05-bbtk/) | 2026-08-05 | first session: pulse width at 5/10/20 ms, two-line skew, host→device latency. Output side only. |
