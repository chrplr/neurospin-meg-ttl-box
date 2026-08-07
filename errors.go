// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Sonnet 4.6
// Licensed under the Apache License, Version 2.0 (see LICENSE.txt).

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

	// ErrLegacyFirmware is returned by Box.GetInfo when the device does not
	// answer the get_info opcode. Firmware older than protocol version 1
	// ignores unknown opcodes silently, so silence — not an error reply — is
	// the signature of an old device.
	ErrLegacyFirmware = errors.New("ttlbox: device does not implement get_info (firmware older than protocol v1)")

	// ErrBadReply is returned when the device answers with the right number of
	// bytes but the wrong content, such as a get_info reply not starting with
	// the "MTB" magic. It usually means the host and device have fallen out of
	// step in the byte stream.
	ErrBadReply = errors.New("ttlbox: malformed reply")

	// ErrNotSupported is returned when a method needs a firmware capability the
	// device does not advertise. Check with Box.GetInfo and Info.Has.
	ErrNotSupported = errors.New("ttlbox: capability not supported by this firmware")

	// ErrBadDebounce is returned when a debounce interval outside
	// [0, 65535 µs] is given.
	ErrBadDebounce = errors.New("ttlbox: debounce out of range (0–65535 µs)")
)
