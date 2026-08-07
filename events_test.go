// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Opus 5.
// Licensed under the Apache License, Version 2.0 (see LICENSE.txt).

package ttlbox

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

// eventReply builds the 6-byte get_event reply the firmware sends.
func eventReply(flags, mask uint8, micros uint32) []byte {
	return []byte{
		flags, mask,
		byte(micros), byte(micros >> 8), byte(micros >> 16), byte(micros >> 24),
	}
}

func TestGetEventPresent(t *testing.T) {
	b, tx := newMockBox(eventReply(eventFlagPresent, 0x04, 123456))

	r, err := b.GetEvent()
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	if got := tx.Bytes(); !bytes.Equal(got, []byte{opGetEvent}) {
		t.Errorf("sent % X, want % X", got, []byte{opGetEvent})
	}
	if !r.Present {
		t.Error("Present = false, want true")
	}
	if r.Overflow {
		t.Error("Overflow = true, want false")
	}
	if r.Event.Mask != 0x04 || r.Event.Micros != 123456 {
		t.Errorf("got mask 0x%02X micros %d, want 0x04 / 123456", r.Event.Mask, r.Event.Micros)
	}
}

// The reply is a fixed 6 bytes even when the queue is empty, so an empty queue
// is never mistaken for a timeout.
func TestGetEventEmptyQueue(t *testing.T) {
	b, _ := newMockBox(eventReply(0, 0, 0))

	r, err := b.GetEvent()
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	if r.Present {
		t.Error("Present = true on an empty queue")
	}
}

// Overflow is independent of Present: the flag is sticky on the device and can
// be reported by a poll that returns no event.
func TestGetEventOverflowWithoutEvent(t *testing.T) {
	b, _ := newMockBox(eventReply(eventFlagOverflow, 0, 0))

	r, err := b.GetEvent()
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	if r.Present {
		t.Error("Present = true, want false")
	}
	if !r.Overflow {
		t.Error("Overflow = false, want true")
	}
}

func TestGetEventOverflowWithEvent(t *testing.T) {
	b, _ := newMockBox(eventReply(eventFlagPresent|eventFlagOverflow, 0xFF, 7))

	r, err := b.GetEvent()
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	if !r.Present || !r.Overflow {
		t.Errorf("got present=%t overflow=%t, want both true", r.Present, r.Overflow)
	}
}

// A 6-byte reply arriving in two packets must be reassembled, not reported as
// a timeout: at thousands of polls per session, dropping the split ones would
// bias every latency figure derived from them.
func TestGetEventReplySplitAcrossReads(t *testing.T) {
	full := eventReply(eventFlagPresent, 0x81, 0xDEADBEEF)
	b, _ := newChunkedBox(full[:1], full[1:4], full[4:])

	r, err := b.GetEvent()
	if err != nil {
		t.Fatalf("GetEvent across split reads: %v", err)
	}
	if !r.Present || r.Event.Mask != 0x81 || r.Event.Micros != 0xDEADBEEF {
		t.Errorf("got %+v, want mask 0x81 micros 0xDEADBEEF present", r)
	}
}

func TestGetEventTruncatedReply(t *testing.T) {
	b, _ := newChunkedBox([]byte{eventFlagPresent, 0x01})

	_, err := b.GetEvent()
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("got %v, want ErrTimeout", err)
	}
}

func TestGetMicros(t *testing.T) {
	b, tx := newMockBox([]byte{0x40, 0xE2, 0x01, 0x00}) // 123456 LE

	us, err := b.GetMicros()
	if err != nil {
		t.Fatalf("GetMicros: %v", err)
	}
	if got := tx.Bytes(); !bytes.Equal(got, []byte{opGetMicros}) {
		t.Errorf("sent % X, want % X", got, []byte{opGetMicros})
	}
	if us != 123456 {
		t.Errorf("got %d, want 123456", us)
	}
}

func TestClearEvents(t *testing.T) {
	b, tx := newMockBox(nil)

	if err := b.ClearEvents(); err != nil {
		t.Fatalf("ClearEvents: %v", err)
	}
	if got := tx.Bytes(); !bytes.Equal(got, []byte{opClearEvents}) {
		t.Errorf("sent % X, want % X", got, []byte{opClearEvents})
	}
}

func TestSetDebounce(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want []byte
	}{
		{"zero disables", 0, []byte{opSetDebounce, 0x00, 0x00}},
		{"1 ms", time.Millisecond, []byte{opSetDebounce, 0xE8, 0x03}},
		{"max", 65535 * time.Microsecond, []byte{opSetDebounce, 0xFF, 0xFF}},
		{"rounds to nearest µs", 1500 * time.Nanosecond, []byte{opSetDebounce, 0x02, 0x00}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, tx := newMockBox(nil)
			if err := b.SetDebounce(tt.d); err != nil {
				t.Fatalf("SetDebounce(%v): %v", tt.d, err)
			}
			if got := tx.Bytes(); !bytes.Equal(got, tt.want) {
				t.Errorf("sent % X, want % X", got, tt.want)
			}
		})
	}
}

func TestSetDebounceOutOfRange(t *testing.T) {
	for _, d := range []time.Duration{-time.Microsecond, 66 * time.Millisecond} {
		b, _ := newMockBox(nil)
		if err := b.SetDebounce(d); !errors.Is(err, ErrBadDebounce) {
			t.Errorf("SetDebounce(%v) = %v, want ErrBadDebounce", d, err)
		}
	}
}

func TestDrainEvents(t *testing.T) {
	var rx []byte
	for i := range 3 {
		rx = append(rx, eventReply(eventFlagPresent, uint8(1<<i), uint32(i))...)
	}
	rx = append(rx, eventReply(0, 0, 0)...) // queue now empty
	b, _ := newMockBox(rx)

	n, overflow, err := b.DrainEvents(100)
	if err != nil {
		t.Fatalf("DrainEvents: %v", err)
	}
	if n != 3 {
		t.Errorf("drained %d, want 3", n)
	}
	if overflow {
		t.Error("overflow reported, want none")
	}
}

// An input chattering faster than the host can drain must not wedge the caller.
func TestDrainEventsRespectsMax(t *testing.T) {
	var rx []byte
	for range 10 {
		rx = append(rx, eventReply(eventFlagPresent, 0x01, 0)...)
	}
	b, _ := newMockBox(rx)

	n, _, err := b.DrainEvents(4)
	if err != nil {
		t.Fatalf("DrainEvents: %v", err)
	}
	if n != 4 {
		t.Errorf("drained %d, want the max of 4", n)
	}
}

// The overflow flag must survive the drain even though it is reported by only
// one of the replies — losing it would turn "presses were lost" into silence.
func TestDrainEventsPropagatesOverflow(t *testing.T) {
	rx := append(eventReply(eventFlagPresent|eventFlagOverflow, 0x01, 1),
		eventReply(eventFlagPresent, 0x02, 2)...)
	rx = append(rx, eventReply(0, 0, 0)...)
	b, _ := newMockBox(rx)

	n, overflow, err := b.DrainEvents(100)
	if err != nil {
		t.Fatalf("DrainEvents: %v", err)
	}
	if n != 2 || !overflow {
		t.Errorf("got n=%d overflow=%t, want 2 / true", n, overflow)
	}
}
