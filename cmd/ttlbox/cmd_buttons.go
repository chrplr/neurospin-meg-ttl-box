// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Sonnet 4.6
// Distributed under the GNU General Public License v3.

package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	ttlbox "github.com/neurospin/neurospin-meg-ttl-box"
	"github.com/spf13/cobra"
)

var buttonsCmd = &cobra.Command{
	Use:   "buttons",
	Short: "Read or wait for FORP response-box button presses",
}

var buttonsReadCmd = &cobra.Command{
	Use:   "read",
	Short: "Read current button state once",
	RunE: func(cmd *cobra.Command, args []string) error {
		box, err := openBox()
		if err != nil {
			return err
		}
		defer box.Close()
		mask, err := box.ReadButtonMask(context.Background())
		if err != nil {
			return err
		}
		printButtons(mask, 0)
		return nil
	},
}

var buttonsWaitTimeoutMs int

var buttonsWaitCmd = &cobra.Command{
	Use:   "wait",
	Short: "Block until a button is pressed; print mask and reaction time",
	RunE: func(cmd *cobra.Command, args []string) error {
		box, err := openBox()
		if err != nil {
			return err
		}
		defer box.Close()

		ctx := context.Background()
		if buttonsWaitTimeoutMs > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, time.Duration(buttonsWaitTimeoutMs)*time.Millisecond)
			defer cancel()
		}

		if err := box.DrainButtons(ctx); err != nil {
			return fmt.Errorf("drain: %w", err)
		}
		mask, rt, err := box.WaitForButton(ctx)
		if err != nil {
			return err
		}
		printButtons(mask, rt)
		return nil
	},
}

func printButtons(mask uint8, rt time.Duration) {
	buttons := ttlbox.DecodeMask(mask)
	names := make([]string, len(buttons))
	for i, b := range buttons {
		names[i] = b.String()
	}
	label := "none"
	if len(names) > 0 {
		label = strings.Join(names, ", ")
	}
	fmt.Printf("mask:    0x%02X (%08b)\n", mask, mask)
	fmt.Printf("buttons: %s\n", label)
	if rt > 0 {
		fmt.Printf("RT:      %s\n", rt.Round(time.Millisecond))
	}
}

func init() {
	buttonsWaitCmd.Flags().IntVar(&buttonsWaitTimeoutMs, "timeout", 0,
		"max wait time in ms (0 = wait indefinitely)")
	buttonsCmd.AddCommand(buttonsReadCmd, buttonsWaitCmd)
}
