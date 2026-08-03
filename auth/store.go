package auth

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Service is the keychain service name used for all bitbucket-cli secrets.
const Service = "bitbucket-cli"

// ErrNotFound is returned when no secret is stored for an account.
var ErrNotFound = errors.New("no stored secret")

// SecretStore persists per-account secrets in the OS credential store.
// Accounts are profile keys, e.g. the Atlassian account email.
type SecretStore interface {
	Get(account string) (string, error)
	Set(account, secret string) error
	Delete(account string) error
}

// OSKeychain stores secrets in the macOS Keychain via the `security` command.
// It stores a generic password under Service with each account as the account
// name (acct).
type OSKeychain struct {
	service string
}

// NewOSKeychain returns a keychain store for the given service label.
func NewOSKeychain(service string) *OSKeychain {
	if service == "" {
		service = Service
	}
	return &OSKeychain{service: service}
}

func (k *OSKeychain) Get(account string) (string, error) {
	out, err := exec.Command("security", "find-generic-password",
		"-a", account, "-s", k.service, "-w").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			// security prints an error for missing items; treat as not found.
			return "", ErrNotFound
		}
		return "", fmt.Errorf("read keychain: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (k *OSKeychain) Set(account, secret string) error {
	if err := exec.Command("security", "add-generic-password",
		"-a", account, "-s", k.service, "-w", secret, "-U").Run(); err != nil {
		return fmt.Errorf("save to keychain: %w", err)
	}
	return nil
}

func (k *OSKeychain) Delete(account string) error {
	if err := exec.Command("security", "delete-generic-password",
		"-a", account, "-s", k.service).Run(); err != nil {
		return fmt.Errorf("delete keychain entry: %w", err)
	}
	return nil
}

// MemoryStore is an in-process SecretStore used in tests. It keeps the
// secret in plain memory only; it is never serialized to disk.
type MemoryStore struct {
	secrets map[string]string
}

// NewMemoryStore returns an empty in-memory SecretStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{secrets: map[string]string{}}
}

func (m *MemoryStore) Get(account string) (string, error) {
	s, ok := m.secrets[account]
	if !ok {
		return "", ErrNotFound
	}
	return s, nil
}

func (m *MemoryStore) Set(account, secret string) error {
	m.secrets[account] = secret
	return nil
}

func (m *MemoryStore) Delete(account string) error {
	delete(m.secrets, account)
	return nil
}

// DefaultStore returns the OSKeychain-backed SecretStore. It is overridable in
// tests via SetGlobalStore.
func DefaultStore() SecretStore { return NewOSKeychain(Service) }

// globalStore is swapped by tests; DefaultStore() is used when nil.
var globalStore SecretStore

// SecretStore returns the active SecretStore: the injected test store if set,
// otherwise the OS keychain.
func CurrentStore() SecretStore {
	if globalStore != nil {
		return globalStore
	}
	return DefaultStore()
}

// SetGlobalStore injects a SecretStore for tests (nil restores the default).
func SetGlobalStore(s SecretStore) { globalStore = s }
