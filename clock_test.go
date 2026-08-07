// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Opus 5.
// Licensed under the Apache License, Version 2.0 (see LICENSE.txt).

package ttlbox

import (
	"math"
	"testing"
	"time"
)

func TestUnwrapperMonotonicReadings(t *testing.T) {
	var u Unwrapper
	for _, raw := range []uint32{0, 1000, 2_000_000, 4_000_000_000} {
		if got := u.Unwrap(raw); got != DeviceTime(raw) {
			t.Errorf("Unwrap(%d) = %d, want %d", raw, got, raw)
		}
	}
}

// micros() wraps every ~71.6 minutes. A reading that steps back across nearly
// the whole counter range is a wrap, and the unwrapped value must keep rising.
func TestUnwrapperHandlesWrap(t *testing.T) {
	var u Unwrapper
	const nearMax = math.MaxUint32 - 1000

	before := u.Unwrap(nearMax)
	after := u.Unwrap(500) // wrapped: 1501 µs later

	if after <= before {
		t.Fatalf("after wrap: %d <= %d, want strictly increasing", after, before)
	}
	if got := after - before; got != 1501 {
		t.Errorf("elapsed across the wrap = %d µs, want 1501", got)
	}
}

// Readings do not always arrive in order: an event timestamped a few hundred
// microseconds before the get_micros call that follows it is a small step
// backwards. Treating that as a wrap would inject 71 minutes of error.
func TestUnwrapperSmallBackwardStepIsNotAWrap(t *testing.T) {
	var u Unwrapper
	u.Unwrap(1_000_000)

	if got := u.Unwrap(999_500); got != DeviceTime(999_500) {
		t.Errorf("Unwrap(999500) = %d, want 999500 (no wrap)", got)
	}
	if got := u.Unwrap(1_001_000); got != DeviceTime(1_001_000) {
		t.Errorf("Unwrap(1001000) = %d, want 1001000", got)
	}
}

// An event queued just before a wrap but drained just after it must stay in the
// old epoch rather than jumping forward by a full counter period.
func TestUnwrapperLateEventFromBeforeWrap(t *testing.T) {
	var u Unwrapper
	const nearMax = math.MaxUint32 - 1000

	u.Unwrap(nearMax)
	afterWrap := u.Unwrap(500)
	late := u.Unwrap(nearMax - 200) // queued 200 µs before the reading above

	if late >= afterWrap {
		t.Fatalf("late pre-wrap event unwrapped to %d, not before %d", late, afterWrap)
	}
	if got := afterWrap - late; got != 1701 {
		t.Errorf("gap = %d µs, want 1701", got)
	}
}

func TestDeviceTimeDuration(t *testing.T) {
	if got := DeviceTime(1_500_000).Duration(); got != 1500*time.Millisecond {
		t.Errorf("got %v, want 1.5s", got)
	}
}

func TestClockSampleOffset(t *testing.T) {
	host := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	s := ClockSample{Host: host, Device: 2_000_000}

	if got := s.Offset(); !got.Equal(host.Add(-2 * time.Second)) {
		t.Errorf("Offset = %v, want %v", got, host.Add(-2*time.Second))
	}
}

// newFittedClock builds a Clock from synthetic samples: a device running
// ppm parts-per-million slow relative to the host, sampled every 60 s, with the
// given per-sample host-time error added.
func newFittedClock(n int, ppm float64, jitter func(i int) time.Duration) *Clock {
	origin := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	c := &Clock{}
	for i := range n {
		dev := DeviceTime(i) * 60_000_000
		hostUs := float64(dev) * (1 + ppm/1e6)
		h := origin.Add(time.Duration(hostUs) * time.Microsecond)
		if jitter != nil {
			h = h.Add(jitter(i))
		}
		c.samples = append(c.samples, ClockSample{Host: h, Device: dev})
	}
	return c
}

func TestClockFitRecoversRate(t *testing.T) {
	const ppm = 35.0
	c := newFittedClock(30, ppm, nil)

	fit, ok := c.Fit()
	if !ok {
		t.Fatal("Fit reported not ok")
	}
	if fit.N != 30 {
		t.Errorf("N = %d, want 30", fit.N)
	}
	if math.Abs(fit.PPM-ppm) > 0.5 {
		t.Errorf("PPM = %.2f, want %.1f", fit.PPM, ppm)
	}
	if fit.Span != 29*60*time.Second {
		t.Errorf("Span = %v, want 29m", fit.Span)
	}
	if fit.ResidualRMS > time.Microsecond {
		t.Errorf("ResidualRMS = %v on noiseless samples, want ~0", fit.ResidualRMS)
	}
}

