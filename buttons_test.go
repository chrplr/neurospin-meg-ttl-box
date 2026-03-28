// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Sonnet 4.6
// Distributed under the GNU General Public License v3.

package ttlbox

import (
	"context"
	"testing"
	"time"
)

func TestPollButtons(t *testing.T) {
	b, _ := newMockBox([]byte{0b00000101})
	mask, err := b.PollButtons()
	if err != nil {
		t.Fatal(err)
	}
	if mask != 0b00000101 {
		t.Errorf("got %08b, want 00000101", mask)
	}
}

func TestReadButtonMask(t *testing.T) {
	b, _ := newMockBox([]byte{0b10000001})
	mask, err := b.ReadButtonMask(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if mask != 0b10000001 {
		t.Errorf("got %08b, want 10000001", mask)
	}
}

func TestReadButtonMaskCancelledContext(t *testing.T) {
	b, _ := newMockBox(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := b.ReadButtonMask(ctx)
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestWaitForButton(t *testing.T) {
	// First poll returns 0 (not pressed), second returns a press.
	b, _ := newMockBox([]byte{0x00, 0b00000001})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	mask, rt, err := b.WaitForButton(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if mask != 0b00000001 {
		t.Errorf("got mask %08b, want 00000001", mask)
	}
	if rt < 0 {
		t.Error("rt should be non-negative")
	}
}

func TestWaitForButtonContextCancel(t *testing.T) {
	// rx buffer always returns 0 (no press); context is pre-cancelled.
	b, _ := newMockBox([]byte{0, 0, 0, 0, 0, 0, 0, 0})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := b.WaitForButton(ctx)
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestDrainButtons(t *testing.T) {
	// Returns pressed (non-zero) twice, then released.
	b, _ := newMockBox([]byte{0b00000001, 0b00000001, 0x00})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := b.DrainButtons(ctx); err != nil {
		t.Fatalf("DrainButtons: %v", err)
	}
}

func TestWaitForButtonMask(t *testing.T) {
	// Returns partial mask first, then full expected mask.
	b, _ := newMockBox([]byte{0b00000001, 0b00000011})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	rt, err := b.WaitForButtonMask(ctx, 0b00000011)
	if err != nil {
		t.Fatal(err)
	}
	if rt < 0 {
		t.Error("rt should be non-negative")
	}
}
