// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Sonnet 4.6
// Distributed under the GNU General Public License v3.

package ttlbox

import "testing"

func TestDecodeMaskEmpty(t *testing.T) {
	if got := DecodeMask(0); len(got) != 0 {
		t.Errorf("DecodeMask(0) = %v, want empty slice", got)
	}
}

func TestDecodeMaskAllBits(t *testing.T) {
	got := DecodeMask(0xFF)
	if len(got) != 8 {
		t.Fatalf("DecodeMask(0xFF) returned %d buttons, want 8", len(got))
	}
	for i, btn := range got {
		if uint8(btn) != uint8(i) {
			t.Errorf("got[%d] = %v, want %d", i, btn, i)
		}
	}
}

func TestDecodeMaskSingleBits(t *testing.T) {
	tests := []struct {
		mask uint8
		want FORPButton
	}{
		{0b00000001, FORPLeftBlue},
		{0b00000010, FORPLeftYellow},
		{0b00000100, FORPLeftGreen},
		{0b00001000, FORPLeftRed},
		{0b00010000, FORPRightBlue},
		{0b00100000, FORPRightYellow},
		{0b01000000, FORPRightGreen},
		{0b10000000, FORPRightRed},
	}
	for _, tt := range tests {
		got := DecodeMask(tt.mask)
		if len(got) != 1 || got[0] != tt.want {
			t.Errorf("DecodeMask(%08b) = %v, want [%v]", tt.mask, got, tt.want)
		}
	}
}

func TestDecodeMaskMultiBit(t *testing.T) {
	got := DecodeMask(0b00000101) // bits 0 and 2
	if len(got) != 2 || got[0] != FORPLeftBlue || got[1] != FORPLeftGreen {
		t.Errorf("DecodeMask(0b00000101) = %v, want [FORPLeftBlue FORPLeftGreen]", got)
	}
}

func TestFORPButtonString(t *testing.T) {
	if got := FORPLeftRed.String(); got != "left red" {
		t.Errorf("FORPLeftRed.String() = %q, want %q", got, "left red")
	}
}

func TestFORPButtonStringUnknown(t *testing.T) {
	unknown := FORPButton(9)
	if got := unknown.String(); got == "" {
		t.Error("String() on unknown button should not be empty")
	}
}
