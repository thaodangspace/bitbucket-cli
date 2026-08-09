//go:build !linux && !darwin

package auth

import "testing"

// TestDefaultStoreOnOtherPlatform ensures unsupported platforms get a store
// that declines every operation rather than a macOS/Linux backend.
func TestDefaultStoreOnOtherPlatform(t *testing.T) {
	if _, ok := DefaultStore().(*UnsupportedStore); !ok {
		t.Fatalf("DefaultStore() = %T, want *UnsupportedStore", DefaultStore())
	}
}
