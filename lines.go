// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Sonnet 4.6
// Distributed under the GNU General Public License v3.

package ttlbox

import "fmt"

// SetHighMask drives HIGH all output lines whose bits are set in mask.
// This is a persistent state (not a pulse); use [Box.SendTriggerMask] for pulses.
func (b *Box) SetHighMask(mask uint8) error {
	return b.tx([]byte{opSetHighMask, mask})
}

// SetLowMask drives LOW all output lines whose bits are set in mask.
func (b *Box) SetLowMask(mask uint8) error {
	return b.tx([]byte{opSetLowMask, mask})
}

// SetHighOnLine drives a single output line (0–7) HIGH persistently.
func (b *Box) SetHighOnLine(line uint8) error {
	if line > 7 {
		return fmt.Errorf("%w: got %d", ErrBadLine, line)
	}
	return b.tx([]byte{opSetHighOnLine, line})
}

// SetLowOnLine drives a single output line (0–7) LOW persistently.
func (b *Box) SetLowOnLine(line uint8) error {
	if line > 7 {
		return fmt.Errorf("%w: got %d", ErrBadLine, line)
	}
	return b.tx([]byte{opSetLowOnLine, line})
}

// AllLow drives all 8 output lines LOW. It is called automatically by
// [Box.Close] to leave the hardware in a safe state.
func (b *Box) AllLow() error {
	return b.SetLowMask(0xFF)
}
