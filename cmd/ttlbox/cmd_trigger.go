// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Sonnet 4.6
// Distributed under the GNU General Public License v3.

package main

import (
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

var triggerCmd = &cobra.Command{
	Use:   "trigger",
	Short: "Send TTL triggers on output lines",
}

var triggerDurationCmd = &cobra.Command{
	Use:   "duration <ms>",
	Short: "Set the TTL pulse width in ms (opcode 10)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ms, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid duration %q: %w", args[0], err)
		}
		box, err := openBox()
		if err != nil {
			return err
		}
		defer box.Close()
		if err := box.SetTriggerDuration(time.Duration(ms) * time.Millisecond); err != nil {
			return err
		}
		fmt.Printf("trigger duration set to %d ms\n", ms)
		return nil
	},
}

var triggerMaskCmd = &cobra.Command{
	Use:   "mask <0-255>",
	Short: "Pulse all lines whose bit is set in mask (opcode 11)",
	Long: `Pulse all output lines whose corresponding bit is set in mask.
Mask can be decimal (e.g. 15), hex (0x0F), or binary (0b00001111).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		v, err := strconv.ParseUint(args[0], 0, 8)
		if err != nil {
			return fmt.Errorf("invalid mask %q (use decimal, 0x hex, or 0b binary): %w", args[0], err)
		}
		box, err := openBox()
		if err != nil {
			return err
		}
		defer box.Close()
		if err := box.SendTriggerMask(uint8(v)); err != nil {
			return err
		}
		fmt.Printf("trigger sent on mask 0x%02X (%08b)\n", v, v)
		return nil
	},
}

var triggerLineCmd = &cobra.Command{
	Use:   "line <0-7>",
	Short: "Pulse a single output line (opcode 12)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		v, err := strconv.ParseUint(args[0], 10, 8)
		if err != nil || v > 7 {
			return fmt.Errorf("line must be 0–7, got %q", args[0])
		}
		box, err := openBox()
		if err != nil {
			return err
		}
		defer box.Close()
		if err := box.SendTriggerOnLine(uint8(v)); err != nil {
			return err
		}
		fmt.Printf("trigger sent on line %d\n", v)
		return nil
	},
}

func init() {
	triggerCmd.AddCommand(triggerDurationCmd, triggerMaskCmd, triggerLineCmd)
}
