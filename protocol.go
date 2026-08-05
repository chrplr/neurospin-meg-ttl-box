// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Sonnet 4.6
// Licensed under the Apache License, Version 2.0 (see LICENSE.txt).

package ttlbox

import "encoding/binary"

// opcode values match those in arduino/meg_protocol/meg_protocol.ino exactly.
// Do not renumber without updating the firmware.
const (
	// opGetInfo asks the firmware to identify itself; it answers
	// 'M','T','B', version uint8, caps uint8. Firmware older than protocol
	// version 1 does not implement it and stays silent, so a host detects
	// legacy firmware by the read timing out rather than by any reply.
	opGetInfo            uint8 = 1
	opSetTriggerDuration uint8 = 10
	opSendTriggerMask    uint8 = 11
	opSendTriggerOnLine  uint8 = 12
	opSetHighMask        uint8 = 13
	opSetLowMask         uint8 = 14
	opSetHighOnLine      uint8 = 15
	opSetLowOnLine       uint8 = 16
	// opSetPortMask assigns all 8 output lines in one atomic port write, so a
	// trigger code is never visible half-written. Only firmware advertising
	// CAP_ATOMIC_PORT (0x01) via opGetInfo implements it; older firmware
	// ignores it silently, leaving the lines unchanged. Feature-detect before
	// using it — there is no error to catch.
	opSetPortMask       uint8 = 17
	opGetResponseButton uint8 = 20
	// opGetEvent..opSetDebounce are the timestamped-input-event API, present
	// only on firmware advertising CAP_TIMESTAMPS (0x02) via opGetInfo. The
	// firmware samples the buttons every loop iteration and records micros()
	// at the transition, so a reaction time is not floored by the host's poll
	// interval. opGetEvent always replies with 6 bytes
	// ([flags u8][mask u8][micros u32 LE]) even when the queue is empty, so
	// the host never has to guess the reply length.
	opGetEvent    uint8 = 21
	opGetMicros   uint8 = 22
	opClearEvents uint8 = 23
	opSetDebounce uint8 = 24
)

// encodeU16LE encodes v as a 2-byte little-endian array, matching the wire
// format expected by the firmware for opSetTriggerDuration.
func encodeU16LE(v uint16) [2]byte {
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], v)
	return b
}
