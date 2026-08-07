// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Opus 5.
// Licensed under the Apache License, Version 2.0 (see LICENSE.txt).

package ttlbox

import (
	"bytes"
	"errors"
	"testing"
)

func TestGetInfo(t *testing.T) {
	b, tx := newMockBox([]byte{'M', 'T', 'B', 1, 0x03})

	info, err := b.GetInfo()
	if err != nil {
		t.Fatalf("GetInfo: %v", err)
	}
	if got := tx.Bytes(); !bytes.Equal(got, []byte{opGetInfo}) {
		t.Errorf("sent % X, want % X", got, []byte{opGetInfo})
	}
	if info.Version != 1 || info.Caps != 0x03 {
		t.Errorf("got %+v, want version 1 caps 0x03", info)
	}
	if !info.AtomicPort() || !info.Timestamps() {
		t.Errorf("caps 0x03 should report both capabilities, got %+v", info)
	}
}

func TestGetInfoCapabilityBits(t *testing.T) {
	tests := []struct {
		caps                uint8
		atomicPort, tstamps bool
	}{
		{0x00, false, false},
		{0x01, true, false},
		{0x02, false, true},
		{0x03, true, true},
		{0x07, true, true}, // unknown future bits must not disturb the known ones
	}
	for _, tt := range tests {
		info := Info{Version: 1, Caps: tt.caps}
		if info.AtomicPort() != tt.atomicPort || info.Timestamps() != tt.tstamps {
			t.Errorf("caps 0x%02X: atomic=%t timestamps=%t, want %t/%t",
				tt.caps, info.AtomicPort(), info.Timestamps(), tt.atomicPort, tt.tstamps)
		}
	}
}

// Firmware older than protocol v1 ignores get_info and answers nothing, so the
// read times out. That silence is the only signature of a legacy device and
// must be distinguishable from a device that is not there at all.
func TestGetInfoLegacyFirmwareIsSilent(t *testing.T) {
	b, _ := newMockBox(nil)

	_, err := b.GetInfo()
	if !errors.Is(err, ErrLegacyFirmware) {
		t.Fatalf("got %v, want ErrLegacyFirmware", err)
	}
}

func TestGetInfoBadMagic(t *testing.T) {
	b, _ := newMockBox([]byte{'X', 'Y', 'Z', 1, 0x03})

	_, err := b.GetInfo()
	if !errors.Is(err, ErrBadReply) {
		t.Fatalf("got %v, want ErrBadReply", err)
	}
}

// The 5-byte reply is written one byte at a time by the firmware and may reach
// the host in several packets. Reassembling it is rxExact's job.
func TestGetInfoReplySplitAcrossReads(t *testing.T) {
	b, _ := newChunkedBox([]byte{'M', 'T'}, []byte{'B', 1}, []byte{0x03})

	info, err := b.GetInfo()
	if err != nil {
		t.Fatalf("GetInfo across split reads: %v", err)
	}
	if info.Version != 1 || info.Caps != 0x03 {
		t.Errorf("got %+v, want version 1 caps 0x03", info)
	}
}

// A reply that stops halfway and never resumes is still a timeout.
func TestGetInfoTruncatedReply(t *testing.T) {
	b, _ := newChunkedBox([]byte{'M', 'T', 'B'})

	_, err := b.GetInfo()
	if !errors.Is(err, ErrLegacyFirmware) {
		t.Fatalf("got %v, want ErrLegacyFirmware (from the underlying timeout)", err)
	}
}