// The residuals are the point of the fit: they are the measured accuracy of
// device→host conversion, so they must reflect injected error rather than being
// absorbed into the slope.
func TestClockFitResidualsReflectJitter(t *testing.T) {
	alternating := func(i int) time.Duration {
		if i%2 == 0 {
			return 200 * time.Microsecond
		}
		return -200 * time.Microsecond
	}
	c := newFittedClock(40, 10, alternating)

	fit, ok := c.Fit()
	if !ok {
		t.Fatal("Fit reported not ok")
	}
	if got := fit.ResidualRMS; got < 150*time.Microsecond || got > 250*time.Microsecond {
		t.Errorf("ResidualRMS = %v, want ~200µs", got)
	}
	if got := fit.ResidualMax; got < 150*time.Microsecond || got > 250*time.Microsecond {
		t.Errorf("ResidualMax = %v, want ~200µs", got)
	}
	if math.Abs(fit.PPM-10) > 1 {
		t.Errorf("PPM = %.2f, want ~10 despite the jitter", fit.PPM)
	}
}

func TestClockFitNeedsTwoSamples(t *testing.T) {
	c := newFittedClock(1, 0, nil)
	if _, ok := c.Fit(); ok {
		t.Error("Fit ok with a single sample, want not ok")
	}
}

// Raw device microseconds reach ~4e9; an uncentred least-squares sum would lose
// milliseconds of precision there. Fit late in the counter's life and the
// recovered rate must be just as good as near zero.
func TestClockFitPrecisionLateInCounterLife(t *testing.T) {
	origin := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	c := &Clock{}
	const base = 4_000_000_000
	const ppm = 25.0
	for i := range 30 {
		dev := DeviceTime(base + i*60_000_000)
		hostUs := float64(dev) * (1 + ppm/1e6)
		c.samples = append(c.samples, ClockSample{
			Host:   origin.Add(time.Duration(hostUs) * time.Microsecond),
			Device: dev,
		})
	}

	fit, ok := c.Fit()
	if !ok {
		t.Fatal("Fit reported not ok")
	}
	if math.Abs(fit.PPM-ppm) > 0.5 {
		t.Errorf("PPM = %.3f, want %.1f", fit.PPM, ppm)
	}
	if fit.ResidualRMS > 2*time.Microsecond {
		t.Errorf("ResidualRMS = %v, want ~0", fit.ResidualRMS)
	}
}

// HostTime must round-trip a sampled device time back to the host time it was
// paired with.
func TestClockHostTimeRoundTrip(t *testing.T) {
	c := newFittedClock(10, 40, nil)

	for _, s := range c.samples {
		got := c.HostTime(s.Device)
		if d := got.Sub(s.Host); d > time.Microsecond || d < -time.Microsecond {
			t.Errorf("HostTime(%d) off by %v", s.Device, d)
		}
	}
}

// With one sample there is no rate to fit, and conversion falls back to that
// sample's offset.
func TestClockHostTimeSingleSample(t *testing.T) {
	host := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	c := &Clock{samples: []ClockSample{{Host: host, Device: 1_000_000}}}

	if got := c.HostTime(3_000_000); !got.Equal(host.Add(2 * time.Second)) {
		t.Errorf("HostTime = %v, want %v", got, host.Add(2*time.Second))
	}
}

func TestClockHostTimeWithoutSamplePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("HostTime with no samples did not panic")
		}
	}()
	c := &Clock{}
	c.HostTime(0)
}

func TestClockSampleKeepsShortestRoundTrip(t *testing.T) {
	// Three get_micros replies; Sample must issue one opcode per round trip
	// and unwrap every reading, not only the one it keeps.
	var rx []byte
	for _, us := range []uint32{1000, 2000, 3000} {
		rx = append(rx, byte(us), byte(us>>8), byte(us>>16), byte(us>>24))
	}
	b, tx := newMockBox(rx)
	c := b.NewClock()

	s, err := c.Sample(3)
	if err != nil {
		t.Fatalf("Sample: %v", err)
	}
	if got := tx.Len(); got != 3 {
		t.Errorf("sent %d bytes, want 3 (one opcode per round trip)", got)
	}
	if s.Device != 1000 && s.Device != 2000 && s.Device != 3000 {
		t.Errorf("Device = %d, want one of the three readings", s.Device)
	}
	if len(c.Samples()) != 1 {
		t.Errorf("recorded %d samples, want 1", len(c.Samples()))
	}
}

func TestClockSampleRejectsZeroCount(t *testing.T) {
	b, _ := newMockBox(nil)
	if _, err := b.NewClock().Sample(0); err == nil {
		t.Error("Sample(0) succeeded, want an error")
	}
}
