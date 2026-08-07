// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Opus 5.
// Licensed under the Apache License, Version 2.0 (see LICENSE.txt).

package ttlbox

import (
	"fmt"
	"math"
	"time"
)

// DeviceTime is a device micros() reading unwrapped to 64 bits: microseconds
// since the firmware started, free of the 32-bit counter's ~71.6 minute wrap.
type DeviceTime uint64

// Duration converts a device timestamp to a time.Duration since firmware start.
func (t DeviceTime) Duration() time.Duration { return time.Duration(t) * time.Microsecond }

// microsHalfRange is half the span of the device's 32-bit micros() counter.
// A jump larger than this in either direction is read as a wrap rather than as
// a genuine 35-minute step, which no measurement in this package produces.
const microsHalfRange = uint32(1) << 31

// Unwrapper turns the device's 32-bit micros() readings into monotonically
// increasing [DeviceTime] values.
//
// It must see every reading from one device in roughly the order they were
// produced, and at least once per wrap period (~71.6 min) — both of which any
// polling loop satisfies comfortably. The zero value is ready to use.
//
// Readings that arrive slightly out of order are tolerated: an event
// timestamped just before the get_micros call that follows it is a small step
// backwards, not a wrap, and only steps larger than half the counter range are
// treated as wraps.
type Unwrapper struct {
	started bool
	last    uint32
	epochs  uint64
}

// Unwrap converts one raw micros() reading to a [DeviceTime].
func (u *Unwrapper) Unwrap(raw uint32) DeviceTime {
	if !u.started {
		u.started = true
		u.last = raw
		return DeviceTime(raw)
	}

	epochs := u.epochs
	switch {
	case raw < u.last && u.last-raw > microsHalfRange:
		// The counter wrapped since the last reading.
		u.epochs++
		epochs = u.epochs
		u.last = raw
	case raw > u.last && raw-u.last > microsHalfRange:
		// A reading produced just BEFORE the most recent wrap, delivered after
		// it — a queued event drained late. It belongs to the previous epoch,
		// and `last` must not be rewound across the wrap.
		if epochs > 0 {
			epochs--
		}
	default:
		u.last = raw
	}
	return DeviceTime(epochs)<<32 | DeviceTime(raw)
}

// ClockSample is one host↔device clock comparison, obtained by bracketing a
// get_micros round trip between two host clock readings.
type ClockSample struct {
	// Host is the midpoint of the round trip: the host's best estimate of the
	// instant the device read its own clock.
	Host time.Time
	// Device is the device's reading at that instant.
	Device DeviceTime
	// RoundTrip is how long the whole exchange took. It bounds the error in
	// Host: the true instant lies within ±RoundTrip/2 of it, and only a
	// perfectly symmetric link makes the midpoint exact.
	RoundTrip time.Duration
}

// Offset returns the host time corresponding to device time zero, from this
// sample alone.
func (s ClockSample) Offset() time.Time { return s.Host.Add(-s.Device.Duration()) }

// Clock maps device timestamps onto the host clock.
//
// It owns an [Unwrapper] and a history of [ClockSample]s, and converts with a
// least-squares fit over that history once it has two or more samples. The fit
// matters over a long run: the AVR's ceramic resonator does not tick at exactly
// the same rate as the host's crystal, so a single offset taken at the start
// accumulates error linearly with elapsed time.
//
// Clock is not safe for concurrent use.
type Clock struct {
	box     *Box
	unwrap  Unwrapper
	samples []ClockSample
}

// NewClock returns a Clock that reads the device's micros() through b.
func (b *Box) NewClock() *Clock { return &Clock{box: b} }

// Unwrap exposes the Clock's shared [Unwrapper], so event timestamps and clock
// samples pass through the same wrap-tracking state.
func (c *Clock) Unwrap(raw uint32) DeviceTime { return c.unwrap.Unwrap(raw) }

// Sample estimates the host↔device offset from the best of k round trips and
// records it in the Clock's history.
//
// Each round trip is bracketed as h1 → get_micros → h2, and the device instant
// is taken to be the midpoint (h1+h2)/2 — Cristian's algorithm. The midpoint is
// exact only if the two directions take equally long, so the shortest round
// trip of the k is kept: it is the one with the least room for asymmetry.
//
// k must be at least 1; 20 is a reasonable default, costing about 20 round
// trips (a few tens of milliseconds).
func (c *Clock) Sample(k int) (ClockSample, error) {
	if k < 1 {
		return ClockSample{}, fmt.Errorf("ttlbox: clock sample count must be >= 1, got %d", k)
	}
	var best ClockSample
	for i := range k {
		h1 := time.Now()
		raw, err := c.box.GetMicros()
		h2 := time.Now()
		if err != nil {
			return ClockSample{}, err
		}
		rt := h2.Sub(h1)
		if i > 0 && rt >= best.RoundTrip {
			// Still unwrap, so the wrap tracker sees every reading.
			c.unwrap.Unwrap(raw)
			continue
		}
		best = ClockSample{
			Host:      h1.Add(rt / 2),
			Device:    c.unwrap.Unwrap(raw),
			RoundTrip: rt,
		}
	}
	c.samples = append(c.samples, best)
	return best, nil
}

