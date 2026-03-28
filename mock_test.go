// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Sonnet 4.6
// Distributed under the GNU General Public License v3.

package ttlbox

import "bytes"

// mockPort implements the port interface using in-memory buffers.
// rx holds bytes the mock returns on Read (simulating device → host traffic).
// tx captures bytes written by the host (simulating host → device traffic).
type mockPort struct {
	rx *bytes.Buffer
	tx *bytes.Buffer
}

func (m *mockPort) Read(p []byte) (int, error)  { return m.rx.Read(p) }
func (m *mockPort) Write(p []byte) (int, error) { return m.tx.Write(p) }
func (m *mockPort) Close() error                { return nil }

// newMockBox returns a Box wired to a mockPort and the tx buffer for
// inspection. rxData is loaded into the mock's receive buffer (device→host).
// The reset delay and poll interval are set to zero so tests run instantly.
func newMockBox(rxData []byte, opts ...Option) (*Box, *bytes.Buffer) {
	tx := &bytes.Buffer{}
	mock := &mockPort{
		rx: bytes.NewBuffer(rxData),
		tx: tx,
	}
	b := &Box{
		port:         mock,
		resetDelay:   0,
		pollInterval: 0,
	}
	for _, opt := range opts {
		opt(b)
	}
	return b, tx
}
