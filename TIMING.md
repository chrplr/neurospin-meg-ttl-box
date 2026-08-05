# Device capabilities and measured timing

What this box can and cannot do, with numbers rather than estimates. Every
figure below was measured on hardware; anything not measured is marked as such.

Measurements: Arduino Mega 2560 R3, firmware protocol v1 (`caps 0x03`), 2026-08-05.
Raw data and reproduction instructions in [`measurements/`](measurements/).

---

## Summary

| Property | Value | How established |
|---|---|---|
| TTL logic level | 5 V | design |
| Output lines | 8 (D30–D37) | — |
| Input lines | 8 (D22–D29), `INPUT_PULLUP` | — |
| Trigger-code write | **atomic**, skew < 250 µs | BBTKv3, 20 trials |
| Pulse width bias | **−0.5 to −0.7 ms** (systematically short) | BBTKv3, 60 pulses |
| Pulse width jitter | < 1 ms beyond quantisation | BBTKv3 |
| Host→device latency | ~1.5 ms median, ~0.8–2.4 ms range | two independent methods |
| Input event timestamp | few µs of the edge | firmware `micros()` |
| Reaction-time accuracy | **sub-millisecond**, not microsecond | see caveat below |
| Button poll (legacy path) | ≥ 5 ms, host-limited | by construction |

---

## Output

### Trigger codes are atomic

On the Mega 2560, output pins D30–D37 land on a single AVR port (`PORTC7`–`PORTC0`,
note the **reversed** bit order). Opcode 17 assigns the whole port in one
instruction, so no intermediate value ever reaches the pins and a recording
device cannot latch a half-written code.

**Measured:** one command pulsed D30 and D31 while a BBTKv3 watched both. All 20
trials placed the two rising edges in the **same 0.25 ms sample** — skew below
250 µs, measured by an instrument independent of the firmware.

This matters because the older two-command path (`set_high_mask` then
`set_low_mask`, opcodes 13/14) leaves the port at `previous | mask` between the
two, for up to a USB frame. That intermediate is a valid-looking but *wrong*
trigger code, and an amplifier sampling at 1 kHz can record it. Clients should
feature-detect `CAP_ATOMIC_PORT` and prefer opcode 17.

### Pulse width runs short — by design, and predictably

The firmware times pulses itself (`g_pulse_end = millis() + width`, dropped from
the main loop), so width does not absorb host scheduling jitter. But `millis()`
has 1 ms resolution and truncates, so the realised width is uniform on
**[w−1, w]**.

| requested | min | median | max | mean error |
|---|---|---|---|---|
| 5 ms | 3.75 | 4.50 | 5.00 | −0.53 ms |
| 10 ms | 8.25 | 9.25 | 10.25 | −0.68 ms |
| 20 ms | 18.50 | 19.50 | 20.25 | −0.69 ms |

*20 pulses per width, BBTKv3 at 0.25 ms sampling.*

**This is a bias, not noise — it cannot be averaged away.** If a paradigm needs a
true 5 ms pulse, request 6 ms. Total spread of 1.25–2.0 ms is barely above the
1.25 ms floor set by truncation plus the instrument's own sampling, so genuine
firmware jitter is well under a millisecond.

A `micros()`-based pulse timer would remove the bias; nobody has needed it yet.

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

### Reaction-time accuracy: sub-millisecond, not microsecond

The edge is timestamped to within a few microseconds. But converting a device
`micros()` value into host time costs the accuracy of the clock-offset estimate,
which is bounded by the asymmetry of the sync round trip — **a few hundred
microseconds**. Quoting the 4 µs `micros()` tick as the RT accuracy would be
wrong.

So: far better than the ≥5 ms floor of polling, but do not claim microseconds.

### Queue overflow is reported, not hidden

The firmware holds 32 events. If the host does not drain them it sets a sticky
flag, surfaced to clients. An overflow means presses were **lost, not delayed**,
so an affected trial should be treated as suspect. In practice it takes a
mechanical button chattering — hence the debounce opcode, which is **off by
default** because fibre-optic pads do not bounce and suppressing real
transitions is worse than reporting extra ones.

---

## Host→device latency

The time from the host issuing a command to the line actually moving. Measured
twice, by unrelated methods:

| method | min | median | max |
|---|---|---|---|
| Firmware timestamp of a loopback edge vs. host clock (50 trials) | 802 µs | **1.44 ms** | 2.05 ms |
| BBTK onset-to-onset interval minus requested ISI (80 pulses) | — | **~1.5 ms** | — |

Two independent measurements agreeing within 0.1 ms is reasonable evidence both
are sound. The spread is one USB full-speed frame (1 ms) plus the ~174 µs two
command bytes take on the 16u2↔2560 UART at 115200 baud.

**This latency is irreducible from the host side** and is the part of a reaction
time that firmware timestamping cannot remove. It is why a trigger's *onset*
should be issued as close to stimulus onset as possible, and why a parallel port
(a single sub-microsecond `outb`, no USB) remains the reference for trigger
timing.

---

## Not measured

Stated for honesty; do not quote these as verified.

- **Pulse width above 20 ms or below 5 ms.** The truncation model predicts the
  same −0.5 ms mean bias at any width, but only 5/10/20 ms were tested.
- **Behaviour under sustained load** — long sessions, high trigger rates, or a
  host that stops draining the event queue.
- **Temperature and long-run clock drift.** The AVR's ceramic resonator will
  drift; the 20-minute client re-sync bounds the resulting error but nobody has
  measured how much drift there is.
- **The 16 remaining digital lines** and any use of the box beyond 8-in/8-out.

---

## Reproducing

The pulse sequence is emitted by `test_megttlbox` in
[goxpyriment](https://github.com/chrplr/goxpyriment) (`tests/test_megttlbox/`),
which drives this box through its Go client:

```bash
# wiring: line 0 -> D30 -> TTLin2, line 1 -> D31 -> TTLin1, common GND
./run-bbtk.sh                                    # capture
./analyse-bbtk.py <capture>-001-events.csv       # numbers
```

BBTK smoothing does not apply here: it affects only `Opto*` and `Mic*` channels,
never TTL inputs.

Loopback checks needing no external instrument (jumper D30→D22 … D37→D29):

```bash
go run ./tests/test_megttlbox -loopback     # all 16 lines end to end
go run ./tests/test_megttlbox -atomic 30    # atomicity, firmware as witness
go run ./tests/test_megttlbox -rtloop 50    # host->device latency
```
