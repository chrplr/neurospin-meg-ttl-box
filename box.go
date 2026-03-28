// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Sonnet 4.6
// Distributed under the GNU General Public License v3.

package ttlbox

import (
	"fmt"
	"time"

	"go.bug.st/serial"
)

const (
	defaultBaud         = 115200
	defaultResetDelay   = 2 * time.Second
	defaultPollInterval = 5 * time.Millisecond
	defaultReadTimeout  = 200 * time.Millisecond
)

// port is the minimal interface of go.bug.st/serial.Port used by Box.
// Keeping it narrow allows substitution with a mock in tests.
type port interface {
	Read(p []byte) (n int, err error)
	Write(p []byte) (n int, err error)
	Close() error
}

// Box represents an open connection to the Arduino TTL/response box.
// Construct with [Open]. Close must be called when done.
// Box is not safe for concurrent use without external synchronization.
type Box struct {
	port         port
	resetDelay   time.Duration
	pollInterval time.Duration
}

// Option configures a Box at construction time.
type Option func(*Box)

// WithResetDelay sets how long [Open] waits after opening the port for the
// Arduino to complete its DTR-triggered hardware reset (default: 2 s).
// Pass 0 to skip the delay if the device is already running.
func WithResetDelay(d time.Duration) Option {
	return func(b *Box) { b.resetDelay = d }
}

// WithPollInterval sets the sleep duration between successive button polls
// in [Box.WaitForButton] and [Box.DrainButtons] (default: 5 ms).
func WithPollInterval(d time.Duration) Option {
	return func(b *Box) { b.pollInterval = d }
}

// Open opens the serial port at portPath, applies opts, waits for the Arduino
// to boot, and returns a ready [Box]. The caller must call [Box.Close] when done.
//
// portPath is a system serial port path such as "/dev/ttyACM0" (Linux/macOS)
// or "COM3" (Windows).
func Open(portPath string, opts ...Option) (*Box, error) {
	mode := &serial.Mode{
		BaudRate: defaultBaud,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}
	p, err := serial.Open(portPath, mode)
	if err != nil {
		return nil, fmt.Errorf("ttlbox: open %s: %w", portPath, err)
	}
	if err := p.SetReadTimeout(defaultReadTimeout); err != nil {
		p.Close()
		return nil, fmt.Errorf("ttlbox: set read timeout: %w", err)
	}

	b := &Box{
		port:         p,
		resetDelay:   defaultResetDelay,
		pollInterval: defaultPollInterval,
	}
	for _, opt := range opts {
		opt(b)
	}

	// Opening a USB-CDC serial port asserts DTR, triggering a hardware reset
	// on most Arduino boards. Wait for the firmware to reinitialise.
	time.Sleep(b.resetDelay)
	return b, nil
}

// Close drives all output lines LOW and then closes the serial port.
// It is safe to call Close on an already-closed Box (no-op).
func (b *Box) Close() error {
	if b.port == nil {
		return nil
	}
	_ = b.AllLow() // best-effort; leave lines safe even if close fails
	err := b.port.Close()
	b.port = nil
	return err
}

func (b *Box) ensureOpen() error {
	if b.port == nil {
		return ErrNotOpen
	}
	return nil
}

func (b *Box) tx(data []byte) error {
	if err := b.ensureOpen(); err != nil {
		return err
	}
	_, err := b.port.Write(data)
	return err
}

func (b *Box) rxExact(n int) ([]byte, error) {
	if err := b.ensureOpen(); err != nil {
		return nil, err
	}
	buf := make([]byte, n)
	got, err := b.port.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("ttlbox: read: %w", err)
	}
	if got != n {
		return nil, fmt.Errorf("%w: expected %d bytes, got %d", ErrTimeout, n, got)
	}
	return buf[:got], nil
}
