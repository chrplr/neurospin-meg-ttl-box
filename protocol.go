// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Sonnet 4.6
// Distributed under the GNU General Public License v3.

package ttlbox

import "encoding/binary"

// opcode values match those in arduino/meg_protocol/meg_protocol.ino exactly.
// Do not renumber without updating the firmware.
const (
	opSetTriggerDuration uint8 = 10
	opSendTriggerMask    uint8 = 11
	opSendTriggerOnLine  uint8 = 12
	opSetHighMask        uint8 = 13
	opSetLowMask         uint8 = 14
	opSetHighOnLine      uint8 = 15
	opSetLowOnLine       uint8 = 16
	opGetResponseButton  uint8 = 20
)

// encodeU16LE encodes v as a 2-byte little-endian array, matching the wire
// format expected by the firmware for opSetTriggerDuration.
func encodeU16LE(v uint16) [2]byte {
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], v)
	return b
}
