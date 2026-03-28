// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Sonnet 4.6
// Distributed under the GNU General Public License v3.

package main

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

var lineCmd = &cobra.Command{
	Use:   "line",
	Short: "Set output lines HIGH or LOW persistently (not a pulse)",
}

var lineHighCmd = &cobra.Command{Use: "high", Short: "Drive lines HIGH"}
var lineLowCmd = &cobra.Command{Use: "low", Short: "Drive lines LOW"}

var lineHighMaskCmd = &cobra.Command{
	Use:   "mask <0-255>",
	Short: "Drive HIGH all lines set in mask (opcode 13)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		v, err := parseUint8(args[0])
		if err != nil {
			return err
		}
		box, err := openBox()
		if err != nil {
			return err
		}
		defer box.Close()
		return box.SetHighMask(v)
	},
}

var lineHighLineCmd = &cobra.Command{
	Use:   "line <0-7>",
	Short: "Drive a single line HIGH (opcode 15)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		v, err := parseLine(args[0])
		if err != nil {
			return err
		}
		box, err := openBox()
		if err != nil {
			return err
		}
		defer box.Close()
		return box.SetHighOnLine(v)
	},
}

var lineLowMaskCmd = &cobra.Command{
	Use:   "mask <0-255>",
	Short: "Drive LOW all lines set in mask (opcode 14)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		v, err := parseUint8(args[0])
		if err != nil {
			return err
		}
		box, err := openBox()
		if err != nil {
			return err
		}
		defer box.Close()
		return box.SetLowMask(v)
	},
}

var lineLowLineCmd = &cobra.Command{
	Use:   "line <0-7>",
	Short: "Drive a single line LOW (opcode 16)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		v, err := parseLine(args[0])
		if err != nil {
			return err
		}
		box, err := openBox()
		if err != nil {
			return err
		}
		defer box.Close()
		return box.SetLowOnLine(v)
	},
}

func parseUint8(s string) (uint8, error) {
	v, err := strconv.ParseUint(s, 0, 8)
	if err != nil {
		return 0, fmt.Errorf("invalid mask %q (use decimal, 0x hex, or 0b binary): %w", s, err)
	}
	return uint8(v), nil
}

func parseLine(s string) (uint8, error) {
	v, err := strconv.ParseUint(s, 10, 8)
	if err != nil || v > 7 {
		return 0, fmt.Errorf("line must be 0–7, got %q", s)
	}
	return uint8(v), nil
}

func init() {
	lineHighCmd.AddCommand(lineHighMaskCmd, lineHighLineCmd)
	lineLowCmd.AddCommand(lineLowMaskCmd, lineLowLineCmd)
	lineCmd.AddCommand(lineHighCmd, lineLowCmd)
}
