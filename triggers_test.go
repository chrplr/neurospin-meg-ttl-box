// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Sonnet 4.6
// Distributed under the GNU General Public License v3.

package ttlbox

import (
	"errors"
	"testing"
	"time"
)

func TestSetTriggerDuration(t *testing.T) {
	tests := []struct {
		d       time.Duration
		want    []byte
		wantErr bool
	}{
		{5 * time.Millisecond, []byte{10, 5, 0}, false},
		{1000 * time.Millisecond, []byte{10, 0xe8, 0x03}, false},
		{65535 * time.Millisecond, []byte{10, 0xff, 0xff}, false},
		{0, []byte{10, 0, 0}, false},
		{-1 * time.Millisecond, nil, true},
		{65536 * time.Millisecond, nil, true},
	}
	for _, tt := range tests {
		b, tx := newMockBox(nil)
		err := b.SetTriggerDuration(tt.d)
		if tt.wantErr {
			if err == nil {
				t.Errorf("SetTriggerDuration(%v): expected error", tt.d)
			}
			if !errors.Is(err, ErrBadDuration) {
				t.Errorf("SetTriggerDuration(%v): want ErrBadDuration, got %v", tt.d, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("SetTriggerDuration(%v): unexpected error: %v", tt.d, err)
			continue
		}
		if got := tx.Bytes(); string(got) != string(tt.want) {
			t.Errorf("SetTriggerDuration(%v): sent %v, want %v", tt.d, got, tt.want)
		}
	}
}

func TestSendTriggerMask(t *testing.T) {
	b, tx := newMockBox(nil)
	if err := b.SendTriggerMask(0b00001111); err != nil {
		t.Fatal(err)
	}
	if got, want := tx.Bytes(), []byte{11, 0b00001111}; string(got) != string(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSendTriggerOnLine(t *testing.T) {
	for line := uint8(0); line <= 7; line++ {
		b, tx := newMockBox(nil)
		if err := b.SendTriggerOnLine(line); err != nil {
			t.Fatalf("line %d: %v", line, err)
		}
		if got, want := tx.Bytes(), []byte{12, line}; string(got) != string(want) {
			t.Errorf("line %d: got %v, want %v", line, got, want)
		}
	}
}

func TestSendTriggerOnLineBadLine(t *testing.T) {
	b, _ := newMockBox(nil)
	err := b.SendTriggerOnLine(8)
	if err == nil {
		t.Fatal("expected error for line 8")
	}
	if !errors.Is(err, ErrBadLine) {
		t.Errorf("want ErrBadLine, got %v", err)
	}
}
