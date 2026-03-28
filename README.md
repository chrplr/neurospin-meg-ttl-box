# neurospin-meg-ttl-box

Go library and CLI for the Arduino-based TTL trigger and response-button interface used in MEG (magnetoencephalography) experiments at NeuroSpin.

The system replaces a legacy parallel port with an Arduino Mega 2560 connected over USB. It exposes:

- **8 TTL output lines** (pins D30–D37) for generating stimulus-onset triggers
- **8 TTL input lines** (pins D22–D29) for reading FORP response-box buttons

See [`arduino/README.md`](arduino/README.md) for hardware setup, pin mapping, and flashing instructions.

The current repository contains is a Go port of [meg_USBio](https://github.com/mirian22ainar/meg_USBio), which provides the original Python client and Arduino firmware.  

The ttl-box and its Python API were designed and implemented by [Mirian Aïnar](https://www.linkedin.com/in/mirian-ainar/) under the supervision of [Christophe Pallier](http://www.pallier.org) and with technical support from Marie-France Fourcade and Jérémy Bernard (CEA Neurospin). 

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

Copyright 2006 Christophe Pallier

Co-author: Claude Sonnet and Mirian Aïnar (original Python code)

Distributed under the [GNU General Public License v3](LICENSE.txt).

 
[ChrPlr](https://github.com/chrplr)
