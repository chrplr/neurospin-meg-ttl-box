# neurospin-meg-ttl-box

Go library and CLI for the Arduino-based TTL trigger and response-button interface used in MEG (magnetoencephalography) experiments at NeuroSpin.

The system replaces a legacy parallel port with an Arduino Mega 2560 connected over USB. It exposes:

- **8 TTL output lines** (pins D30–D37) for generating stimulus-onset triggers
- **8 TTL input lines** (pins D22–D29) for reading FORP response-box buttons

See [`arduino/README.md`](arduino/README.md) for hardware setup, pin mapping, and flashing instructions.

See [`TIMING.md`](TIMING.md) for what the device can and cannot do, measured on
hardware: a simultaneous two-line write is atomic to within 250 µs, pulse widths
run systematically 0.5–0.7 ms **short** and are unaffected by host load, and
reaction-time accuracy is sub-millisecond rather than microsecond. It also marks
what is *not* yet measured — including the absolute host→device latency, for the
reason below — and names the block that measures it. Raw data and the measurement
harness are in [`measurements/`](measurements/).

### Why no absolute latency is quoted

An earlier version of this README said "host→device latency is ~1.5 ms". That
figure was withdrawn: it rested on two methods, and neither measures it.

The onset-to-onset method cannot, as a matter of arithmetic. For pulses commanded
at `c[i]` and observed at `c[i] + L[i]`, the measured interval is
`(c[i+1] − c[i]) + (L[i+1] − L[i])` — **the latency cancels**, so with a constant
latency the measured interval equals the commanded one however large that latency
is. What it actually showed was the host loop's per-iteration overhead, dominated
by a USB round trip, hence a number that looks like a latency.

The loopback method does measure latency, but converting the firmware's `micros()`
timestamp into host time costs the clock-offset estimate, which is bounded by the
asymmetry of the sync round trip — and that round trip has a ~2.4 ms floor here,
larger than the quantity being measured.

**Round trips cannot rescue this.** Every such measurement is a sum of an
outbound and a return latency, and no combination of devices separates them: it
is the one-way delay problem from clock synchronisation, where round-trip time is
measurable to arbitrary precision and one-way delay is not derivable from it. NTP
assumes symmetry for the same reason.

Measuring it needs an event the host can produce at a time it knows exactly,
visible to the same instrument as the TTL output — a parallel-port `outb`, or a
memory-mapped GPIO write. Everything else in `TIMING.md` stays inside a single
clock and is unaffected.

~1.5 ms remains a reasonable estimate from first principles — one USB frame plus
the ~174 µs two command bytes take on the 16u2 UART — and a scope comparison
against a DLP-IO8 puts the two devices within 38 µs of each other. But it is an
estimate, and this document no longer presents it as a result.

