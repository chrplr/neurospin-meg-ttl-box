// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Sonnet 4.6
// Distributed under the GNU General Public License v3.

package ttlbox

import (
	"fmt"
	"time"
)

// SetTriggerDuration sets the TTL pulse width stored on the device.
// d is rounded to the nearest millisecond and must be in [0, 65535 ms].
// The value persists on the device until changed or the device resets.
//
// Example: box.SetTriggerDuration(5 * time.Millisecond)
func (b *Box) SetTriggerDuration(d time.Duration) error {
	ms := int64(d.Round(time.Millisecond) / time.Millisecond)
	if ms < 0 || ms > 65535 {
		return fmt.Errorf("%w: got %d ms", ErrBadDuration, ms)
	}
	u16 := encodeU16LE(uint16(ms))
	return b.tx([]byte{opSetTriggerDuration, u16[0], u16[1]})
}

// SendTriggerMask fires a TTL pulse on every output line whose corresponding
// bit is set in mask. Bit 0 = line 0 (pin 30) … bit 7 = line 7 (pin 37).
// The pulse width is the value last set by [Box.SetTriggerDuration].
func (b *Box) SendTriggerMask(mask uint8) error {
	return b.tx([]byte{opSendTriggerMask, mask})
}

// SendTriggerOnLine fires a TTL pulse on a single output line (0–7).
// The pulse width is the value last set by [Box.SetTriggerDuration].
func (b *Box) SendTriggerOnLine(line uint8) error {
	if line > 7 {
		return fmt.Errorf("%w: got %d", ErrBadLine, line)
	}
	return b.tx([]byte{opSendTriggerOnLine, line})
}
