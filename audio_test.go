package main

import "testing"

func TestSoundIndexForKey(t *testing.T) {
	// Determinism: same key always maps to same index.
	for i := 0; i < 100; i++ {
		if got := soundIndexForKey("a", 3); got != soundIndexForKey("a", 3) {
			t.Fatalf("non-deterministic: got different results for same key")
		}
	}

	// Range: result is always in [0, numSounds).
	keys := []string{"a", "b", "Enter", "Space", "Ctrl+c", "Shift+Tab", "x", "1", "2", "3"}
	for _, k := range keys {
		idx := soundIndexForKey(k, 3)
		if idx < 0 || idx >= 3 {
			t.Errorf("soundIndexForKey(%q, 3) = %d, want [0,3)", k, idx)
		}
	}

	// Different keys should produce at least two distinct indices with 3 sounds.
	seen := map[int]bool{}
	for _, k := range keys {
		seen[soundIndexForKey(k, 3)] = true
	}
	if len(seen) < 2 {
		t.Errorf("expected at least 2 distinct indices from %d keys, got %d", len(keys), len(seen))
	}

	// Single sound: always returns 0.
	for _, k := range keys {
		if got := soundIndexForKey(k, 1); got != 0 {
			t.Errorf("soundIndexForKey(%q, 1) = %d, want 0", k, got)
		}
	}

	// Case insensitive: uppercase and lowercase map to the same index.
	casePairs := [][2]string{{"a", "A"}, {"h", "H"}, {"z", "Z"}, {"Enter", "enter"}}
	for _, pair := range casePairs {
		lo := soundIndexForKey(pair[0], 3)
		hi := soundIndexForKey(pair[1], 3)
		if lo != hi {
			t.Errorf("soundIndexForKey(%q, 3) = %d, soundIndexForKey(%q, 3) = %d; want same index", pair[0], lo, pair[1], hi)
		}
	}
}