The current repository is a Go port of [meg_USBio](https://github.com/mirian22ainar/meg_USBio), which provides the original Python client and Arduino firmware.  

The ttl-box and its Python API were designed and implemented by **[Mirian Aïnar](https://www.linkedin.com/in/mirian-ainar/)** under the supervision of **[Christophe Pallier](http://www.pallier.org)** with support from **Marie-France Fourcade** and **Jérémy Bernard** (CEA Neurospin TEAM-stim). 

> [!WARNING]
> While we have battle-tested the Python version, this one needs testing. Please submit bug reports and suggestions to https://github.com/chrplr/neurospin-meg-ttl-box/issues


## Installation

### Library (for Go projects)

```bash
go get github.com/neurospin/neurospin-meg-ttl-box
```

### CLI (`ttlbox`)

If you have Go installed:

```bash
go install github.com/neurospin/neurospin-meg-ttl-box/cmd/ttlbox@latest
```

Otherwise, download a pre-built binary for your platform from the [GitHub Releases page](../../releases/latest):

| OS | Architecture | File |
|---|---|---|
| Linux | x86-64 | `ttlbox-linux-amd64` |
| Linux | ARM64 | `ttlbox-linux-arm64` |
| macOS | x86-64 (Intel) | `ttlbox-macos-amd64` |
| macOS | ARM64 (Apple Silicon) | `ttlbox-macos-arm64` |
| Windows | x86-64 | `ttlbox-windows-amd64.exe` |
| Windows | ARM64 | `ttlbox-windows-arm64.exe` |

Make it executable (Linux/macOS: `chmod +x ttlbox-*`) and place it somewhere on your `PATH`.

> [!WARNING]
> If  Windows Defender or macOS Getkeeper pretend that the binary is damaged or a dangerous, go ahead anyway.
> Under macOS, you may have to use `xattr -d com.apple.quarantine ./ttbox-*` then `chmod +x ttlbox*`. 

## Finding the serial port

Once the CLI is installed, the easiest way is:

```bash
ttlbox ports
```

This lists all detected serial ports. Plug the Arduino in, run it again, and the new entry is your device.

If the CLI is not yet available, use the OS-native method below.

**Linux**

Watch kernel messages while plugging the Arduino in:

```bash
sudo dmesg -w
```

Look for a line like `cdc_acm ... ttyACM0: USB ACM device`. The port will be `/dev/ttyACM0` (or `ttyACM1`, etc.). You can also list candidate devices directly:

```bash
ls /dev/ttyACM* /dev/ttyUSB*
```

> If you get a "permission denied" error when opening the port, add yourself to the `dialout` group: `sudo usermod -aG dialout $USER` (then log out and back in).

**macOS**

```bash
ls /dev/cu.*
```

An Arduino Mega typically appears as `/dev/cu.usbmodem<number>` (native USB) or `/dev/cu.usbserial-<number>` (FTDI chip). Plug and unplug to identify the right entry.

**Windows**

Open **Device Manager** (Win + X → Device Manager) and expand **Ports (COM & LPT)**. The Arduino will appear as `USB Serial Device (COMx)` or `Arduino Mega 2560 (COMx)`. Use `COMx` as the port value, e.g. `--port COM3`.

## Library usage

```go
import (
    "context"
    "fmt"
    "time"

    ttlbox "github.com/neurospin/neurospin-meg-ttl-box"
)

box, err := ttlbox.Open("/dev/ttyACM0")
if err != nil {
    log.Fatal(err)
}
defer box.Close()

// Set pulse width and send a trigger on line 0 at stimulus onset
box.SetTriggerDuration(5 * time.Millisecond)
box.SendTriggerOnLine(0)

// Wait for a button press (up to 2 s), measuring reaction time
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

box.DrainButtons(ctx) // clear any held buttons before the trial
mask, rt, err := box.WaitForButton(ctx)
fmt.Println(ttlbox.DecodeMask(mask), rt)
```

### Protocol v1: atomic codes and timestamped inputs

Firmware advertising protocol v1 adds two things worth having. Feature-detect
them — older firmware ignores the opcodes silently, so there is no error to
catch.

```go
info, err := box.GetInfo()
if err != nil {
    // ErrLegacyFirmware when the device stays silent: firmware older than
    // protocol v1 ignores the opcode, so silence is the only signature.
    log.Fatal(err)
}

if info.AtomicPort() {
    // Change the whole trigger code in one AVR port write, so no
    // intermediate value ever reaches the wires.
    box.SetPortMask(0b00000110)
}

if info.Timestamps() {
    clock := box.NewClock()
    clock.Sample(20)                // host<->device offset, best of 20 round trips

    box.ClearEvents()               // re-seed at the start of a trial
    r, _ := box.GetEvent()
    if r.Present {
        // When the edge happened, not when you got round to asking.
        onset := clock.EventHost(r.Event)
        fmt.Println(ttlbox.DecodeMask(r.Event.Mask), onset)
    }
    if r.Overflow {
        // Transitions were lost, not delayed: the trial is wrong, not late.
    }
}
```

`Clock` fits offset *and* rate over repeated samples, and `Clock.Fit()` reports
the residuals — which is the honest accuracy of a device→host conversion, rather
than the device's 4 µs tick.

**Build a command and write it once.** The firmware blocks in an unbounded spin
waiting for an argument byte, and samples no inputs while it waits. The typed
methods above all write in one call; `SendRaw` is the only way to break this and
is documented as a diagnostic.

## CLI usage

```
ttlbox [--port /dev/ttyACM0] [--reset-delay 2000] <command>

Commands:
  ports                        List available serial ports
  trigger duration <ms>        Set TTL pulse width
  trigger mask <0-255>         Pulse all lines set in mask
  trigger line <0-7>           Pulse a single line
  line high mask <0-255>       Drive lines HIGH persistently
  line high line <0-7>         Drive one line HIGH
  line low  mask <0-255>       Drive lines LOW persistently
  line low  line <0-7>         Drive one line LOW
  buttons read                 Read current button state
  buttons wait [--timeout ms]  Block until a button is pressed; print RT
```

### `ttlbox-timing`

A second binary runs the measurement blocks behind [`TIMING.md`](TIMING.md).
Several need no instrument beyond a `D30 → D22` jumper, and are worth running on
a stimulus PC before it is used for an experiment — host→device latency in
particular depends on the host, not the box.

```bash
go install github.com/neurospin/neurospin-meg-ttl-box/cmd/ttlbox-timing@latest

ttlbox-timing latency --pulses 10000 --isi 20   # write latency, with the tail
ttlbox-timing splitwrite                        # the split-write stall
ttlbox-timing widths -n                         # any block: -n prints the plan
```

Every block writes a CSV of raw per-trial rows and summarises nothing; read them
with [`measurements/analyse-timing.py`](measurements/analyse-timing.py). See
[`measurements/README.md`](measurements/README.md) for the wiring and the
sessions that need a Black Box ToolKit.

## API improvements over the Python version

| Python | Go | Reason |
|---|---|---|
| `set_trigger_duration(ms: int)` | `SetTriggerDuration(time.Duration)` | Units explicit at call site |
| `get_response_button_mask()` | `ReadButtonMask(ctx)` | Cancellable, returns error |
| Polling loop in user code | `WaitForButton(ctx) (mask, rt, error)` | Returns reaction time directly; 5 ms poll interval avoids saturating the serial bus |
| `decode_forp(mask) []string` | `DecodeMask(mask) []FORPButton` | Strongly typed; call `.String()` for text |
| No cleanup on exit | `Close()` calls `AllLow()` first | Lines are safe even on crash |
| No `DrainButtons` | `DrainButtons(ctx)` | Clears latched presses before a new trial |

## Running tests

```bash
go test ./...
```

All unit tests run without hardware using an in-memory mock serial port.

Hardware-dependent tests (build tag `integration`) require a connected Arduino and `TTLBOX_PORT` set:

```bash
TTLBOX_PORT=/dev/ttyACM0 go test -tags integration ./...
```

## License

Copyright 2025-2026 Christophe Pallier
Copyright 2025 Mirian Aïnar

Distributed under the [Apache License, Version 2.0](LICENSE.txt).

The Arduino firmware and the original Python implementation from which the Go
client was ported were written by Mirian Aïnar. Co-authored with Claude.

If you redistribute this software, the Apache License requires you to pass on
the attribution notices in [NOTICE](NOTICE) — see section 4(d).

 
[ChrPlr](https://github.com/chrplr)
