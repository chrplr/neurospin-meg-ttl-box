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
	driftDuration   time.Duration
	driftIntervalMs int
	driftWidthMs    int
	driftLine       uint8
	driftClockEvery int
)

var driftCmd = &cobra.Command{
	Use:   "drift",
	Short: "Rate error between the device, host and BBTK clocks (block B4)",
	Long: `Emit a slow, regular pulse train for a long time and record each pulse on
every clock available, so their rates can be compared.

The Mega's timebase is a ceramic resonator, not a crystal: it is specified in
the thousands of parts per million, where a host's crystal is in the tens. Over
a one-hour session that difference is not academic — 50 ppm is 180 ms — and a
device timestamp converted with an offset taken once at the start inherits all
of it. Nobody has measured how much this box actually drifts, which is why the
existing documentation can only say the client re-syncs every 20 minutes and
leave the resulting error unstated.

Three clocks see the same pulses: the host writes them, the box timestamps them
through its loopback, and a BBTKv3 capture (optional) timestamps them from
outside. Host against device needs no instrument and already resolves a fraction
of a ppm over an hour; the BBTK supplies an independent third opinion, which
matters because a two-clock comparison cannot say which one is wrong.

Runs longer than ~71 minutes cross the device's micros() wrap. That is handled,
but a BBTK capture has its own limits, so prefer several shorter sessions.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDrift()
	},
}

func init() {
	f := driftCmd.Flags()
	f.DurationVar(&driftDuration, "duration", 45*time.Minute, "total run time")
	f.IntVar(&driftIntervalMs, "interval", 1000, "ms between pulses")
	f.IntVar(&driftWidthMs, "width", 10, "pulse width in ms")
	f.Uint8Var(&driftLine, "line", 0, "output line to pulse (0-7)")
	f.IntVar(&driftClockEvery, "clock-every", 60, "seconds between host/device clock samples")
}

func driftPlan() blockPlan {
	n := int(driftDuration / (time.Duration(driftIntervalMs) * time.Millisecond))
	return blockPlan{
		Name: "drift",
		Steps: []string{
			fmt.Sprintf("%s of %d ms pulses on line %d every %d ms (~%d pulses)",
				driftDuration, driftWidthMs, driftLine, driftIntervalMs, n),
			fmt.Sprintf("host/device clock sample every %d s", driftClockEvery),
		},
		Duration: driftDuration + markerDuration() + 2*time.Second,
	}
}

func runDrift() error {
	if driftIntervalMs <= driftWidthMs {
		return fmt.Errorf("--interval (%d ms) must exceed --width (%d ms)",
			driftIntervalMs, driftWidthMs)
	}
	if preflight(driftPlan()) {
		return nil
	}

	box, _, err := openBox(ttlbox.CapTimestamps)
	if err != nil {
		return err
	}
	defer box.Close()

	rec, err := newRecorder("drift",
		"trial", "host_write_us", "host_recv_us", "dev_rise_us", "n_events", "overflow")
	if err != nil {
		return err
	}
	defer rec.Close()

	origin := time.Now()
	clk, err := newClockTracker(box, "drift", origin)
	if err != nil {
		return err
	}
	defer clk.Close()
	if err := clk.Sample(20); err != nil {
		return err
	}

	if err := marker(box, driftLine); err != nil {
		return err
	}
	if err := box.SetTriggerDuration(time.Duration(driftWidthMs) * time.Millisecond); err != nil {
		return err
	}

	interval := time.Duration(driftIntervalMs) * time.Millisecond
	end := time.Now().Add(driftDuration)
	nextClock := time.Now().Add(time.Duration(driftClockEvery) * time.Second)
	nextReport := time.Now().Add(5 * time.Minute)

	fmt.Printf("  running for %s — a pulse every %d ms\n", driftDuration, driftIntervalMs)
	for i := 0; time.Now().Before(end); i++ {
		next := time.Now().Add(interval)
		if err := box.ClearEvents(); err != nil {
			return err
		}
		hostWrite := time.Now()
		if err := box.SendTriggerMask(1 << driftLine); err != nil {
			return err
		}
		evs, overflow, err := collectEvents(box, 1, 0, 500*time.Millisecond)
		if err != nil {
			return err
		}
		var rise ttlbox.DeviceTime
		var recv time.Duration
		if len(evs) > 0 {
			rise = clk.Unwrap(evs[0].Event.Micros)
			recv = evs[0].Host.Sub(origin)
		}
		rec.Row(i, hostWrite.Sub(origin), recv, rise, len(evs), overflow)

		if time.Now().After(nextClock) {
			if err := clk.Sample(20); err != nil {
				return err
			}
			nextClock = time.Now().Add(time.Duration(driftClockEvery) * time.Second)
		}
		if time.Now().After(nextReport) {
			fmt.Printf("  %s elapsed, %d pulses\n", time.Since(origin).Round(time.Second), i+1)
			nextReport = time.Now().Add(5 * time.Minute)
		}
		sleepUntil(next)
	}
	if err := clk.Sample(20); err != nil {
		return err
	}

	fmt.Printf("  wrote %s\n", rec.Path)
	return nil
}
