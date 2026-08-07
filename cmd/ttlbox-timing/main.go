// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Opus 5.
// Licensed under the Apache License, Version 2.0 (see LICENSE.txt).

// Command ttlbox-timing measures the timing behaviour of the MEG TTL box.
//
// Each subcommand is one measurement block. Blocks fall into three groups by
// what they need:
//
//   - Session 1 (widths, codechange, latency, drift) — the box emits and a
//     BBTKv3 in Digital Stimulus Capture mode observes. Run them inside a
//     `bbtk-capture -d <s> <base> -- ttlbox-timing <block>` window; the capture
//     starts the block at the instant recording begins. Every block also
//     records the box's own view through the D30→D22 / D31→D23 loopbacks, at
//     microsecond resolution rather than the BBTK's 0.25 ms, so each figure has
//     two independent witnesses.
//
//   - Session 2 (respond) — the BBTK runs a Digital Stimulus Response Echo
//     program (bbtk-trigger-response) and answers the box's trigger with a
//     TTL pulse after a programmed delay. The box is the timestamper.
//
//   - Session 3 (latency, splitwrite, overflow) — no instrument, just the
//     loopback jumpers. Runnable on any host at any time.
//
// Every block writes a CSV of raw per-trial rows and summarises nothing that
// cannot be recomputed from them; the analysis lives in measurements/.
//
// Wiring for the full battery (see measurements/README.md):
//
//	box D30 (out line 0) -> BBTK TTLin2  and  box D22 (in line 0)
//	box D31 (out line 1) -> BBTK TTLin1  and  box D23 (in line 1)
//	BBTK TTLout1         -> box D24 (in line 2)
//	GND                  -> GND   (required)
package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	ttlbox "github.com/neurospin/neurospin-meg-ttl-box"
	"github.com/spf13/cobra"
)

var (
	flagPort         string
	flagResetDelayMs int
	flagOutDir       string
	flagTag          string
	flagDryRun       bool
	flagSeconds      bool
	flagNoMarker     bool
)

var rootCmd = &cobra.Command{
	Use:   "ttlbox-timing",
	Short: "Measure the timing behaviour of the NeuroSpin MEG TTL box",
	Long: `ttlbox-timing runs the measurement blocks that characterise how long the
box takes to write a TTL line and to report one it receives.

Each subcommand writes a CSV of raw per-trial rows to --out. Use --dry-run to
print a block's plan without touching the hardware, and --seconds to print the
block's duration in whole seconds, which is what sizes a BBTK capture window.`,
	SilenceUsage: true,
}

func main() {
	pf := rootCmd.PersistentFlags()
	pf.StringVarP(&flagPort, "port", "p", "/dev/ttyACM0",
		"serial port of the TTL box")
	pf.IntVar(&flagResetDelayMs, "reset-delay", 2000,
		"ms to wait after opening the port for the Arduino to reset")
	pf.StringVarP(&flagOutDir, "out", "o", ".",
		"directory for the CSV output")
	pf.StringVar(&flagTag, "tag", "",
		"suffix for the CSV filename, to keep repeated runs apart (e.g. -rt100)")
	pf.BoolVarP(&flagDryRun, "dry-run", "n", false,
		"print the plan and exit without opening the device")
	pf.BoolVar(&flagSeconds, "seconds", false,
		"print the block duration in whole seconds and exit (sizes a BBTK capture)")
	pf.BoolVar(&flagNoMarker, "no-marker", false,
		"skip the 100 ms alignment marker that precedes a block")

	rootCmd.AddCommand(widthsCmd, codechangeCmd, latencyCmd, driftCmd,
		respondCmd, splitwriteCmd, overflowCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// blockPlan is what a block intends to do: enough to review the run before
// committing hardware time to it, and a duration to size a capture window with.
type blockPlan struct {
	Name     string
	Steps    []string
	Duration time.Duration
}

// preflight handles --seconds and --dry-run. It reports whether the caller
// should stop without touching the device.
//
// The duration is deliberately computed from the block's own parameters rather
// than passed in by the operator: a BBTK capture window is fixed before
// recording starts and an interrupted capture is unrecoverable, so the sequence
// and the window must not be able to drift apart.
func preflight(p blockPlan) bool {
	if flagSeconds {
		fmt.Println(int(p.Duration.Round(time.Second).Seconds()))
		return true
	}
	fmt.Printf("block %s — about %s\n", p.Name, p.Duration.Round(time.Second))
	for _, s := range p.Steps {
		fmt.Printf("  %s\n", s)
	}
	if flagDryRun {
		fmt.Println("\n(dry run — nothing recorded)")
		return true
	}
	fmt.Println()
	return false
}

// openBox opens the device and reports its firmware, refusing to continue on
// firmware that lacks a capability the block needs. Every block here depends on
// timestamped input events, and most on atomic port writes.
func openBox(needed uint8) (*ttlbox.Box, ttlbox.Info, error) {
	box, err := ttlbox.Open(flagPort,
		ttlbox.WithResetDelay(time.Duration(flagResetDelayMs)*time.Millisecond))
	if err != nil {
		return nil, ttlbox.Info{}, err
	}
	info, err := box.GetInfo()
	if err != nil {
		box.Close()
		if errors.Is(err, ttlbox.ErrLegacyFirmware) {
			return nil, ttlbox.Info{}, fmt.Errorf(
				"%w — these measurements need protocol v1; reflash arduino/meg_protocol", err)
		}
		return nil, ttlbox.Info{}, err
	}
	fmt.Printf("TTL box on %s: %s\n", flagPort, info)
	if !info.Has(needed) {
		box.Close()
		return nil, info, fmt.Errorf("%w: block needs caps 0x%02X, device reports 0x%02X",
			ttlbox.ErrNotSupported, needed, info.Caps)
	}
	return box, info, nil
}

// marker emits a 100 ms pulse on line 0 followed by a 2 s gap.
//
// It separates a block from anything the BBTK captured beforehand — a capture
// window always starts early — and it is far longer than any pulse a block
// emits, so the analysis can find it without knowing the block's parameters.
func marker(box *ttlbox.Box, line uint8) error {
	if flagNoMarker {
		return nil
	}
	fmt.Println("  marker: 100 ms pulse on line", line)
	if err := box.SetTriggerDuration(100 * time.Millisecond); err != nil {
		return err
	}
	if err := box.SendTriggerMask(1 << line); err != nil {
		return err
	}
	time.Sleep(2100 * time.Millisecond)
	return nil
}

// markerDuration is what marker costs, for a block's duration estimate.
func markerDuration() time.Duration {
	if flagNoMarker {
		return 0
	}
	return 2200 * time.Millisecond
}
