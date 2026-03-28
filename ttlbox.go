// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Sonnet 4.6
// Distributed under the GNU General Public License v3.

// Package ttlbox provides a Go client for the Arduino-based TTL trigger and
// response-button interface used in MEG (magnetoencephalography) experiments
// at NeuroSpin.
//
// The Arduino firmware (see arduino/meg_protocol/meg_protocol.ino) exposes
// eight TTL output lines (trigger generation) and eight TTL input lines
// (FORP response-box buttons) over a USB serial connection using a compact
// binary protocol.
//
// Basic usage:
//
//	box, err := ttlbox.Open("/dev/ttyACM0")
//	if err != nil { log.Fatal(err) }
//	defer box.Close()
//
//	box.SetTriggerDuration(5 * time.Millisecond)
//	box.SendTriggerOnLine(0)
//
//	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
//	defer cancel()
//	box.DrainButtons(ctx)
//	mask, rt, err := box.WaitForButton(ctx)
//	fmt.Println(ttlbox.DecodeMask(mask), rt)
package ttlbox
