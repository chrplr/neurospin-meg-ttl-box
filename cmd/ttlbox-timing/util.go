// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Opus 5.
// Licensed under the Apache License, Version 2.0 (see LICENSE.txt).

package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	ttlbox "github.com/neurospin/neurospin-meg-ttl-box"
)

// recorder writes one CSV of raw per-trial rows.
//
// Blocks record raw rows and nothing else: a summary computed here could not be
// recomputed, re-cut or challenged later, and hardware time is too expensive to
// spend on data that only supports one analysis.
type recorder struct {
	Path string
	f    *os.File
	w    *csv.Writer
	err  error
}

// newRecorder creates <out>/<name><tag>.csv and writes the header.
func newRecorder(name string, header ...string) (*recorder, error) {
	return newRecorderExact(name+flagTag, header...)
}

// newRecorderExact is newRecorder without the automatic --tag suffix, for files
// that must sit alongside a tagged one under a related name. The clock samples
// belong to a specific run and are paired with it by filename stem, so they
// have to be <name><tag>-clock.csv and not <name>-clock<tag>.csv.
func newRecorderExact(stem string, header ...string) (*recorder, error) {
	if err := os.MkdirAll(flagOutDir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(flagOutDir, stem+".csv")
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	r := &recorder{Path: path, f: f, w: csv.NewWriter(f)}
	r.err = r.w.Write(header)
	return r, r.err
}

// Row appends one row. Errors are held until Close so a measurement loop stays
// readable; nothing is lost, because a failed write fails the whole file.
func (r *recorder) Row(vals ...any) {
	if r.err != nil {
		return
	}
	rec := make([]string, len(vals))
	for i, v := range vals {
		rec[i] = field(v)
	}
	r.err = r.w.Write(rec)
}

// field renders one value. Durations become microseconds with 3 decimals,
// which is below the device's 4 µs tick and well below anything the BBTK can
// resolve, so no measurement is quantised by the file format.
func field(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.Itoa(x)
	case uint8:
		return strconv.FormatUint(uint64(x), 10)
	case uint32:
		return strconv.FormatUint(uint64(x), 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case ttlbox.DeviceTime:
		return strconv.FormatUint(uint64(x), 10)
	case float64:
		return strconv.FormatFloat(x, 'f', 3, 64)
	case time.Duration:
		return strconv.FormatFloat(float64(x)/float64(time.Microsecond), 'f', 3, 64)
	default:
		return fmt.Sprint(v)
	}
}

// Close flushes and closes the file, reporting the first error seen.
func (r *recorder) Close() error {
	r.w.Flush()
	if r.err == nil {
		r.err = r.w.Error()
	}
	if cerr := r.f.Close(); r.err == nil {
		r.err = cerr
	}
	return r.err
}

// stampedEvent is a device event together with the host time at which the poll
// that returned it completed.
//
// The two timestamps answer different questions: Event.Micros is when the edge
// happened, Host is when the host found out. Their difference is the notify
// latency, and it is the part of a reaction time that firmware timestamping
// removes from the measurement but not from a closed loop.
type stampedEvent struct {
	Event ttlbox.Event
	Host  time.Time
}

// collectEvents polls the device event queue until want events have arrived or
// timeout elapses. poll is the sleep between polls; 0 polls as fast as the link
// allows.
//
// It returns everything collected even on timeout, because a trial that yielded
// one edge instead of two is data — it says the second edge was lost or late —
// and discarding it would quietly bias the result towards the successes.
func collectEvents(box *ttlbox.Box, want int, poll, timeout time.Duration) (evs []stampedEvent, overflow bool, err error) {
	deadline := time.Now().Add(timeout)
	for len(evs) < want {
		r, err := box.GetEvent()
		now := time.Now()
		if err != nil {
			return evs, overflow, err
		}
		overflow = overflow || r.Overflow
		if r.Present {
			evs = append(evs, stampedEvent{Event: r.Event, Host: now})
			continue // drain what is queued before sleeping again
		}
		if now.After(deadline) {
			return evs, overflow, nil
		}
		if poll > 0 {
			time.Sleep(poll)
		}
	}
	return evs, overflow, nil
}

// collectUntilBit polls until the given input bit changes state, returning
// every event seen on the way, or everything collected when timeout elapses.
//
// A block that waits for an external device's answer cannot say in advance how
// many events precede it: its own trigger contributes a rising and a falling
// edge on another line, and how they interleave with the answer depends on the
// delay being measured. Waiting for the line that matters is the only condition
// that holds across the whole sweep.
func collectUntilBit(box *ttlbox.Box, bit, baseline uint8, poll, timeout time.Duration) (evs []stampedEvent, overflow bool, err error) {
	deadline := time.Now().Add(timeout)
	m := uint8(1) << bit
	prev := baseline
	for {
		r, err := box.GetEvent()
		now := time.Now()
		if err != nil {
			return evs, overflow, err
		}
		overflow = overflow || r.Overflow
		if r.Present {
			evs = append(evs, stampedEvent{Event: r.Event, Host: now})
			changed := (r.Event.Mask^prev)&m != 0
			prev = r.Event.Mask
			if changed {
				return evs, overflow, nil
			}
			continue
		}
		if now.After(deadline) {
			return evs, overflow, nil
		}
		if poll > 0 {
			time.Sleep(poll)
		}
	}
}

// firstChangeOn returns the first event in evs at which the given input bit
// changed state, and whether one was found.
//
// Blocks look for a specific line rather than for "the next event" because the
// firmware reports the whole 8-bit port: one sample can carry a change on two
// lines at once, which is exactly what the atomicity block is testing for.
func firstChangeOn(evs []stampedEvent, bit uint8, prev uint8) (stampedEvent, bool) {
	m := uint8(1) << bit
	for _, e := range evs {
		if (e.Event.Mask^prev)&m != 0 {
			return e, true
		}
		prev = e.Event.Mask
	}
	return stampedEvent{}, false
}

// sleepUntil sleeps until the given instant, so a schedule does not accumulate
// the cost of the work done inside each iteration.
func sleepUntil(t time.Time) {
	if d := time.Until(t); d > 0 {
		time.Sleep(d)
	}
}
