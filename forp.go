// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Sonnet 4.6
// Distributed under the GNU General Public License v3.

package ttlbox

import "fmt"

// FORPButton represents a named button on the FORP (Fibre Optic Response Pad)
// response box. Its numeric value corresponds to the bit position in the mask
// returned by [Box.PollButtons].
type FORPButton uint8

const (
	FORPLeftBlue    FORPButton = 0 // bit 0 — Arduino pin 22 — STI007
	FORPLeftYellow  FORPButton = 1 // bit 1 — Arduino pin 23 — STI008
	FORPLeftGreen   FORPButton = 2 // bit 2 — Arduino pin 24 — STI009
	FORPLeftRed     FORPButton = 3 // bit 3 — Arduino pin 25 — STI010
	FORPRightBlue   FORPButton = 4 // bit 4 — Arduino pin 26 — STI012
	FORPRightYellow FORPButton = 5 // bit 5 — Arduino pin 27 — STI013
	FORPRightGreen  FORPButton = 6 // bit 6 — Arduino pin 28 — STI014
	FORPRightRed    FORPButton = 7 // bit 7 — Arduino pin 29 — STI015
)

// FORPButtonNames maps each [FORPButton] to its human-readable label.
// It is a package-level variable so callers can extend or localise it.
var FORPButtonNames = map[FORPButton]string{
	FORPLeftBlue:    "left blue",
	FORPLeftYellow:  "left yellow",
	FORPLeftGreen:   "left green",
	FORPLeftRed:     "left red",
	FORPRightBlue:   "right blue",
	FORPRightYellow: "right yellow",
	FORPRightGreen:  "right green",
	FORPRightRed:    "right red",
}

// String implements [fmt.Stringer].
func (b FORPButton) String() string {
	if name, ok := FORPButtonNames[b]; ok {
		return name
	}
	return fmt.Sprintf("button %d", uint8(b))
}

// DecodeMask returns the [FORPButton] values whose bits are set in mask,
// ordered from LSB (FORPLeftBlue) to MSB (FORPRightRed).
func DecodeMask(mask uint8) []FORPButton {
	var buttons []FORPButton
	for i := uint8(0); i < 8; i++ {
		if (mask>>i)&1 == 1 {
			buttons = append(buttons, FORPButton(i))
		}
	}
	return buttons
}
