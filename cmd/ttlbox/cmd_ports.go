// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Sonnet 4.6
// Distributed under the GNU General Public License v3.

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"go.bug.st/serial"
)

var portsCmd = &cobra.Command{
	Use:   "ports",
	Short: "List available serial ports",
	RunE: func(cmd *cobra.Command, args []string) error {
		ports, err := serial.GetPortsList()
		if err != nil {
			return fmt.Errorf("list ports: %w", err)
		}
		if len(ports) == 0 {
			fmt.Fprintln(os.Stderr, "no serial ports found")
			return nil
		}
		for _, p := range ports {
			fmt.Println(p)
		}
		return nil
	},
}
