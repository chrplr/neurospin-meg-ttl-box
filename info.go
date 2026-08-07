// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Opus 5.
// Licensed under the Apache License, Version 2.0 (see LICENSE.txt).

package ttlbox

import (
	"errors"
	"fmt"
)

// Capability bits reported in the caps byte of [Info].
// They match CAP_ATOMIC_PORT and CAP_TIMESTAMPS in the firmware.
const (
	// CapAtomicPort means the device implements opcode 17 (set_port_mask),
	// which assigns all 8 output lines in a single AVR port write. Without it
	// a trigger code has to be composed from set_high_mask + set_low_mask, and
	// the port shows a half-written value in between.
	CapAtomicPort uint8 = 0x01

	// CapTimestamps means the device implements opcodes 21–24, which timestamp
	// input transitions with the firmware's own micros() clock instead of
	// leaving the resolution to the host's polling interval.
	CapTimestamps uint8 = 0x02
)

// Info is the firmware identification returned by [Box.GetInfo].
type Info struct {
	// Version is the protocol version. Only version 1 exists so far.
	Version uint8
	// Caps is the raw capability bitmask; test it with [Info.Has].
	Caps uint8
}

// Has reports whether every bit in cap is set in the capability mask.
func (i Info) Has(cap uint8) bool { return i.Caps&cap == cap }

// AtomicPort reports whether the device can assign the whole output port in one
// instruction (see [CapAtomicPort]).
func (i Info) AtomicPort() bool { return i.Has(CapAtomicPort) }

// Timestamps reports whether the device timestamps input events
// (see [CapTimestamps]).
func (i Info) Timestamps() bool { return i.Has(CapTimestamps) }

// String renders the info the way the measurement tools log it.
func (i Info) String() string {
	return fmt.Sprintf("firmware v%d, caps 0x%02X (atomic port: %t, input timestamps: %t)",
		i.Version, i.Caps, i.AtomicPort(), i.Timestamps())
}

// GetInfo asks the firmware to identify itself (opcode 1).
//
// Firmware predating protocol version 1 does not implement the opcode and
// ignores it silently, so there is no reply and no error to catch — the read
// simply times out. That is the only way to detect legacy firmware, and it is
// reported as [ErrLegacyFirmware] so callers can tell "old device" from "no
// device". Feature-detect with [Info.AtomicPort] and [Info.Timestamps] rather
// than assuming a version implies a capability.
func (b *Box) GetInfo() (Info, error) {
	if err := b.tx([]byte{opGetInfo}); err != nil {
		return Info{}, err
	}
	buf, err := b.rxExact(5)
	if err != nil {
		if errors.Is(err, ErrTimeout) {
			return Info{}, fmt.Errorf("%w: no reply to get_info", ErrLegacyFirmware)
		}
		return Info{}, err
	}
	if buf[0] != 'M' || buf[1] != 'T' || buf[2] != 'B' {
		return Info{}, fmt.Errorf("%w: get_info replied %q, want \"MTB\"",
			ErrBadReply, string(buf[:3]))
	}
	return Info{Version: buf[3], Caps: buf[4]}, nil
}
