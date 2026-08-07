// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Sonnet 4.6
// Licensed under the Apache License, Version 2.0 (see LICENSE.txt).

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

// chunkedPort returns its receive data in preset chunks, one per Read, to
// reproduce a multi-byte reply split across USB packets. Once the chunks run
// out it reports (0, nil), which is how go.bug.st/serial signals a read
// timeout — so a test can also make a reply arrive incomplete.
type chunkedPort struct {
	chunks [][]byte
	tx     *bytes.Buffer
}

func (m *chunkedPort) Read(p []byte) (int, error) {
	if len(m.chunks) == 0 {
		return 0, nil
	}
	c := m.chunks[0]
	n := copy(p, c)
	if n < len(c) {
		m.chunks[0] = c[n:]
	} else {
		m.chunks = m.chunks[1:]
	}
	return n, nil
}

func (m *chunkedPort) Write(p []byte) (int, error) { return m.tx.Write(p) }
func (m *chunkedPort) Close() error                { return nil }

// newChunkedBox returns a Box whose port hands back one chunk per Read.
func newChunkedBox(chunks ...[]byte) (*Box, *bytes.Buffer) {
	tx := &bytes.Buffer{}
	b := &Box{
		port:         &chunkedPort{chunks: chunks, tx: tx},
		resetDelay:   0,
		pollInterval: 0,
	}
	return b, tx
}
