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
	ccFrom      uint8
	ccTo        uint8
	ccTrials    int
	ccHoldMs    int
	ccISIMs     int
	ccArms      string
	ccLegacyLow bool
)

var codechangeCmd = &cobra.Command{
	Use:   "codechange",
	Short: "Is a trigger-code CHANGE atomic on the wires? (block B2)",
	Long: `Change the output port from one trigger code to another and record whether
any intermediate value reached the wires.

This is the test that matters, and it is not the same as pulsing two lines from
zero together. Going from 0b01 to 0b10 requires one line to rise as another
falls; the atomic path (opcode 17) assigns the whole port in a single AVR
instruction, so no intermediate can exist, while the legacy pair (opcodes 13
then 14) leaves the port at from|to for as long as a USB frame — a valid-looking
but wrong code that an amplifier sampling at 1 kHz can latch.

Both arms are run, and the legacy arm is the point of the block: it is the
positive control. Without it, "no glitch observed" on the atomic path could
equally mean the apparatus cannot see glitches. With it, the same apparatus is
shown to detect exactly the artefact opcode 17 is claimed to prevent — and if
the legacy arm shows no intermediate either, the block has failed and its
conclusion must be withheld rather than reported.

The discriminator needs no instrument. The firmware samples all eight input
lines in one PINA read, so a change on two lines within one sampling instant
arrives as ONE event carrying the new port value, and a change spread over two
instants arrives as TWO. Counting events per transition therefore answers the
question directly, with the gap between the pair measuring how long the wrong
code was on the wires. A BBTKv3 capture witnesses the same thing from outside.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCodechange()
	},
}

func init() {
	f := codechangeCmd.Flags()
	f.Uint8Var(&ccFrom, "from", 0b01, "trigger code to start from")
	f.Uint8Var(&ccTo, "to", 0b10, "trigger code to change to")
	f.IntVar(&ccTrials, "trials", 50, "trials per arm")
	f.IntVar(&ccHoldMs, "hold", 50, "ms to hold each code")
	f.IntVar(&ccISIMs, "isi", 200, "ms between trials")
	f.StringVar(&ccArms, "arms", "both", "which arms to run: both, atomic, legacy")
	f.BoolVar(&ccLegacyLow, "legacy-low-first", false,
		"in the legacy arm clear before setting, exposing from&to instead of from|to")
}

func ccArmList() ([]string, error) {
	switch ccArms {
	case "both":
		return []string{"atomic", "legacy"}, nil
	case "atomic", "legacy":
		return []string{ccArms}, nil
	}
	return nil, fmt.Errorf("unknown --arms %q (want both, atomic or legacy)", ccArms)
}

func codechangePlan(arms []string) blockPlan {
	per := time.Duration(2*ccHoldMs+ccISIMs) * time.Millisecond
	steps := make([]string, 0, len(arms))
	for _, a := range arms {
		steps = append(steps, fmt.Sprintf("%s arm: %d trials 0b%08b -> 0b%08b (%s)",
			a, ccTrials, ccFrom, ccTo, (per*time.Duration(ccTrials)).Round(time.Second)))
	}
	return blockPlan{
		Name:     "codechange",
		Steps:    steps,
		Duration: per*time.Duration(ccTrials*len(arms)) + markerDuration() + 2*time.Second,
	}
}

func runCodechange() error {
	arms, err := ccArmList()
	if err != nil {
		return err
	}
	if ccFrom == ccTo {
		return fmt.Errorf("--from and --to are both 0b%08b; a code change needs two codes", ccFrom)
	}
	if preflight(codechangePlan(arms)) {
		return nil
	}

	// The legacy arm still needs opcode 17 to establish the starting code
	// without a glitch of its own, so both capabilities are required.
	box, _, err := openBox(ttlbox.CapAtomicPort | ttlbox.CapTimestamps)
	if err != nil {
		return err
	}
	defer box.Close()

	rec, err := newRecorder("codechange",
		"arm", "trial", "host_cmd_us", "n_events",
		"dev_t0_us", "dev_t1_us", "glitch_us", "mask0", "mask1")
	if err != nil {
		return err
	}
	defer rec.Close()

	origin := time.Now()
	clk, err := newClockTracker(box, "codechange", origin)
	if err != nil {
		return err
	}
	defer clk.Close()
	if err := clk.Sample(20); err != nil {
		return err
	}

	if err := marker(box, 0); err != nil {
		return err
	}

	hold := time.Duration(ccHoldMs) * time.Millisecond
	isi := time.Duration(ccISIMs) * time.Millisecond
	glitched := map[string]int{}

	for _, arm := range arms {
		fmt.Printf("  %s arm: %d trials 0b%08b -> 0b%08b\n", arm, ccTrials, ccFrom, ccTo)
		for trial := range ccTrials {
			next := time.Now().Add(2*hold + isi)

			if err := box.SetPortMask(ccFrom); err != nil {
				return err
			}
			time.Sleep(hold)
			if err := box.ClearEvents(); err != nil {
				return err
			}

			hostCmd := time.Now()
			if err := applyCodeChange(box, arm); err != nil {
				return err
			}

			// Two events would mean an intermediate value was visible. Wait
			// long enough for a second one to arrive before concluding there
			// was none: a USB frame is ~1 ms, so 50 ms is not a close call.
			evs, _, err := collectEvents(box, 2, 0, 50*time.Millisecond)
			if err != nil {
				return err
			}

			var t0, t1 ttlbox.DeviceTime
			var m0, m1 uint8
			glitch := time.Duration(0)
			if len(evs) > 0 {
				t0, m0 = clk.Unwrap(evs[0].Event.Micros), evs[0].Event.Mask
			}
			if len(evs) > 1 {
				t1, m1 = clk.Unwrap(evs[1].Event.Micros), evs[1].Event.Mask
				glitch = time.Duration(t1-t0) * time.Microsecond
				glitched[arm]++
			}
			rec.Row(arm, trial, hostCmd.Sub(origin), len(evs), t0, t1, glitch, m0, m1)

			time.Sleep(hold)
			if err := box.SetPortMask(0); err != nil {
				return err
			}
			sleepUntil(next)
		}
		if err := clk.Sample(20); err != nil {
			return err
		}
	}

	fmt.Println()
	for _, arm := range arms {
		fmt.Printf("  %s: %d of %d trials showed an intermediate code\n",
			arm, glitched[arm], ccTrials)
	}
	if len(arms) == 2 && glitched["legacy"] == 0 {
		fmt.Println("\n  WARNING: the legacy arm produced no intermediate either.")
		fmt.Println("  The positive control failed, so the atomic arm's result is")
		fmt.Println("  not evidence of anything. Re-run before quoting it.")
	}
	fmt.Printf("\n  wrote %s\n", rec.Path)
	return nil
}

// applyCodeChange performs the transition under test for one arm.
func applyCodeChange(box *ttlbox.Box, arm string) error {
	if arm == "atomic" {
		return box.SetPortMask(ccTo)
	}
	// Legacy: two commands, and whichever goes first decides which wrong code
	// is exposed in between — the union (a trigger with extra bits set) or the
	// intersection (one with bits missing).
	rising, falling := ccTo&^ccFrom, ccFrom&^ccTo
	if ccLegacyLow {
		if err := box.SetLowMask(falling); err != nil {
			return err
		}
		return box.SetHighMask(rising)
	}
	if err := box.SetHighMask(rising); err != nil {
		return err
	}
	return box.SetLowMask(falling)
}
