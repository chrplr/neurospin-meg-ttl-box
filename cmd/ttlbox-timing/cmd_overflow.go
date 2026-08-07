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
	ovDuration   time.Duration
	ovRateHz     int
	ovWidthMs    int
	ovLine       uint8
	ovStallMs    int
	ovStallEvery time.Duration
)

var overflowCmd = &cobra.Command{
	Use:   "overflow",
	Short: "Sustained load and the event-queue overflow flag (block B11)",
	Long: `Drive the box hard for a long time, stop draining its event queue at
intervals, and check that what it reports matches what it did.

Two things are being established. First, that nothing degrades over a session's
worth of triggers — the existing documentation lists behaviour under sustained
load as unmeasured, so a long run at a high rate is the only way to say
otherwise. Second, that the 32-slot event queue behaves as documented when it is
deliberately overrun: the firmware drops the NEWEST event and raises a sticky
overflow flag, which the host clears by reading it.

That distinction matters more than it sounds. An overflow means transitions were
lost, not delayed, so an affected trial is not merely late — it is wrong, and
must be discarded rather than corrected. A device that silently dropped events
would leave no way to know which trials those were.

Each stall window withholds polling for long enough to overrun the queue on
purpose; the run then checks that the flag came back and counts how many events
went missing against how many pulses were emitted.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runOverflow()
	},
}

func init() {
	f := overflowCmd.Flags()
	f.DurationVar(&ovDuration, "duration", time.Hour, "total run time")
	f.IntVar(&ovRateHz, "rate", 20, "pulses per second")
	f.IntVar(&ovWidthMs, "width", 5, "pulse width in ms")
	f.Uint8Var(&ovLine, "line", 0, "output line to pulse (0-7)")
	f.IntVar(&ovStallMs, "stall", 3000, "ms to stop draining the queue in each stall window")
	f.DurationVar(&ovStallEvery, "stall-every", 5*time.Minute, "how often to stall (0 = never)")
}

func overflowPlan() blockPlan {
	steps := []string{
		fmt.Sprintf("%s at %d pulses/s of %d ms on line %d", ovDuration, ovRateHz, ovWidthMs, ovLine),
	}
	if ovStallEvery > 0 {
		queueFills := 32 * 1000 / (2 * max(ovRateHz, 1)) // 2 events per pulse
		steps = append(steps, fmt.Sprintf(
			"stop draining for %d ms every %s (the 32-slot queue fills in ~%d ms)",
			ovStallMs, ovStallEvery, queueFills))
	}
	return blockPlan{Name: "overflow", Steps: steps, Duration: ovDuration + 2*time.Second}
}

func runOverflow() error {
	if ovRateHz < 1 {
		return fmt.Errorf("--rate must be at least 1, got %d", ovRateHz)
	}
	interval := time.Second / time.Duration(ovRateHz)
	if interval <= time.Duration(ovWidthMs)*time.Millisecond {
		return fmt.Errorf("--rate %d Hz leaves %s per pulse, which is not longer than --width %d ms",
			ovRateHz, interval, ovWidthMs)
	}
	if preflight(overflowPlan()) {
		return nil
	}

	box, _, err := openBox(ttlbox.CapTimestamps)
	if err != nil {
		return err
	}
	defer box.Close()

	rec, err := newRecorder("overflow",
		"window", "phase", "host_start_us", "pulses", "events", "overflow")
	if err != nil {
		return err
	}
	defer rec.Close()

	origin := time.Now()
	if err := box.SetTriggerDuration(time.Duration(ovWidthMs) * time.Millisecond); err != nil {
		return err
	}
	if err := box.ClearEvents(); err != nil {
		return err
	}

	end := origin.Add(ovDuration)
	nextStall := origin.Add(ovStallEvery)
	nextReport := origin.Add(5 * time.Minute)

	// A window is one reporting unit: the pulses emitted and events seen
	// between two flushes of the counters.
	window, pulses, events := 0, 0, 0
	overflow := false
	phase := "drain"
	windowStart := origin
	stallUntil := time.Time{}
	totalPulses, totalEvents, overflowWindows := 0, 0, 0

	flush := func() {
		rec.Row(window, phase, windowStart.Sub(origin), pulses, events, overflow)
		totalPulses += pulses
		totalEvents += events
		if overflow {
			overflowWindows++
		}
		window++
		pulses, events, overflow = 0, 0, false
		windowStart = time.Now()
	}

	fmt.Printf("  %s at %d pulses/s\n", ovDuration, ovRateHz)
	for time.Now().Before(end) {
		next := time.Now().Add(interval)

		if err := box.SendTriggerMask(1 << ovLine); err != nil {
			return err
		}
		pulses++

		stalling := !stallUntil.IsZero() && time.Now().Before(stallUntil)
		if !stalling {
			if !stallUntil.IsZero() {
				// The stall just ended: whatever survived in the queue, plus
				// the flag, is the evidence this window exists to collect.
				stallUntil = time.Time{}
			}
			// Drain without blocking the schedule; the cap is generous enough
			// to empty a full queue in one pass.
			n, ov, err := box.DrainEvents(64)
			if err != nil {
				return err
			}
			events += n
			overflow = overflow || ov
		}

		if ovStallEvery > 0 && stallUntil.IsZero() && time.Now().After(nextStall) {
			flush()
			phase = "stall"
			stallUntil = time.Now().Add(time.Duration(ovStallMs) * time.Millisecond)
			nextStall = time.Now().Add(ovStallEvery)
		} else if phase == "stall" && stallUntil.IsZero() {
			flush()
			phase = "drain"
		}

		if time.Now().After(nextReport) {
			fmt.Printf("  %s elapsed, %d pulses, %d events, %d windows with overflow\n",
				time.Since(origin).Round(time.Second), totalPulses+pulses,
				totalEvents+events, overflowWindows)
			nextReport = time.Now().Add(5 * time.Minute)
		}
		sleepUntil(next)
	}
	flush()

	fmt.Println()
	fmt.Printf("  %d pulses emitted, %d events received (2 per pulse would be %d)\n",
		totalPulses, totalEvents, 2*totalPulses)
	fmt.Printf("  %d of %d windows reported a queue overflow\n", overflowWindows, window)
	if ovStallEvery > 0 && overflowWindows == 0 {
		fmt.Println("\n  No overflow was ever reported despite the deliberate stalls.")
		fmt.Println("  Either the stalls were too short to fill 32 slots at this rate, or")
		fmt.Println("  the flag is not reaching the host — check before reading this as")
		fmt.Println("  evidence that the queue never overflows in practice.")
	}
	fmt.Printf("  wrote %s\n", rec.Path)
	return nil
}
