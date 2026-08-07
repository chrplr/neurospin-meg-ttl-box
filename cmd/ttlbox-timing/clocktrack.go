// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Opus 5.
// Licensed under the Apache License, Version 2.0 (see LICENSE.txt).

package main

import (
	"fmt"
	"time"

	ttlbox "github.com/neurospin/neurospin-meg-ttl-box"
)

// clockTracker samples the host↔device clock relationship alongside a
// measurement block and records every sample.
//
// It exists because a device timestamp is only as useful as the mapping onto
// host time, and that mapping is neither free nor constant: the AVR's ceramic
// resonator and the host's crystal do not tick at the same rate, so an offset
// taken once at the start drifts linearly with elapsed time. Sampling
// throughout lets the analysis fit offset and rate together, and — more to the
// point — lets it report the fit's residuals, which are the honest accuracy of
// device→host conversion rather than the device's 4 µs tick.
type clockTracker struct {
	clock  *ttlbox.Clock
	rec    *recorder
	origin time.Time
	n      int
}

// newClockTracker starts tracking and writes <name><tag>-clock.csv, so the
// samples share a filename stem with the run they belong to.
func newClockTracker(box *ttlbox.Box, name string, origin time.Time) (*clockTracker, error) {
	rec, err := newRecorderExact(name+flagTag+"-clock",
		"sample", "host_us", "device_us", "roundtrip_us")
	if err != nil {
		return nil, err
	}
	return &clockTracker{clock: box.NewClock(), rec: rec, origin: origin}, nil
}

// Sample takes the best of k round trips and records it.
func (c *clockTracker) Sample(k int) error {
	s, err := c.clock.Sample(k)
	if err != nil {
		return err
	}
	c.rec.Row(c.n, s.Host.Sub(c.origin), s.Device, s.RoundTrip)
	c.n++
	return nil
}

// Unwrap converts a raw device reading through the tracker's shared wrap state.
// Event timestamps and clock samples must pass through the same [ttlbox.Clock]
// so they agree about which side of a wrap they are on.
func (c *clockTracker) Unwrap(raw uint32) ttlbox.DeviceTime { return c.clock.Unwrap(raw) }

// HostTime converts a device timestamp to host time using every sample so far.
func (c *clockTracker) HostTime(d ttlbox.DeviceTime) time.Time { return c.clock.HostTime(d) }

// Close reports the fit and closes the file. The fit is printed rather than
// written to the CSV so the raw samples stay the only thing in the file, and so
// a run script's log carries the summary.
func (c *clockTracker) Close() error {
	if fit, ok := c.clock.Fit(); ok {
		fmt.Printf("  clock: %d samples over %s — rate error %+.2f ppm, "+
			"residual RMS %v, max %v\n",
			fit.N, fit.Span.Round(time.Second), fit.PPM,
			fit.ResidualRMS, fit.ResidualMax)
		if fit.Span < time.Minute {
			fmt.Println("  clock: span under a minute — the ppm figure is round-trip noise, not drift")
		}
	}
	return c.rec.Close()
}