// Samples returns the recorded clock samples, oldest first.
func (c *Clock) Samples() []ClockSample { return c.samples }

// HostTime converts a device timestamp to host time.
//
// With two or more samples it uses the least-squares fit, which corrects both
// offset and rate; with one it uses that sample's offset and assumes the clocks
// run at the same rate. It panics if no sample has been taken — a conversion
// with no reference is a programming error, not a runtime condition.
func (c *Clock) HostTime(d DeviceTime) time.Time {
	switch len(c.samples) {
	case 0:
		panic("ttlbox: Clock.HostTime before any Clock.Sample")
	case 1:
		return c.samples[0].Offset().Add(d.Duration())
	}
	f, _ := c.Fit()
	return f.HostTime(d)
}

// EventHost is the common case: unwrap an event's raw timestamp and convert it
// to host time in one step.
func (c *Clock) EventHost(ev Event) time.Time { return c.HostTime(c.Unwrap(ev.Micros)) }

// ClockFit is a least-squares fit of host time against device time.
type ClockFit struct {
	// N is the number of samples the fit is based on.
	N int
	// Span is the device-time interval the samples cover. A rate estimated
	// over a short span is dominated by round-trip noise; quote PPM only when
	// Span is minutes or more.
	Span time.Duration
	// Rate is host seconds per device second. 1.0 means the two clocks agree;
	// above 1.0 the device runs slow.
	Rate float64
	// PPM is (Rate-1)e6, the device's rate error in parts per million.
	PPM float64
	// ResidualRMS and ResidualMax describe how far the samples fall from the
	// fitted line. They are the measured accuracy of device→host conversion,
	// and the honest figure to quote for reaction-time accuracy — not the
	// device's 4 µs micros() tick.
	ResidualRMS time.Duration
	ResidualMax time.Duration

	// origin, meanX and meanY carry the fit; see HostTime.
	origin       time.Time
	meanX, meanY float64
}

// HostTime converts a device timestamp using the fit.
func (f ClockFit) HostTime(d DeviceTime) time.Time {
	us := f.meanY + f.Rate*(float64(d)-f.meanX)
	return f.origin.Add(time.Duration(math.Round(us)) * time.Microsecond)
}

// Fit computes the least-squares mapping from device time to host time over the
// recorded samples. ok is false when fewer than two samples exist, or when they
// all share one device timestamp and no rate can be estimated.
//
// The regression is centred on the sample means before summing, because raw
// device microseconds reach ~4e9 and their uncorrected squares would lose
// milliseconds of precision in float64.
func (c *Clock) Fit() (fit ClockFit, ok bool) {
	if len(c.samples) < 2 {
		return ClockFit{}, false
	}
	origin := c.samples[0].Host

	xs := make([]float64, len(c.samples))
	ys := make([]float64, len(c.samples))
	var sumX, sumY float64
	for i, s := range c.samples {
		xs[i] = float64(s.Device)
		ys[i] = float64(s.Host.Sub(origin).Microseconds())
		sumX += xs[i]
		sumY += ys[i]
	}
	n := float64(len(c.samples))
	meanX, meanY := sumX/n, sumY/n

	var sxx, sxy float64
	for i := range xs {
		dx := xs[i] - meanX
		sxx += dx * dx
		sxy += dx * (ys[i] - meanY)
	}
	if sxx == 0 {
		return ClockFit{}, false
	}
	rate := sxy / sxx

	fit = ClockFit{
		N:      len(c.samples),
		Span:   time.Duration(xs[len(xs)-1]-xs[0]) * time.Microsecond,
		Rate:   rate,
		PPM:    (rate - 1) * 1e6,
		origin: origin,
		meanX:  meanX,
		meanY:  meanY,
	}

	var sumSq, worst float64
	for i := range xs {
		r := ys[i] - (meanY + rate*(xs[i]-meanX))
		sumSq += r * r
		if math.Abs(r) > math.Abs(worst) {
			worst = r
		}
	}
	fit.ResidualRMS = time.Duration(math.Round(math.Sqrt(sumSq/n))) * time.Microsecond
	fit.ResidualMax = time.Duration(math.Round(math.Abs(worst))) * time.Microsecond
	return fit, true
}
