//go:build linux

package auth

import "testing"

// TestDefaultStoreOnLinux guards the Linux release: the default backend must
// be the Secret Service store, never the macOS `security` command and never an
// unsupported placeholder.
func TestDefaultStoreOnLinux(t *testing.T) {
	if _, ok := DefaultStore().(*LinuxSecretStore); !ok {
		t.Fatalf("DefaultStore() = %T on linux, want *LinuxSecretStore", DefaultStore())
	}
}
