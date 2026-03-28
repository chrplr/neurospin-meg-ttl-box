// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Sonnet 4.6
// Distributed under the GNU General Public License v3.

package ttlbox

import (
	"errors"
	"testing"
)

func TestEnsureOpenOnZeroBox(t *testing.T) {
	var b Box
	if err := b.ensureOpen(); !errors.Is(err, ErrNotOpen) {
		t.Fatalf("expected ErrNotOpen, got %v", err)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	b, _ := newMockBox(nil)
	if err := b.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("second Close should be a no-op, got %v", err)
	}
}

func TestCloseOnZeroBox(t *testing.T) {
	var b Box
	if err := b.Close(); err != nil {
		t.Fatalf("Close on zero Box should be a no-op, got %v", err)
	}
}

func TestTxFailsWhenNotOpen(t *testing.T) {
	var b Box
	if err := b.tx([]byte{1}); !errors.Is(err, ErrNotOpen) {
		t.Fatalf("expected ErrNotOpen, got %v", err)
	}
}
