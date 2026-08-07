// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Opus 5.
// Licensed under the Apache License, Version 2.0 (see LICENSE.txt).

package ttlbox

import (
	"bytes"
	"testing"
)

func TestSetPortMask(t *testing.T) {
	b, tx := newMockBox(nil)

	if err := b.SetPortMask(0xAA); err != nil {
		t.Fatalf("SetPortMask: %v", err)
	}
	want := []byte{opSetPortMask, 0xAA}
	if got := tx.Bytes(); !bytes.Equal(got, want) {
		t.Errorf("sent % X, want % X", got, want)
	}
}

// Changing a trigger code from 0b01 to 0b10 is one command with SetPortMask and
// two with the legacy pair — and the two-command form leaves the port showing
// 0b11 in between. This test pins the byte sequences the two paths produce,
// because the measurement that demonstrates the difference on hardware
// (measurements/, block B2) depends on exactly this wire behaviour.
func TestCodeChangeAtomicVersusLegacy(t *testing.T) {
	atomic, atomicTx := newMockBox(nil)
	if err := atomic.SetPortMask(0b10); err != nil {
		t.Fatalf("SetPortMask: %v", err)
	}
	if got, want := atomicTx.Bytes(), []byte{opSetPortMask, 0b10}; !bytes.Equal(got, want) {
		t.Errorf("atomic path sent % X, want % X", got, want)
	}

	legacy, legacyTx := newMockBox(nil)
	if err := legacy.SetHighMask(0b10); err != nil {
		t.Fatalf("SetHighMask: %v", err)
	}
	if err := legacy.SetLowMask(0b01); err != nil {
		t.Fatalf("SetLowMask: %v", err)
	}
	want := []byte{opSetHighMask, 0b10, opSetLowMask, 0b01}
	if got := legacyTx.Bytes(); !bytes.Equal(got, want) {
		t.Errorf("legacy path sent % X, want % X", got, want)
	}
}

func TestAllLow(t *testing.T) {
	b, tx := newMockBox(nil)

	if err := b.AllLow(); err != nil {
		t.Fatalf("AllLow: %v", err)
	}
	want := []byte{opSetLowMask, 0xFF}
	if got := tx.Bytes(); !bytes.Equal(got, want) {
		t.Errorf("sent % X, want % X", got, want)
	}
}

func TestSetLineOutOfRange(t *testing.T) {
	b, _ := newMockBox(nil)

	if err := b.SetHighOnLine(8); err == nil {
		t.Error("SetHighOnLine(8) succeeded, want ErrBadLine")
	}
	if err := b.SetLowOnLine(8); err == nil {
		t.Error("SetLowOnLine(8) succeeded, want ErrBadLine")
	}
}
