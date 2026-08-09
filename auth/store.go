package auth

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Service is the keychain service name used for all bitbucket-cli secrets.
const Service = "bitbucket-cli"

// ErrNotFound is returned when no secret is stored for an account.
var ErrNotFound = errors.New("no stored secret")

// ErrStoreUnavailable is returned when the credential-store backend cannot
// store secrets: the platform is unsupported, a required helper binary is
// missing, or a Linux session has no Secret Service daemon. Callers surface
// this with actionable guidance; a token must never be silently downgraded to
// plaintext storage as a fallback.
var ErrStoreUnavailable = errors.New("credential store unavailable")

// SecretStore persists per-account secrets in the OS credential store.
// Accounts are profile keys, e.g. the Atlassian account email.
type SecretStore interface {
	Get(account string) (string, error)
	Set(account, secret string) error
	Delete(account string) error
}

// AvailabilityStore is implemented by a SecretStore that can verify up front
// that the underlying store is usable before a login mutates anything.
type AvailabilityStore interface {
	// Available returns nil when the store is usable, or an error wrapping
	// ErrStoreUnavailable that names safe alternatives.
	Available() error
}

// MacOSKeychain stores secrets in the macOS Keychain via the `security`
// command. It stores a generic password under Service with each account as
// the account name (acct).
type MacOSKeychain struct {
	service  string
	lookPath func(string) (string, error)
}

// NewMacOSKeychain returns a keychain store for the given service label.
func NewMacOSKeychain(service string) *MacOSKeychain {
	if service == "" {
		service = Service
	}
	return &MacOSKeychain{service: service, lookPath: exec.LookPath}
}

// Available confirms the Keychain helper binary exists before login proceeds.
func (k *MacOSKeychain) Available() error {
	if _, err := requireTool(k.lookPath, macOSSecurityTool, "macOS Keychain"); err != nil {
		return err
	}
	return nil
}

func (k *MacOSKeychain) Get(account string) (string, error) {
	out, err := exec.Command(macOSSecurityTool, "find-generic-password",
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

func (k *MacOSKeychain) Set(account, secret string) error {
	if err := exec.Command(macOSSecurityTool, "add-generic-password",
		"-a", account, "-s", k.service, "-w", secret, "-U").Run(); err != nil {
		return fmt.Errorf("save to keychain: %w", err)
	}
	return nil
}

func (k *MacOSKeychain) Delete(account string) error {
	if err := exec.Command(macOSSecurityTool, "delete-generic-password",
		"-a", account, "-s", k.service).Run(); err != nil {
		return fmt.Errorf("delete keychain entry: %w", err)
	}
	return nil
}

// LinuxSecretStore persists secrets through the freedesktop Secret Service,
// the Linux desktop keyring, via the `secret-tool` CLI shipped with libsecret.
// It requires a running Secret Service session (GNOME Keyring / KWallet); a
// headless server without one must use environment credentials instead.
type LinuxSecretStore struct {
	service  string
	lookPath func(string) (string, error)
}

// NewLinuxSecretStore returns a Secret Service store for the given service
// label. Secrets are keyed by the {service, account} attribute pair.
func NewLinuxSecretStore(service string) *LinuxSecretStore {
	if service == "" {
		service = Service
	}
	return &LinuxSecretStore{service: service, lookPath: exec.LookPath}
}

// Available reports whether secret-tool is installed and a Secret Service
// daemon is reachable on the session bus. `secret-tool search` exits 0 when
// the daemon answers, even when nothing matches.
func (l *LinuxSecretStore) Available() error {
	path, err := requireTool(l.lookPath, linuxSecretTool, "Linux Secret Service")
	if err != nil {
		return err
	}
	if err := exec.Command(path, "search", "--all", "service", l.service).Run(); err != nil {
		return fmt.Errorf("%w: Linux Secret Service daemon is not reachable: %v. Start a desktop keyring (GNOME Keyring / KWallet), or use the BITBUCKET_API_TOKEN / BITBUCKET_EMAIL environment variables instead.", ErrStoreUnavailable, err)
	}
	return nil
}

func (l *LinuxSecretStore) Get(account string) (string, error) {
	path, err := requireTool(l.lookPath, linuxSecretTool, "Linux Secret Service")
	if err != nil {
		return "", err
	}
	var stderr bytes.Buffer
	cmd := exec.Command(path, "lookup", "service", l.service, "account", account)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		// secret-tool reports a missing item and a failing daemon with the
		// same exit code. An empty result with no diagnostic means "not
		// found"; a diagnostic on stderr means the store itself is unusable.
		if len(out) == 0 && strings.TrimSpace(stderr.String()) == "" {
			return "", ErrNotFound
		}
		return "", secretActionError("lookup in Secret Service", stderr, err)
	}
	if len(out) == 0 {
		return "", ErrNotFound
	}
	return strings.TrimSpace(string(out)), nil
}

func (l *LinuxSecretStore) Set(account, secret string) error {
	path, err := requireTool(l.lookPath, linuxSecretTool, "Linux Secret Service")
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd := exec.Command(path, "store", "--label", "Bitbucket CLI",
		"service", l.service, "account", account)
	cmd.Stdin = strings.NewReader(secret)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return secretActionError("save to Secret Service", stderr, err)
	}
	return nil
}

