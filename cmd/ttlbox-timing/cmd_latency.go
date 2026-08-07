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
onset-to-onset intervals give an external view of the jitter that owes nothing
to the box's own clock. Without an instrument (B9) it still gives the absolute
latency, at whatever sample size is wanted — the tail needs thousands of trials,
not the fifty a previous session could afford.

One caveat belongs on the result: a serial Write returns when the kernel accepts
the bytes, not when they reach the wire, so this is an upper bound on the host's
contribution. That is exactly why the BBTK arm exists.

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

func runLatency() error {
	if latISIMs <= latWidthMs {
		return fmt.Errorf("--isi (%d ms) must exceed --width (%d ms), or consecutive pulses merge",
			latISIMs, latWidthMs)
	}
	if preflight(latencyPlan()) {
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
	clockEvery := time.Duration(latClockEvery) * time.Second
	nextClock := time.Now().Add(clockEvery)

	// Kept in memory only to print a summary at the end; the CSV stays raw so
	// the analysis can redo this with the completed clock fit.
	type trial struct {
		hostWrite time.Time
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
			t.devRise, t.ok = rise, true
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

	// Convert with every clock sample now in hand, so the figures printed here
	// use the same fit the analysis will.
	var lat []time.Duration
	for _, t := range trials {
		if t.ok {
			lat = append(lat, clk.HostTime(t.devRise).Sub(t.hostWrite))
		}
	}
	fmt.Printf("\n  host->device latency (%s): %v\n", latCondition, summarise(lat))
	if lost > 0 {
		fmt.Printf("  %d of %d pulses produced no event — investigate before using this run\n",
			lost, latPulses)
	}
	fmt.Printf("  wrote %s\n", rec.Path)
	return nil
}
