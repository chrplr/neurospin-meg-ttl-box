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
	swTrials       int
	swWidthMs      int
	swStallStartMs int
	swStallMs      int
	swLine         uint8
	swISIMs        int
)

var splitwriteCmd = &cobra.Command{
	Use:   "splitwrite",
	Short: "What a command split across two writes does to the firmware (block B8)",
	Long: `Send a command's opcode and its argument in two separate writes, and measure
what the firmware does while it waits for the second byte.

The firmware's main loop reads

    if (Serial.available() < 1) return;
    int opcode = readU8Blocking();

so the opcode itself never blocks — but every argument does, in an unbounded
spin:

    while (Serial.available() < 1) { /* wait */ }

Nothing else runs during that spin. Not sampleButtons(), which is what
timestamps inputs, and not the pulse teardown, which is what ends an active TTL
pulse. So an opcode whose argument is one USB frame behind should stretch any
pulse in flight by the length of the gap.

That prediction is testable with no instrument at all. The control arm pulses
normally; the split arm issues the same pulse and then, part way through it,
sends a lone opcode byte and holds its argument back. The box's own loopback
reports the realised width both times. If the split arm's pulses come back
longer by about the stall, the stall is real and the client contract follows:
never split a command across two writes. The typed methods in this package
build the whole command and write it once, so ordinary code is already safe --
this block exists to establish what the rule is protecting against.

The argument used is set_high_mask 0, which is a no-op on arrival: it raises no
line and releases no pulse. Only the waiting has an effect.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSplitwrite()
	},
}

func init() {
	f := splitwriteCmd.Flags()
	f.IntVar(&swTrials, "trials", 50, "trials per arm")
	f.IntVar(&swWidthMs, "width", 5, "requested pulse width in ms")
	f.IntVar(&swStallStartMs, "stall-start", 2, "ms after the pulse command to send the lone opcode")
	f.IntVar(&swStallMs, "stall", 20, "ms to withhold the argument")
	f.Uint8Var(&swLine, "line", 0, "output line to pulse (0-7)")
	f.IntVar(&swISIMs, "isi", 200, "ms between trials")
}

// opSetHighMask is the opcode whose argument is withheld. It is spelled out
// here rather than exported from the package because this block is about wire
// behaviour: the byte on the link is the subject, not the method that sends it.
const opSetHighMask = 13

func splitwritePlan() blockPlan {
	per := time.Duration(max(swISIMs, swStallStartMs+swStallMs+100)) * time.Millisecond
	return blockPlan{
		Name: "splitwrite",
		Steps: []string{
			fmt.Sprintf("control arm: %d pulses of %d ms, command written once",
				swTrials, swWidthMs),
			fmt.Sprintf("split arm: same, plus a lone opcode at +%d ms with its argument held back %d ms",
				swStallStartMs, swStallMs),
			fmt.Sprintf("prediction: split-arm pulses longer by about %d ms",
				max(swStallStartMs+swStallMs-swWidthMs, 0)),
		},
		Duration: per*time.Duration(2*swTrials) + markerDuration() + 2*time.Second,
	}
}

func runSplitwrite() error {
	if preflight(splitwritePlan()) {
		return nil
	}

	box, _, err := openBox(ttlbox.CapTimestamps)
	if err != nil {
		return err
	}
	defer box.Close()

	rec, err := newRecorder("splitwrite",
		"arm", "trial", "requested_ms", "stall_start_ms", "stall_ms",
		"dev_rise_us", "dev_fall_us", "dev_width_us", "n_events")
	if err != nil {
		return err
	}
	defer rec.Close()

	if err := marker(box, swLine); err != nil {
		return err
	}
	if err := box.SetTriggerDuration(time.Duration(swWidthMs) * time.Millisecond); err != nil {
		return err
	}

	var unwrap ttlbox.Unwrapper
	isi := time.Duration(max(swISIMs, swStallStartMs+swStallMs+100)) * time.Millisecond
	widths := map[string][]time.Duration{}

	for _, arm := range []string{"control", "split"} {
		fmt.Printf("  %s arm: %d pulses of %d ms\n", arm, swTrials, swWidthMs)
		for trial := range swTrials {
			next := time.Now().Add(isi)
			if err := box.ClearEvents(); err != nil {
				return err
			}
			if err := box.SendTriggerMask(1 << swLine); err != nil {
				return err
			}

			if arm == "split" {
				time.Sleep(time.Duration(swStallStartMs) * time.Millisecond)
				// One byte on its own: the firmware consumes it as an opcode
				// and then spins waiting for an argument that is not coming
				// yet, doing nothing else at all.
				if err := box.SendRaw([]byte{opSetHighMask}); err != nil {
					return err
				}
				time.Sleep(time.Duration(swStallMs) * time.Millisecond)
				if err := box.SendRaw([]byte{0x00}); err != nil {
					return err
				}
			}

			evs, _, err := collectEvents(box, 2, 0,
				time.Duration(swStallStartMs+swStallMs+swWidthMs)*time.Millisecond+500*time.Millisecond)
			if err != nil {
				return err
			}
			var rise, fall ttlbox.DeviceTime
			width := time.Duration(0)
			if len(evs) > 0 {
				rise = unwrap.Unwrap(evs[0].Event.Micros)
			}
			if len(evs) > 1 {
				fall = unwrap.Unwrap(evs[1].Event.Micros)
				width = time.Duration(fall-rise) * time.Microsecond
				widths[arm] = append(widths[arm], width)
			}
			rec.Row(arm, trial, swWidthMs, swStallStartMs, swStallMs,
				rise, fall, width, len(evs))
			sleepUntil(next)
		}
	}

	control, split := summarise(widths["control"]), summarise(widths["split"])
	fmt.Println()
	fmt.Printf("  control realised width  %v\n", control)
	fmt.Printf("  split realised width    %v\n", split)

	predicted := time.Duration(max(swStallStartMs+swStallMs-swWidthMs, 0)) * time.Millisecond
	if control.N > 0 && split.N > 0 {
		observed := split.P50 - control.P50
		fmt.Printf("\n  median extension %v, predicted %v\n", observed, predicted)
		switch {
		case predicted == 0:
			fmt.Println("  the stall ends before the pulse would have, so no extension was expected;")
			fmt.Println("  raise --stall to test the hypothesis")
		case observed > predicted/2:
			fmt.Println("  the stall is real: a command split across two writes holds the")
			fmt.Println("  firmware's main loop, extending any pulse in flight and delaying")
			fmt.Println("  the timestamp of any input that changes meanwhile.")
		default:
			fmt.Println("  no extension of the predicted size — the hypothesis is not supported")
			fmt.Println("  by this run. Check that the two writes really left the host as")
			fmt.Println("  separate USB frames before concluding the firmware is unaffected.")
		}
	}
	fmt.Printf("  wrote %s\n", rec.Path)
	return nil
}
