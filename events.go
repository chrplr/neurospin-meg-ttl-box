// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Opus 5.
// Licensed under the Apache License, Version 2.0 (see LICENSE.txt).

package ttlbox

import (
	"encoding/binary"
	"fmt"
	"time"
)

// Event is one input-port transition, timestamped by the firmware.
//
// The firmware samples all 8 input lines in a single PINA read on every loop
// iteration and, when the value differs from the previous one, records micros()
// and pushes the new mask onto a 32-slot queue. The host's polling therefore
// determines only when it *learns* of a transition, not the instant recorded —
// which is the whole point of this API over [Box.PollButtons].
type Event struct {
	// Mask is the state of the 8 input lines after the transition, in the same
	// inverted convention as [Box.PollButtons]: bit N is 1 when line N is
	// pulled LOW ("pressed"). An unconnected line reads 0.
	Mask uint8

	// Micros is the raw device micros() reading at the transition. It is a
	// 32-bit counter that ticks in 4 µs steps and wraps every ~71.6 minutes;
	// feed it through an [Unwrapper] before doing arithmetic across a long run.
	Micros uint32
}

// EventReply is the full result of one [Box.GetEvent] call.
//
// Present and Overflow are separate because they are independent: the queue can
// report an overflow on a poll that returns no event.
type EventReply struct {
	// Event is meaningful only when Present is true.
	Event Event

	// Present is false when the queue was empty.
	Present bool

	// Overflow reports that the device's 32-slot event queue filled up since
	// the last poll. The flag is sticky on the device and cleared by the read
	// that reports it.
	//
	// An overflow means transitions were LOST, not delayed, so an affected
	// trial should be treated as suspect rather than merely late.
	Overflow bool
}

// Flag bits in the first byte of a get_event reply. They match the firmware.
const (
	eventFlagPresent  uint8 = 0x01
	eventFlagOverflow uint8 = 0x02
)

// GetEvent pops the oldest timestamped input event from the device queue
// (opcode 21).
//
// The reply is always 6 bytes even when the queue is empty, so the host never
// has to guess the reply length or distinguish "no event" from a timeout.
//
// Requires firmware advertising [CapTimestamps]; older firmware ignores the
// opcode and sends nothing, which surfaces as [ErrTimeout].
func (b *Box) GetEvent() (EventReply, error) {
	if err := b.tx([]byte{opGetEvent}); err != nil {
		return EventReply{}, err
	}
	buf, err := b.rxExact(6)
	if err != nil {
		return EventReply{}, err
	}
	return EventReply{
		Event: Event{
			Mask:   buf[1],
			Micros: binary.LittleEndian.Uint32(buf[2:6]),
		},
		Present:  buf[0]&eventFlagPresent != 0,
		Overflow: buf[0]&eventFlagOverflow != 0,
	}, nil
}

// GetMicros returns the device's current micros() reading (opcode 22).
//
// It is the primitive behind host↔device clock alignment; see [Box.SampleClock]
// for an estimator that accounts for the round trip rather than attributing the
// whole of it to one direction.
func (b *Box) GetMicros() (uint32, error) {
	if err := b.tx([]byte{opGetMicros}); err != nil {
		return 0, err
	}
	buf, err := b.rxExact(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(buf), nil
}

// ClearEvents discards every queued event, clears the overflow flag and
// re-seeds the firmware's change detector to the live input state (opcode 23).
//
// Call it at the start of a trial so that transitions from the previous one —
// including the release of a button held across the boundary — cannot be
// mistaken for a response. It sends no reply.
func (b *Box) ClearEvents() error {
	return b.tx([]byte{opClearEvents})
}

// SetDebounce sets the firmware's input debounce interval (opcode 24), rounded
// to the nearest microsecond and limited to 65535 µs. Zero disables debouncing,
// which is the firmware default.
//
// Debouncing is off by default on purpose: fibre-optic response pads do not
// bounce, and suppressing a real transition is worse than reporting an extra
// one. Enable it only for mechanical switches, and only after confirming the
// chatter is real.
func (b *Box) SetDebounce(d time.Duration) error {
	us := int64(d.Round(time.Microsecond) / time.Microsecond)
	if us < 0 || us > 65535 {
		return fmt.Errorf("%w: got %d µs", ErrBadDebounce, us)
	}
	u16 := encodeU16LE(uint16(us))
	return b.tx([]byte{opSetDebounce, u16[0], u16[1]})
}

// DrainEvents pops events until the queue is empty and returns how many it
// discarded, together with whether an overflow was reported while draining.
//
// Unlike [Box.ClearEvents] this costs one round trip per queued event; prefer
// ClearEvents unless the events themselves are wanted. max bounds the work so a
// device transitioning faster than the host can drain cannot loop forever; it
// is reached only if the input is chattering.
func (b *Box) DrainEvents(max int) (n int, overflow bool, err error) {
	for n < max {
		r, err := b.GetEvent()
		if err != nil {
			return n, overflow, err
		}
		overflow = overflow || r.Overflow
		if !r.Present {
			return n, overflow, nil
		}
		n++
	}
	return n, overflow, nil
}
