# Arduino Firmware — NeuroSpin MEG TTL Box

This directory contains the Arduino firmware for the USB-to-TTL interface used in MEG experiments at NeuroSpin.

## Hardware

**Board:** Arduino Mega 2560

**Connections:**

| Role | Arduino pins | Count |
|---|---|---|
| TTL trigger outputs (to STI box) | D30–D37 | 8 |
| Response button inputs (from FORP box) | D22–D29 | 8 |

Input pins are configured with `INPUT_PULLUP`; the firmware inverts their logic so that a pressed button reads as 1 in the returned mask.

## FORP Button Mapping

Pins D22–D29 carry the FORP response-box signals to the STI box:

| Bit | Arduino pin | STI signal | Button |
|:---:|:-----------:|:----------:|--------|
| 0 | 22 | STI007 | Left blue |
| 1 | 23 | STI008 | Left yellow |
| 2 | 24 | STI009 | Left green |
| 3 | 25 | STI010 | Left red |
| 4 | 26 | STI012 | Right blue |
| 5 | 27 | STI013 | Right yellow |
| 6 | 28 | STI014 | Right green |
| 7 | 29 | STI015 | Right red |

## Trigger Output Mapping

Pins D30–D37 generate TTL pulses sent to the MEG acquisition PC:

| Bit | Arduino pin |
|:---:|:-----------:|
| 0 | 30 |
| 1 | 31 |
| 2 | 32 |
| 3 | 33 |
| 4 | 34 |
| 5 | 35 |
| 6 | 36 |
| 7 | 37 |

Up to 8 bits of information can be encoded simultaneously using a bitmask. The pulse width is configurable (default: 5 ms).

## Flashing the Firmware

1. Install the [Arduino IDE](https://www.arduino.cc/en/software).
2. Open `meg_protocol/meg_protocol.ino`.
3. Select **Tools → Board → Arduino Mega or Mega 2560**.
4. Select the correct port under **Tools → Port**.
5. Click **Upload**.

The firmware communicates at **115200 baud, 8N1** over the USB serial connection.

## Serial Protocol

Commands are sent as binary frames (host → device). The only device → host traffic is the 1-byte response to the button-read command.

| Opcode | Arguments | Description |
|:------:|-----------|-------------|
| 10 | u16 LE (ms) | Set trigger pulse width |
| 11 | u8 mask | Pulse all lines set in mask |
| 12 | u8 line (0–7) | Pulse a single line |
| 13 | u8 mask | Set lines HIGH (persistent) |
| 14 | u8 mask | Set lines LOW (persistent) |
| 15 | u8 line (0–7) | Set single line HIGH |
| 16 | u8 line (0–7) | Set single line LOW |
| 20 | — | Read button mask → returns u8 |

## Diagrams

- [`Schematic_Stim-MEG.png`](docs/Schematic_Stim-MEG.png) — system overview: Arduino in the MEG room
- [`schematic_forp_mapping.png`](docs/schematic_forp_mapping.png) — Arduino ↔ STI box wiring detail

## Role in the MEG System

The Arduino replaces the legacy parallel port as the interface between the stimulation PC and the MEG acquisition system. It provides:

- **Precise TTL trigger generation** at stimulus onset, for epoching MEG data
- **Real-time button readout** from the FORP response box
- **Bidirectional visibility** into what happens inside the MEG room

The firmware is designed to be extensible: pins beyond D22–D37 remain available for additional peripherals (photodiodes, extra sensors, etc.) without modifying the existing wiring.
