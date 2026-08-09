//go:build darwin

package auth

import "testing"

// TestDefaultStoreOnDarwin keeps the macOS Keychain backend wired in.
func TestDefaultStoreOnDarwin(t *testing.T) {
	if _, ok := DefaultStore().(*MacOSKeychain); !ok {
		t.Fatalf("DefaultStore() = %T on darwin, want *MacOSKeychain", DefaultStore())
	}
}
