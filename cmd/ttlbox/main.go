// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Sonnet 4.6
// Distributed under the GNU General Public License v3.

package main

import (
	"fmt"
	"os"
	"time"

	ttlbox "github.com/neurospin/neurospin-meg-ttl-box"
	"github.com/spf13/cobra"
)

var (
	flagPort         string
	flagResetDelayMs int
	flagPollMs       int
)

var rootCmd = &cobra.Command{
	Use:   "ttlbox",
	Short: "Command-line interface for the NeuroSpin Arduino TTL/response box",
	Long: `ttlbox lets you send TTL triggers, control output lines, and read
response-box buttons from the NeuroSpin Arduino MEG interface.`,
}

// openBox opens a Box using the current persistent flag values.
func openBox() (*ttlbox.Box, error) {
	return ttlbox.Open(
		flagPort,
		ttlbox.WithResetDelay(time.Duration(flagResetDelayMs)*time.Millisecond),
		ttlbox.WithPollInterval(time.Duration(flagPollMs)*time.Millisecond),
	)
}

func main() {
	rootCmd.PersistentFlags().StringVarP(&flagPort, "port", "p", "/dev/ttyACM0",
		"serial port path (e.g. /dev/ttyACM0 or COM3)")
	rootCmd.PersistentFlags().IntVar(&flagResetDelayMs, "reset-delay", 2000,
		"ms to wait after opening port for Arduino reset")
	rootCmd.PersistentFlags().IntVar(&flagPollMs, "poll", 5,
		"ms between button polls in wait commands")

	rootCmd.AddCommand(portsCmd, triggerCmd, lineCmd, buttonsCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