func (l *LinuxSecretStore) Delete(account string) error {
	path, err := requireTool(l.lookPath, linuxSecretTool, "Linux Secret Service")
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd := exec.Command(path, "clear", "service", l.service, "account", account)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// `secret-tool clear` only removes unlocked matching items; a locked
		// collection or an item that could not be removed must not be reported
		// as "daemon unavailable". Only genuine daemon/bus failures map to
		// ErrStoreUnavailable.
		return secretActionError("clear the entry from Secret Service", stderr, err)
	}
	return nil
}

// UnsupportedStore is the fallback for platforms without a secure credential
// store. It never stores or reads a secret.
type UnsupportedStore struct {
	goos string
}

// NewUnsupportedStore returns a store that declines every operation.
func NewUnsupportedStore(goos string) *UnsupportedStore {
	return &UnsupportedStore{goos: goos}
}

// Available always fails: there is no secure store for this platform.
func (u *UnsupportedStore) Available() error { return u.unavailable() }

func (u *UnsupportedStore) Get(account string) (string, error) { return "", u.unavailable() }
func (u *UnsupportedStore) Set(account, secret string) error   { return u.unavailable() }
func (u *UnsupportedStore) Delete(account string) error        { return u.unavailable() }

func (u *UnsupportedStore) unavailable() error {
	return fmt.Errorf("%w: no credential store for platform %s; use the BITBUCKET_API_TOKEN / BITBUCKET_EMAIL environment variables instead", ErrStoreUnavailable, u.goos)
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

// NewStoreForOS returns the platform-appropriate secret store behind
// DefaultStore. It is the injectable backend factory, so tests can exercise
// backend selection with an explicit GOOS.
func NewStoreForOS(goos string) SecretStore {
	switch goos {
	case "darwin":
		return NewMacOSKeychain(Service)
	case "linux":
		return NewLinuxSecretStore(Service)
	default:
		return NewUnsupportedStore(goos)
	}
}

// DefaultStore returns the backend for the current platform (runtime.GOOS).
func DefaultStore() SecretStore { return NewStoreForOS(runtime.GOOS) }

// globalStore is swapped by tests; DefaultStore() is used when nil.
var globalStore SecretStore

// CurrentStore returns the active SecretStore: the injected test store if set,
// otherwise the platform default.
func CurrentStore() SecretStore {
	if globalStore != nil {
		return globalStore
	}
	return DefaultStore()
}

// SetGlobalStore injects a SecretStore for tests (nil restores the default).
func SetGlobalStore(s SecretStore) { globalStore = s }

// SnapshotSecret returns the secret currently stored under account, plus
// whether one existed. All errors other than ErrNotFound are surfaced so a
// caller never overwrites a value it was unable to inspect.
func SnapshotSecret(store SecretStore, account string) (string, bool, error) {
	prev, err := store.Get(account)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	return prev, true, nil
}

// RestoreSecret reverts a store.Set(account, _) that must be rolled back: the
// previous value is written back when one existed, otherwise the newly written
// entry is deleted. This is a rollback-only helper.
func RestoreSecret(store SecretStore, account, prev string, existed bool) error {
	if existed {
		return store.Set(account, prev)
	}
	return store.Delete(account)
}

const (
	macOSSecurityTool = "security"
	linuxSecretTool   = "secret-tool"
)

// requireTool resolves the backing CLI's absolute path, explaining on failure
// that no usable credential store can be built and how to proceed safely.
func requireTool(lookPath func(string) (string, error), name, display string) (string, error) {
	path, err := lookPath(name)
	if err != nil {
		return "", fmt.Errorf("%w: %s requires %q, which is not installed: %v. Provide the credential via the BITBUCKET_API_TOKEN / BITBUCKET_EMAIL environment variables instead.", ErrStoreUnavailable, display, name, err)
	}
	return path, nil
}

// secretActionError wraps a failing secret-tool operation. Only failures that
// indicate the Secret Service daemon itself is unreachable are mapped to
// ErrStoreUnavailable; an item-level failure (such as a locked collection that
// cannot be cleared) is reported as an ordinary error so it is never confused
// with "the store is down".
func secretActionError(action string, stderr bytes.Buffer, err error) error {
	msg := strings.TrimSpace(stderr.String())
	if msg == "" {
		msg = err.Error()
	}
	if secretDaemonReachable(msg) {
		return fmt.Errorf("%w: Linux Secret Service daemon is not reachable: %s. Start a desktop keyring (GNOME Keyring / KWallet), or use the BITBUCKET_API_TOKEN / BITBUCKET_EMAIL environment variables instead.", ErrStoreUnavailable, msg)
	}
	return fmt.Errorf("Secret Service %s failed (%s); the item may be locked or the operation unsupported", action, msg)
}

// secretDaemonReachable reports whether a secret-tool diagnostic points at the
// Secret Service / session bus rather than at an individual item.
func secretDaemonReachable(msg string) bool {
	lower := strings.ToLower(msg)
	for _, probe := range []string{
		"no session bus",
		"session bus",
		"not provided by any .service",
		"cannot autolaunch",
		"failed to connect",
		"could not connect",
		"connection is closed",
		"no dbus",
		"org.freedesktop.secrets",
	} {
		if strings.Contains(lower, probe) {
			return true
		}
	}
	return false
}
