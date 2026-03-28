// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Sonnet 4.6
// Distributed under the GNU General Public License v3.

package ttlbox

import (
	"context"
	"time"
)

// PollButtons sends one request to the device and returns the raw button mask.
// Bit N is 1 when FORP button N is currently pressed. This is a single
// non-blocking read from the Go side; use [Box.WaitForButton] for blocking waits.
func (b *Box) PollButtons() (uint8, error) {
	if err := b.tx([]byte{opGetResponseButton}); err != nil {
		return 0, err
	}
	buf, err := b.rxExact(1)
	if err != nil {
		return 0, err
	}
	return buf[0], nil
}

// ReadButtonMask queries the device once and returns the current button mask.
// ctx is checked for cancellation before the serial round-trip.
func (b *Box) ReadButtonMask(ctx context.Context) (uint8, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	return b.PollButtons()
}

// DrainButtons polls the device until no buttons are pressed (mask == 0) or
// ctx is cancelled. Call this before [Box.WaitForButton] to ensure that
// buttons held from a previous trial do not produce an immediate false trigger.
func (b *Box) DrainButtons(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		mask, err := b.PollButtons()
		if err != nil {
			return err
		}
		if mask == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(b.pollInterval):
		}
	}
}

// WaitForButton blocks until at least one button is pressed or ctx is
// cancelled. It returns the button mask and the elapsed time since the call
// (reaction time). Call [Box.DrainButtons] first to clear any latched presses.
func (b *Box) WaitForButton(ctx context.Context) (mask uint8, rt time.Duration, err error) {
	start := time.Now()
	for {
		if ctx.Err() != nil {
			return 0, 0, ctx.Err()
		}
		mask, err = b.PollButtons()
		if err != nil {
			return 0, 0, err
		}
		if mask != 0 {
			return mask, time.Since(start), nil
		}
		select {
		case <-ctx.Done():
			return 0, 0, ctx.Err()
		case <-time.After(b.pollInterval):
		}
	}
}

// WaitForButtonMask blocks until all bits in expectedMask are simultaneously
// set in the live button mask, or ctx is cancelled. Returns the elapsed time.
func (b *Box) WaitForButtonMask(ctx context.Context, expectedMask uint8) (rt time.Duration, err error) {
	start := time.Now()
	for {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		mask, err := b.PollButtons()
		if err != nil {
			return 0, err
		}
		if (mask & expectedMask) == expectedMask {
			return time.Since(start), nil
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(b.pollInterval):
		}
	}
}
