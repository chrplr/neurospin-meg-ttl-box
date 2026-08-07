// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Opus 5.
// Licensed under the Apache License, Version 2.0 (see LICENSE.txt).

package main

import (
	"fmt"
	"time"

	ttlbox "github.com/neurospin/neurospin-meg-ttl-box"
	"github.com/spf13/cobra"
)

var (
	latPulses     int
	latISIMs      int
	latWidthMs    int
	latLine       uint8
	latClockEvery int
	latCondition  string
)

var latencyCmd = &cobra.Command{
	Use:   "latency",
	Short: "How long from the host's write to the line moving (blocks B3 and B9)",
	Long: `Pulse one output line many times and record, for every pulse, when the host
issued the command and when the firmware saw the resulting edge come back
through the loopback.

The difference is the host->device latency: the part of a trigger's timing that
no amount of firmware timestamping can remove, and the reason a parallel port
remains the reference for trigger onset. It is dominated by one USB full-speed
frame (1 ms) plus the ~174 µs two command bytes take on the 16u2-to-2560 UART at
115200 baud.

The same run serves two blocks. With a BBTKv3 capturing (B3), the recorded
onset-to-onset intervals give an external view that owes nothing to the box's
own clock. Without an instrument (B9) it still gives the jitter and the tail at
whatever sample size is wanted — and the tail needs thousands of trials, not the
fifty a previous session could afford.

Two caveats belong on the result, and the second is the important one.

A serial Write returns when the kernel accepts the bytes, not when they reach
the wire, so every figure here bounds the host's contribution from above.

And an ABSOLUTE latency has to convert a device timestamp into host time, which
costs the accuracy of the clock-offset estimate. That estimate is bounded by the
asymmetry of a get_micros round trip, and the round trip has a floor of about
2.4 ms on this link — one USB frame each way plus four reply bytes at 115200 on
the 16u2 UART. So the offset can be wrong by more than the ~1.5 ms being
measured, and no amount of sampling reduces it. Absolute latency is reported,
but the figures to quote and to compare between conditions are the ones that
stay inside a single clock: the host round trip printed alongside it, and the
BBTK's onset-to-onset intervals. See measurements/analyse-timing.py, which
reports the offset bound explicitly.

Conditions worth running and comparing with --condition and --tag: an idle host,
a host under load (stress-ng), and a real-time priority (chrt -f 50).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runLatency()
	},
}

func init() {
	f := latencyCmd.Flags()
	f.IntVar(&latPulses, "pulses", 1000, "number of pulses")
	f.IntVar(&latISIMs, "isi", 200, "ms between pulse onsets")
	f.IntVar(&latWidthMs, "width", 5, "pulse width in ms")
	f.Uint8Var(&latLine, "line", 0, "output line to pulse (0-7)")
	f.IntVar(&latClockEvery, "clock-every", 60, "seconds between host/device clock samples")
	f.StringVar(&latCondition, "condition", "idle",
		"label for the host condition, recorded in the CSV (e.g. idle, load, rt)")
}

func latencyPlan() blockPlan {
	d := time.Duration(latPulses) * time.Duration(latISIMs) * time.Millisecond
	return blockPlan{
		Name: "latency",
		Steps: []string{
			fmt.Sprintf("%d pulses of %d ms on line %d, ISI %d ms (%s)",
				latPulses, latWidthMs, latLine, latISIMs, d.Round(time.Second)),
			fmt.Sprintf("host condition recorded as %q", latCondition),
		},
		Duration: d + markerDuration() + 2*time.Second,
	}
}

// minClockSamples is the fewest host/device clock comparisons a run should
// gather, whatever --clock-every says. Below about this many the least-squares
// fit cannot tell the device's drift from the noise of its own sampling.
const minClockSamples = 20

func runLatency() error {
	if latISIMs <= latWidthMs {
		return fmt.Errorf("--isi (%d ms) must exceed --width (%d ms), or consecutive pulses merge",
			latISIMs, latWidthMs)
	}
	plan := latencyPlan()
	if preflight(plan) {
		return nil
	}

	box, _, err := openBox(ttlbox.CapTimestamps)
	if err != nil {
		return err
	}
	defer box.Close()

	rec, err := newRecorder("latency",
		"condition", "trial", "host_write_us", "host_recv_us",
		"dev_rise_us", "n_events", "overflow")
	if err != nil {
		return err
	}
	defer rec.Close()

	origin := time.Now()
	clk, err := newClockTracker(box, "latency", origin)
	if err != nil {
		return err
	}
	defer clk.Close()
	if err := clk.Sample(20); err != nil {
		return err
	}

	if err := marker(box, latLine); err != nil {
		return err
	}
	if err := box.SetTriggerDuration(time.Duration(latWidthMs) * time.Millisecond); err != nil {
		return err
	}

	isi := time.Duration(latISIMs) * time.Millisecond

	// Take at least minClockSamples over the run however short it is. The
	// flag's value is a ceiling, not a schedule: a 200 s run at the default
	// 60 s cadence yields five samples, which cannot separate the device's
	// drift from the noise of the sampling round trip, and leaves the offset
	// resting on almost nothing.
	clockEvery := time.Duration(latClockEvery) * time.Second
	if spread := plan.Duration / minClockSamples; spread < clockEvery {
		clockEvery = max(spread, time.Second)
	}
	nextClock := time.Now().Add(clockEvery)

	// Kept in memory only to print a summary at the end; the CSV stays raw so
	// the analysis can redo this with the completed clock fit.
	type trial struct {
		hostWrite time.Time
		hostRecv  time.Time
		devRise   ttlbox.DeviceTime
		ok        bool
	}
	trials := make([]trial, 0, latPulses)
	lost := 0

	fmt.Printf("  %d pulses of %d ms on line %d\n", latPulses, latWidthMs, latLine)
	for i := range latPulses {
		next := time.Now().Add(isi)
		if err := box.ClearEvents(); err != nil {
			return err
		}
		hostWrite := time.Now()
		if err := box.SendTriggerMask(1 << latLine); err != nil {
			return err
		}

		// Only the rising edge is wanted; the falling edge arrives later and is
		// left in the queue for the next ClearEvents to discard.
		evs, overflow, err := collectEvents(box, 1, 0, 500*time.Millisecond)
		if err != nil {
			return err
		}

		var rise ttlbox.DeviceTime
		var recv time.Duration
		t := trial{hostWrite: hostWrite}
		if len(evs) > 0 {
			rise = clk.Unwrap(evs[0].Event.Micros)
			recv = evs[0].Host.Sub(origin)
			t.devRise, t.hostRecv, t.ok = rise, evs[0].Host, true
		} else {
			lost++
		}
		trials = append(trials, t)
		rec.Row(latCondition, i, hostWrite.Sub(origin), recv, rise, len(evs), overflow)

		if time.Now().After(nextClock) {
			if err := clk.Sample(20); err != nil {
				return err
			}
			nextClock = time.Now().Add(clockEvery)
		}
		sleepUntil(next)
	}
	if err := clk.Sample(20); err != nil {
		return err
	}

	// Report the offset-free figure first. Host write to host receipt uses only
	// the host clock, so it needs no estimate of anything and bounds the write
	// latency from above; the absolute latency below has to cross clocks, and
	// pays the offset error to do it.
	var trip, lat []time.Duration
	for _, t := range trials {
		if !t.ok {
			continue
		}
		trip = append(trip, t.hostRecv.Sub(t.hostWrite))
		lat = append(lat, clk.HostTime(t.devRise).Sub(t.hostWrite))
	}
	fmt.Printf("\n  host write -> host learns of edge (%s): %v\n", latCondition, summarise(trip))
	fmt.Printf("  absolute host->device latency:      %v\n", summarise(lat))
	fmt.Println("  The second line crosses clocks and inherits the offset estimate;")
	fmt.Println("  ./measurements/analyse-timing.py reports how much error that carries.")
	if lost > 0 {
		fmt.Printf("  %d of %d pulses produced no event — investigate before using this run\n",
			lost, latPulses)
	}
	fmt.Printf("  wrote %s\n", rec.Path)
	return nil
}
