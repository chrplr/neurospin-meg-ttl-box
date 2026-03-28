// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Sonnet 4.6
// Distributed under the GNU General Public License v3.

package ttlbox

import "errors"

// Sentinel errors returned by Box methods.
// Use errors.Is to test for these in calling code.
var (
	// ErrNotOpen is returned when a method is called on a Box whose port is
	// not open (i.e. Open was never called, or Close has already been called).
	ErrNotOpen = errors.New("ttlbox: port not open")

	// ErrTimeout is returned when a serial read does not receive the expected
	// number of bytes within the configured read timeout.
	ErrTimeout = errors.New("ttlbox: read timeout")

	// ErrBadLine is returned when a line number outside [0, 7] is given.
	ErrBadLine = errors.New("ttlbox: line out of range (0–7)")

	// ErrBadDuration is returned when a trigger duration outside [0, 65535 ms]
	// is given.
	ErrBadDuration = errors.New("ttlbox: duration out of range (0–65535 ms)")
)
