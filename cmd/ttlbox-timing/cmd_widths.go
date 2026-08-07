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
	widthsList   []int
	widthsPulses int
	widthsISIMs  int
	widthsLine   uint8
)

var widthsCmd = &cobra.Command{
	Use:   "widths",
	Short: "Realised TTL pulse width against the requested width (block B1)",
	Long: `Emit blocks of pulses at a range of requested widths and record what the
hardware actually produced.

The firmware times a pulse itself, from millis(), so the width does not absorb
host scheduling jitter — but millis() truncates, which predicts a realised width
uniform on [w-1, w] and therefore a bias of about -0.5 ms at EVERY width. A
previous session confirmed that at 5, 10 and 20 ms; this block extends it below
and above that range, where the model has never been checked. The 1 ms request
is the interesting one: truncation predicts a mean of 0.5 ms and a floor at 0.

Two witnesses record each pulse. A BBTKv3 capture sees it from outside at
0.25 ms; the box's own D30->D22 loopback timestamps both edges with micros(), at
a resolution the instrument cannot reach. Neither can be blamed for a
disagreement without the other.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWidths()
	},
}

func init() {
	f := widthsCmd.Flags()
	f.IntSliceVar(&widthsList, "widths", []int{1, 2, 3, 5, 10, 20, 50, 100, 200},
		"requested pulse widths in ms")
	f.IntVar(&widthsPulses, "pulses", 50, "pulses per width")
	f.IntVar(&widthsISIMs, "isi", 500, "ms between pulse onsets")
	f.Uint8Var(&widthsLine, "line", 0, "output line to pulse (0-7)")
}

func widthsPlan() blockPlan {
	var d time.Duration
	steps := []string{}
	for _, w := range widthsList {
		bd := time.Duration(widthsPulses) * time.Duration(max(widthsISIMs, w+50)) * time.Millisecond
		d += bd
		steps = append(steps, fmt.Sprintf("%d pulses of %d ms on line %d (%s)",
			widthsPulses, w, widthsLine, bd.Round(time.Second)))
	}
	return blockPlan{
		Name:     "widths",
		Steps:    steps,
		Duration: d + markerDuration() + 2*time.Second,
	}
}

func runWidths() error {
	plan := widthsPlan()
	if preflight(plan) {
		return nil
	}

	box, _, err := openBox(ttlbox.CapTimestamps)
	if err != nil {
		return err
	}
	defer box.Close()

	rec, err := newRecorder("widths",
		"requested_ms", "trial", "host_write_us",
		"dev_rise_us", "dev_fall_us", "dev_width_us", "n_events", "overflow")
	if err != nil {
		return err
	}
	defer rec.Close()

	origin := time.Now()
	clk, err := newClockTracker(box, "widths", origin)
	if err != nil {
		return err
	}
	defer clk.Close()
	if err := clk.Sample(20); err != nil {
		return err
	}

	if err := marker(box, widthsLine); err != nil {
		return err
	}

	for _, w := range widthsList {
		fmt.Printf("  %d pulses of %d ms on line %d\n", widthsPulses, w, widthsLine)
		if err := box.SetTriggerDuration(time.Duration(w) * time.Millisecond); err != nil {
			return err
		}
		// The inter-onset interval must outlast the pulse itself, or the next
		// trial starts while the line is still high and the two merge into one
		// event pair with no gap.
		isi := time.Duration(max(widthsISIMs, w+50)) * time.Millisecond

		for trial := range widthsPulses {
			next := time.Now().Add(isi)
			if err := box.ClearEvents(); err != nil {
				return err
			}
			hostWrite := time.Now()
			if err := box.SendTriggerMask(1 << widthsLine); err != nil {
				return err
			}

			// Both edges of the pulse: the loopback turns one pulse into a
			// rising and a falling transition on the paired input line.
			evs, overflow, err := collectEvents(box, 2, 0,
				time.Duration(w)*time.Millisecond+500*time.Millisecond)
			if err != nil {
				return err
			}

			var rise, fall ttlbox.DeviceTime
			if len(evs) > 0 {
				rise = clk.Unwrap(evs[0].Event.Micros)
			}
			if len(evs) > 1 {
				fall = clk.Unwrap(evs[1].Event.Micros)
			}
			width := time.Duration(0)
			if len(evs) > 1 {
				width = time.Duration(fall-rise) * time.Microsecond
			}
			rec.Row(w, trial, hostWrite.Sub(origin), rise, fall, width, len(evs), overflow)

			sleepUntil(next)
		}
		if err := clk.Sample(20); err != nil {
			return err
		}
	}

	fmt.Printf("  wrote %s\n", rec.Path)
	return nil
}
