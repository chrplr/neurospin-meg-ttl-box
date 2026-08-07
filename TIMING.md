# Device capabilities and measured timing

What this box can and cannot do, with numbers rather than estimates. Every
figure below is either measured on hardware or marked as not yet measured, with
the block that measures it named. Nothing here is an estimate presented as a
result.

Measurements: Arduino Mega 2560 R3, firmware protocol v1 (`caps 0x03`).
Raw data and reproduction instructions in [`measurements/`](measurements/).

| session | date | covered |
|---|---|---|
| [`2026-08-05-bbtk`](measurements/2026-08-05-bbtk/) | 2026-08-05 | output side: pulse width at 5/10/20 ms, two-line skew, host→device latency |
| [`2026-08-07-s3-loopback`](measurements/2026-08-07-s3-loopback/) | 2026-08-07 | loopback only, no instrument: host round trip idle vs loaded (n=10 000 each), the split-write stall, an hour of sustained load |
| (in [dlp-io8-g](https://github.com/chrplr/dlp-io8-g)) | 2026-08-07 | scope: pulse width at 5–50 ms under three host conditions, and a head-to-head against a DLP-IO8 |
| (in [dlp-io8-g](https://github.com/chrplr/dlp-io8-g)) | 2026-08-07 | Analog Discovery 3, streamed: pulse width at 1–50 ms, n=1000 per width |

---

## Summary

| Property | Value | How established |
|---|---|---|
| TTL logic level | 5 V | design |
| Output lines | 8 (D30–D37) | — |
| Input lines | 8 (D22–D29), `INPUT_PULLUP` | — |
| Simultaneous two-line write | **atomic**, skew < 250 µs | BBTKv3, 20 trials |
| Pulse width, 1–50 ms | lands in **(w−2.05, w+0.04] ms** | AD3, n=1000 per width, 6 widths |
| Pulse width bias | **−0.37 to −0.47 ms** (systematically short) | as above |
| Pulse width spread | **2.048 ms**, two uniform bands; parameter-free model of `millis()` | as above |
| Host→device latency | **not measured** (~1.5 ms is an estimate; see below) | neither available method measures it |
| Write latency vs a DLP-IO8 | **tens of µs**; sign and magnitude not resolved | scope n≈98 and AD3 n=1631; the estimator does not cancel the host's contribution |
| Pulse width, immunity to host load | **spread unchanged** by CPU load or real-time priority | scope, n=50 per width, 3 conditions |
| Host round trip, idle | 2.64 ms median, 6.01 ms max | loopback, n=10 000 |
| Host round trip, under CPU load | 3.71 ms median, **25.29 ms max** | loopback, n=10 000 |
| Input event timestamp | few µs of the edge | firmware `micros()`, by construction |
| Button poll (legacy path) | ≥ 5 ms, host-limited | by construction |
| Pulse width above 50 ms | **not yet measured** | block `widths` |
| Output port latch-up | observed once: whole port dead, protocol still answering, cleared only by a power cycle | 2026-08-07 session |
| Trigger-code *change* atomicity | **not yet measured** | block `codechange` |
| Split command stalls the firmware | **+17.5 ms** on a pulse in flight | block `splitwrite`, n=50 |
| Device clock rate error | **≈ +900 to +975 ppm** (~3.3 s/hour) | two independent routes: `latency` runs agreeing to 6 ppm, and the 905 ppm width-vs-AD3 slope |
| Device→host notification latency | **not yet measured** | block `respond` |
| Closed-loop latency | **not yet measured** | block `respond` |
| Sustained load, 1 h at 20 Hz | 2 events missing of 141 088 (0.0014 %) | block `overflow` |
| Queue overflow reporting | flagged in 11 of 11 stall windows, 0 of 12 drain windows | block `overflow` |
| Real-time priority | **not yet measured** | needs `RLIMIT_RTPRIO` > 0 on the host |

The unmeasured rows are not oversights being confessed: each names a block in
[`cmd/ttlbox-timing`](cmd/ttlbox-timing/) that measures it, with the protocol
written down in [`measurements/README.md`](measurements/README.md).

---

## Output

### A simultaneous two-line write is atomic

On the Mega 2560, output pins D30–D37 land on a single AVR port (`PORTC7`–`PORTC0`,
note the **reversed** bit order). Opcode 17 assigns the whole port in one
instruction, so no intermediate value ever reaches the pins and a recording
device cannot latch a half-written code.

**Measured:** one command pulsed D30 and D31 while a BBTKv3 watched both. All 20
trials placed the two rising edges in the **same 0.25 ms sample** — skew below
250 µs, measured by an instrument independent of the firmware.

**What that does not yet establish.** Pulsing two lines *from zero* cannot
distinguish "atomic" from "consistently ordered", and it is not the case that
matters. A real trigger-code change makes one line rise as another falls, and
the older two-command path (`set_high_mask` then `set_low_mask`, opcodes 13/14)
leaves the port at `previous | mask` in between, for up to a USB frame — a
valid-looking but *wrong* code that an amplifier sampling at 1 kHz can record.

The `codechange` block tests exactly that transition, and runs the legacy pair
alongside as a **positive control**: the same apparatus must be seen catching
the artefact before "no artefact on opcode 17" means anything. Until it has been
run, clients should still feature-detect `CAP_ATOMIC_PORT` and prefer opcode 17
— the argument for it is sound, it is simply not yet demonstrated on this axis.

### Pulse width runs short — by design, and predictably

The firmware times pulses itself (`g_pulse_end = millis() + width`, dropped from
the main loop), so width does not absorb host scheduling jitter. It runs short,
and by more than the obvious model predicts.

**A requested *w* ms pulse lands in (w−2.05, w+0.04] ms.** Measured with a
Digilent Analog Discovery 3 at 100 kS/s, n=1000 per width:

| requested | min | median | max | spread | median error |
|---|---|---|---|---|---|
| 1 ms | 0.014 | 0.535 | 1.040 | 1.025 | −0.465 ms |
| 2 ms | 0.048 | 1.541 | 2.057 | 2.009 | −0.459 ms |
| 5 ms | 3.092 | 4.578 | 5.142 | 2.050 | −0.423 ms |
| 10 ms | 8.212 | 9.611 | 10.261 | 2.049 | −0.390 ms |
| 20 ms | 18.467 | 19.581 | 20.508 | 2.041 | −0.419 ms |
| 50 ms | 48.196 | 49.635 | 50.239 | 2.043 | −0.367 ms |

**This is a bias, not noise — it cannot be averaged away.** If a paradigm needs a
true 5 ms pulse, request 6 ms.

#### Why the spread is 2 ms, not 1

Because `millis()` truncates, the obvious model says the realised width is
uniform on [w−1, w] — a flat histogram exactly 1 ms wide. The measured spread is
twice that, at every width from 2 ms up. The reason is that **`millis()` does not
tick at 1 ms.**

Timer0 on a 16 MHz AVR overflows every 1024 µs, and `wiring.c` carries a
fractional accumulator (`FRACT_INC` 3, `FRACT_MAX` 125) adding a catch-up
millisecond every ~41.7 overflows — keeping the clock accurate on average at the
price of advancing by 2 about one time in 42. So the number of overflows needed
to reach `millis() + w` depends on the accumulator's phase at pulse onset: for
some phases *n* suffice, for the rest *n+1*. Each case gives a uniform band
1024 µs wide, and the width is a mixture of two of them — **2.048 ms across**.

The model has **no free parameters**; everything in it is fixed by `wiring.c` and
the 16 MHz clock. Its sharpest prediction is the fraction of trials falling in
the early band, which swings non-monotonically with width:

| requested | measured | model |
|---|---|---|
| 2 ms | 2.0% | 2.4% |
| 5 ms | 8.6% | 9.6% |
| 10 ms | 20.9% | 21.5% |
| 20 ms | 43.7% | 45.6% |
| 50 ms | 14.2% | 15.1% |

A two-sample Kolmogorov-Smirnov test passes at four of six widths outright, and
at all six once a single scalar offset is removed (D = 0.015–0.033 against a
0.043 critical value) — so what it rejects is a shift, not a shape. Fitting that
offset across widths gives `+17.6 µs + 905 ppm × width`: the constant is the
firmware's own gap between raising the line and reading `millis()` plus one loop
pass to notice the end, and the 905 ppm is this board's ceramic resonator running
slow, well inside its ±5000 ppm specification.

A `micros()`-based pulse timer would remove both the bias and the doubled spread;
nobody has needed it yet. Note that the accumulator is a property of the Arduino
core, not of this firmware, so any `millis()`-timed pulse on any AVR board has
the same 2 ms floor.

*(An earlier BBTKv3 session, 20 pulses per width at 0.25 ms sampling, gave mean
errors of −0.53, −0.68 and −0.69 ms at 5, 10 and 20 ms — consistent with the
above, but too coarse to see the band structure.)*

### The width does not care what the host is doing

The same scope measurement under three host conditions, spread in ms:

| condition | 5 ms | 10 ms | 20 ms | 50 ms |
|---|---|---|---|---|
| idle | 1.96 | 2.00 | 2.01 | 1.93 |
| under CPU load (`stress-ng --cpu 0`) | 1.55 | 2.01 | 1.96 | 1.95 |
| load + `chrt -f 50` | 1.92 | 1.93 | 1.90 | 1.92 |

Twelve figures, all 1.9–2.0 ms. CPU load does not move it and real-time priority
does not improve it, because the firmware times the pulse and the host is not in
that loop at any point.

The contrast is with a device that has no pulse timer. Measured identically on
the same scope and the same host, a DLP-IO8's width — which is the interval
between two host writes — spreads 0.05–0.12 ms idle, **1.8–4.8 ms under the same
load**, and returns to 0.07–0.12 ms under `chrt`. Correctly configured it is far
more precise than this box; carelessly configured it is far worse. This box
offers ~2 ms you cannot improve; that one offers 0.1 ms conditional on system
administration. See
[dlp-io8-g/measurements](https://github.com/chrplr/dlp-io8-g).

Widths from 1 to 50 ms are now covered at n=1000 each. Above 50 ms is still
untested, though the model has no width-dependent term and there is no reason to
expect a change. A second witness remains available and unused: the box's own
loopback timestamps both edges with `micros()`, at 4 µs rather than an external
instrument's resolution, which would test the model against the device's own
clock rather than against the host's.

### The output port can fail silently, and serial will not tell you

Observed once, on 2026-08-07, part-way through a measurement run after roughly
19 000 pulses: **the entire output port stopped driving.** What makes it worth
recording is the failure signature.

| check | result |
|---|---|
| `get_info` (opcode 1) | answered normally — version 1, caps 0x03 |
| `set_port(0x01)` (opcode 17, no pulse machinery) | nothing on the line |
| `set_port(0xFF)` | nothing on any line — the whole port |
| DTR reset into the bootloader | **did not recover it** |
| unplug and replug USB | recovered it completely |

A reset re-runs `setup()`, including `pinMode(OUT_PINS[i], OUTPUT)` on all eight
lines, and that was not enough; only removing power was. That is the signature of
I/O latch-up rather than firmware state or permanent damage. The cause is not
known — nothing was connected to the line but a high-impedance instrument input,
and the same wiring had run for hours beforehand. It is reported as an
observation, not as a characterised failure mode.

**The operational consequence does not depend on the cause.** A pre-flight check
that only talks to the box over serial reports a perfectly healthy device while
no triggers whatsoever reach the amplifier — an entire session with no usable
event markers, discovered in analysis. Any check worth running must observe the
line **electrically**. This box can do that by itself: with D30 jumpered to D22,
pulse the line and confirm the input event arrives. If the port is dead the box
fails to see its own edge, and the check fails before the participant is in the
scanner rather than after.

---

## Input

Inputs are configured `INPUT_PULLUP` and reported inverted, so a line pulled LOW
reads as 1 ("pressed") and an **unconnected line reads 0**. Unlike devices
without pull-ups, an unwired box cannot generate spurious responses.

All 8 lines are sampled in a single `PINA` read, so they share one instant
rather than being smeared across ~40 µs of sequential `digitalRead` calls.

### Two ways to read responses

**Polling (`get_response_button_mask`, opcode 20)** — resolution is your polling
interval plus a USB round trip. It can only establish that a press had already
happened by the time you asked. Adequate for detecting a response, **not** for
measuring one.

**Timestamped events (opcodes 21–24, needs `CAP_TIMESTAMPS`)** — the firmware
samples every loop iteration (a few µs) and records `micros()` at the
transition. Your polling then determines only when you *learn* of the press, not
the instant recorded.

### The receive path has never been measured externally

This is the honest state of things. Everything above about the input side is
argued from the firmware source, not from data: no instrument has ever driven
these inputs and checked what the box reported. Three quantities are unknown.

- **Are the timestamps metrically right, or only precise?** A 4 µs tick means
  nothing if the clock it counts is wrong. The `respond` block sweeps a BBTKv3's
  programmable response delay from 0 to 500 ms and regresses what the box
  measured on what the BBTK was told to do. The slope is the box's clock rate
  against the instrument's over a 500× lever arm; the intercept is the fixed
  latency of the pair.
- **How long does the firmware take to notice an edge?** Bounded above by one
  main-loop iteration, which is microseconds — except when it is not, see the
  split-write hazard below. The residual spread of that same regression bounds
  it externally, since nothing else in the chain varies.
- **How long until the host learns?** Detection plus queue plus the host's poll
  interval plus a USB round trip. The `respond` block measures it directly and
  reruns the same physical stimuli through the legacy polling path, which turns
  "timestamps are better" into a number.

### Reaction-time accuracy: sub-millisecond, not microsecond

The edge is timestamped to within a few microseconds. But converting a device
`micros()` value into host time costs the accuracy of the clock-offset estimate,
which is bounded by the asymmetry of the sync round trip — a few hundred
microseconds. Quoting the 4 µs `micros()` tick as the RT accuracy would be
wrong.

That bound is now measured rather than asserted: [`clock.go`](clock.go) samples
the offset repeatedly and fits offset and rate together, and **the residuals of
that fit are the accuracy figure**. Every block records its clock samples
alongside its data for exactly this reason.

So: far better than the ≥5 ms floor of polling, but do not claim microseconds.

### Queue overflow is reported, not hidden

The firmware holds 32 events. If the host does not drain them it sets a sticky
flag, surfaced to clients. An overflow means presses were **lost, not delayed**,
so an affected trial should be treated as suspect. In practice it takes a
mechanical button chattering — hence the debounce opcode, which is **off by
default** because fibre-optic pads do not bounce and suppressing real
transitions is worse than reporting extra ones.

The `overflow` block overruns the queue on purpose and checks the flag comes
back, over a session-length run.

---

## Host→device latency

The time from the host issuing a command to the line actually moving. Measured
twice, by unrelated methods:

| method | min | median | max |
|---|---|---|---|
| Firmware timestamp of a loopback edge vs. host clock (50 trials) | 802 µs | **1.44 ms** | 2.05 ms |
| BBTK onset-to-onset interval minus requested ISI (80 pulses) | — | **~1.5 ms** | — |

The spread is one USB full-speed frame (1 ms) plus the ~174 µs two command bytes
take on the 16u2↔2560 UART at 115200 baud.

**Neither method measures what this section claims, and their agreement is not
corroboration.**

The onset-to-onset method cannot measure latency at all. For pulses commanded at
times c[i] and observed at c[i] + L[i], the measured interval is

    o[i+1] − o[i] = (c[i+1] − c[i]) + (L[i+1] − L[i])

and the latency cancels. With a constant latency the measured interval equals
the commanded one exactly, however large that latency is. What the +1.5 ms
actually shows is the host loop's per-iteration overhead — which is dominated by
a USB round trip, hence a number that looks like a latency and agrees with one.

The loopback method does measure latency, but pays for it: The loopback method has to convert a device `micros()` value
into host time, which costs the accuracy of the clock-offset estimate — and that
estimate is bounded by the asymmetry of a `get_micros` round trip, whose floor on
this link is about 2.4 ms (one USB frame each way plus four reply bytes at
115200). The offset can therefore be wrong by more than the ~1.5 ms being
measured, and taking more samples does not help, because the floor is structural.

Measured on 2026-08-07: the best round trip achieved in 20 tries was 2.003 ms in
one run and 0.984 ms in another, giving offset bounds of ±1.00 ms and ±0.49 ms.
Those two runs then reported absolute latencies of 1.979 ms and 1.361 ms — a
difference explained entirely by their differing offset estimates, and in the
wrong direction, since the 1.361 ms run was the one under CPU load. That is the
symptom that makes the limitation undeniable.

**So ~1.5 ms is an estimate from first principles — one USB frame plus the
~174 µs two command bytes take on the 16u2 UART — and not a measurement.** It is
almost certainly the right order of magnitude, and it is consistent with
everything measured since, but nothing here establishes it.

Measuring it properly needs something this setup does not have: an event the
host can produce at a time it knows exactly, visible to the same instrument.
A parallel-port `outb` is the classic choice (sub-microsecond, and the host knows
when it executed), a memory-mapped GPIO write on an SBC is equivalent, and a USB
protocol analyser would do it by timestamping the packet on the wire. With any
of those, the order-swapped comparison used for the DLP head-to-head below gives
the absolute figure directly. Without one, only differences and bounds are
available.

For comparing conditions, or for any figure meant to be quoted, use a quantity
that stays inside one clock — see the next section.

**This latency is irreducible from the host side** and is the part of a reaction
time that firmware timestamping cannot remove. It is why a trigger's *onset*
should be issued as close to stimulus onset as possible, and why a parallel port
(a single sub-microsecond `outb`, no USB) remains the reference for trigger
timing.

Fifty trials describe a median well and a tail not at all, and for MEG the tail
is what corrupts a trial. The `latency` block runs thousands, reports to the
99.9th percentile, and repeats under host conditions — because the figure that
matters is the one on a stimulus PC that is also decoding video.

### Host round trip, and what CPU load does to it

The safe way to compare conditions is a quantity that never leaves the host
clock: from issuing the command to learning of the resulting edge. It bounds the
write latency from above and needs no offset estimate at all.

**Measured 2026-08-07**, 10 000 pulses per condition, loopback D30→D22:

| condition | p50 | p99 | p99.9 | max |
|---|---|---|---|---|
| idle | 2.64 | 5.45 | 5.79 | 6.01 ms |
| CPU load (`stress-ng --cpu 0`) | 3.71 | 6.83 | 8.77 | **25.29 ms** |

The inter-onset intervals, measured purely in device time and so equally free of
any offset, agree: p99.9 rises from 21.2 ms to 26.8 ms under load against a
commanded 20 ms.

A 25 ms worst case is a corrupted trial, and it is the host's doing rather than
the box's. Real-time priority was not measured — `RLIMIT_RTPRIO` was 0 on the
test machine — and is the obvious next comparison.

---

## A command must be written in one call

The firmware's main loop reads

```c
if (Serial.available() < 1) return;
int opcode = readU8Blocking();
```

so the opcode never blocks — but every *argument* does, in an unbounded spin:

```c
while (Serial.available() < 1) { /* wait */ }
```

Nothing else runs during that spin. Not `sampleButtons()`, which timestamps
inputs; not the pulse teardown, which ends an active TTL pulse. So a command
whose argument is one USB frame behind its opcode should stretch any pulse in
flight and delay the timestamp of any input that changes meanwhile.

**Client contract: build the whole command and write it once.** The typed
methods in this package already do (`box.go`, `tx`), so ordinary code is safe;
`SendRaw` is the only way to violate it, and it is documented as such.

**Measured 2026-08-07**, no instrument required. A 5 ms pulse was issued, then a
lone opcode byte 2 ms later with its argument withheld for 20 ms:

| arm | realised width (median) |
|---|---|
| command written once | 4.59 ms |
| opcode and argument split | **22.14 ms** |

An extension of +17.5 ms against +17.0 predicted, over 50 trials per arm, with
the two distributions not overlapping at all. The stall is real, and it is as
long as you make it: the spin has no timeout.

---

## Not measured

Stated for honesty; do not quote these as verified. Unlike the rows in the
summary table, nothing in this repository currently measures them.

- **The 16 remaining digital lines** and any use of the box beyond 8-in/8-out.
  Every measurement uses lines 0–2 of each bank.
- **Temperature dependence.** The `drift` block measures rate error over a run,
  but nothing varies the temperature or repeats across ambient conditions.
- **Anything about the FORP box or the STI box** either side of this one. The
  measurements characterise the interface, not the chain it sits in.
- **The absolute host→device latency, to better than an order of magnitude, from
  the device alone.** This one is not a gap waiting to be filled but a limit:
  converting a device timestamp to host time costs the clock-offset estimate, and
  that estimate is floored by the ~2.4 ms round trip of the sync exchange itself.
  No amount of sampling improves it. An absolute figure has to come from an
  external instrument; from the box alone, quote the host round trip instead.

---

## A note on USB latency under Linux

A tip that circulates for serial devices is

```
echo 1 | sudo tee /sys/bus/usb-serial/devices/ttyUSB0/latency_timer
```

**It does not apply to this box.** `latency_timer` is an FTDI (`ftdi_sio`) knob,
exposed for devices on the `usb-serial` bus. The Mega 2560 presents a USB CDC
ACM interface and binds to `cdc_acm`, appearing as `/dev/ttyACM*`; it has no
`latency_timer` and does not appear under `/sys/bus/usb-serial/` at all. Check
before assuming otherwise:

```
basename $(readlink -f /sys/class/tty/ttyACM0/device/driver)   # -> cdc_acm
```

The tip *is* relevant to a BBTKv3, which is an FTDI device on `/dev/ttyUSB*` —
but the BBTK timestamps internally and uploads afterwards, so its USB latency is
not in any timing path that matters here either.

If a host-side tuning knob does turn out to matter on a given stimulus PC, the
way to find out is to run the `latency` block with and without it and compare
the distributions, not to apply the setting and assume.

---

## Reproducing

Everything is in this repository. The measurement harness is
[`cmd/ttlbox-timing`](cmd/ttlbox-timing/), driven by the scripts in
[`measurements/`](measurements/):

```bash
# no instrument needed: jumper D30 -> D22, common ground
./measurements/run-session3.sh -n        # dry run: prints the plan, records nothing
./measurements/run-session3.sh

# with a BBTKv3 — see measurements/README.md for the wiring
./measurements/run-session1.sh widths
./measurements/run-session2.sh

./measurements/analyse-timing.py measurements/<session>/
```

`ttlbox-timing <block> --help` explains what each block is testing and why.
`--seconds` prints a block's duration, which is what sizes a BBTK capture
window — and that window is fixed before recording starts, with an interrupted
capture unrecoverable, so let the script compute it.

The 2026-08-05 session predates this harness; its pulse sequence was emitted by
`test_megttlbox` in [goxpyriment](https://github.com/chrplr/goxpyriment) and is
read with [`measurements/analyse-bbtk.py`](measurements/analyse-bbtk.py).
