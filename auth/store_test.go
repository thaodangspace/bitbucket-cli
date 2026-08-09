package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewStoreForOS(t *testing.T) {
	cases := []struct {
		goos string
		want SecretStore
	}{
		{"darwin", &MacOSKeychain{}},
		{"linux", &LinuxSecretStore{}},
		{"windows", &UnsupportedStore{}},
		{"freebsd", &UnsupportedStore{}},
	}
	for _, tc := range cases {
		s := NewStoreForOS(tc.goos)
		switch tc.want.(type) {
		case *MacOSKeychain:
			if _, ok := s.(*MacOSKeychain); !ok {
				t.Errorf("NewStoreForOS(%q) = %T", tc.goos, s)
			}
		case *LinuxSecretStore:
			if _, ok := s.(*LinuxSecretStore); !ok {
				t.Errorf("NewStoreForOS(%q) = %T", tc.goos, s)
			}
		case *UnsupportedStore:
			u, ok := s.(*UnsupportedStore)
			if !ok {
				t.Errorf("NewStoreForOS(%q) = %T", tc.goos, s)
			} else if u.goos != tc.goos {
				t.Errorf("NewStoreForOS(%q) goos = %q", tc.goos, u.goos)
			}
		}
	}
}

func TestUnsupportedStoreDeclinesEverything(t *testing.T) {
	u := NewUnsupportedStore("windows")
	for _, op := range []struct {
		name string
		err  error
	}{
		{"Get", func() error { _, err := u.Get("a"); return err }()},
		{"Set", u.Set("a", "b")},
		{"Delete", u.Delete("a")},
		{"Available", u.Available()},
	} {
		if !errors.Is(op.err, ErrStoreUnavailable) {
			t.Errorf("%s: got %v, want ErrStoreUnavailable", op.name, op.err)
		}
	}
	msg := u.Set("a", "b").Error()
	if !strings.Contains(msg, "windows") {
		t.Errorf("message %q should name the platform", msg)
	}
}

// writeSecretToolScript writes a fake `secret-tool` into dir. It persists
// secrets under $TOOL_STATE_DIR keyed by service:account. When
// $TOOL_FAIL_MARKER exists, every verb fails like a missing Secret Service
// daemon (diagnostic on stderr, exit 1).
func writeSecretToolScript(t *testing.T, dir string) string {
	t.Helper()
	script := `#!/bin/sh
verb="$1"
shift
svc=""
acct=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --label|-l|--all|--unlock) shift ;;
    service) shift; svc="$1" ;;
    account) shift; acct="$1" ;;
    *) ;;
  esac
  shift
done
if [ -n "$TOOL_FAIL_MARKER" ] && [ -f "$TOOL_FAIL_MARKER" ]; then
  echo "no session bus" >&2
  exit 1
fi
dir="$TOOL_STATE_DIR"
mkdir -p "$dir"
key="$svc:$acct"
case "$verb" in
  lookup)
    if [ -f "$dir/$key" ]; then cat "$dir/$key"; exit 0; fi
    exit 1
    ;;
  search)
    exit 0
    ;;
  store)
    cat > "$dir/$key"
    ;;
  clear)
    rm -f "$dir/$key"
    ;;
esac
exit 0
`
	path := filepath.Join(dir, "secret-tool")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeSecurityScript writes a fake `security` binary that emulates the
// generic-password subcommands bitbucket-cli uses.
func writeSecurityScript(t *testing.T, dir string) string {
	t.Helper()
	script := `#!/bin/sh
verb="$1"
shift
acct=""
svc=""
addw="0"
secret=""
prev=""
while [ "$#" -gt 0 ]; do
  case "$prev" in
    a) acct="$1" ;;
    s) svc="$1" ;;
    w) secret="$1" ;;
  esac
  prev=""
  case "$1" in
    -a) prev="a" ;;
    -s) prev="s" ;;
    -w) if [ "$verb" = "add-generic-password" ]; then prev="w"; fi ;;
  esac
  shift
done
key="$acct:$svc"
dir="$TOOL_STATE_DIR"
mkdir -p "$dir"
case "$verb" in
  find-generic-password)
    if [ -f "$dir/$key" ]; then cat "$dir/$key"; exit 0; fi
    echo "security: item not found" >&2
    exit 44
    ;;
  add-generic-password)
    printf '%s' "$secret" > "$dir/$key"
    ;;
  delete-generic-password)
    rm -f "$dir/$key"
    ;;
esac
exit 0
`
	path := filepath.Join(dir, "security")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func envForTool(t *testing.T, stateDir string) {
	t.Helper()
	t.Setenv("TOOL_STATE_DIR", stateDir)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestMacOSKeychainAvailableMissingSecurity(t *testing.T) {
	k := NewMacOSKeychain(Service)
	k.lookPath = func(string) (string, error) { return "", errors.New("not installed") }
	if err := k.Available(); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("available = %v, want ErrStoreUnavailable", err)
	}
}

func TestMacOSKeychainRoundTripAndMissing(t *testing.T) {
	dir := t.TempDir()
	writeSecurityScript(t, dir)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	envForTool(t, filepath.Join(dir, "state"))

	k := NewMacOSKeychain(Service)
	if _, err := k.Get("dev@example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing item: got %v, want ErrNotFound", err)
	}
	if err := k.Set("dev@example.com", "s3cret"); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err := k.Get("dev@example.com")
	if err != nil || got != "s3cret" {
		t.Fatalf("get: %q %v", got, err)
	}
	if err := k.Delete("dev@example.com"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := k.Get("dev@example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: got %v, want ErrNotFound", err)
	}
}

func TestLinuxSecretStoreToolMissing(t *testing.T) {
	l := NewLinuxSecretStore(Service)
	l.lookPath = func(string) (string, error) { return "", errors.New("command not found") }
	if err := l.Available(); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("available = %v, want ErrStoreUnavailable", err)
	}
	if err := l.Set("a", "b"); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("set = %v, want ErrStoreUnavailable", err)
	}
	if _, err := l.Get("a"); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("get = %v, want ErrStoreUnavailable", err)
	}
	if err := l.Delete("a"); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("delete = %v, want ErrStoreUnavailable", err)
	}
}

func TestLinuxSecretStoreDaemonUnavailable(t *testing.T) {
	dir := t.TempDir()
	bin := writeSecretToolScript(t, dir)
	envForTool(t, filepath.Join(dir, "state"))
	marker := filepath.Join(dir, "fail")
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TOOL_FAIL_MARKER", marker)

	l := &LinuxSecretStore{service: Service, lookPath: func(string) (string, error) { return bin, nil }}
	if err := l.Available(); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("available = %v, want ErrStoreUnavailable", err)
	}
	if err := l.Set("a", "b"); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("set = %v, want ErrStoreUnavailable", err)
	}
	if _, err := l.Get("a"); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("get = %v, want ErrStoreUnavailable", err)
	}
}

func TestLinuxSecretStoreRoundTripAndNotFound(t *testing.T) {
	dir := t.TempDir()
	bin := writeSecretToolScript(t, dir)
	envForTool(t, filepath.Join(dir, "state"))

	l := &LinuxSecretStore{service: Service, lookPath: func(string) (string, error) { return bin, nil }}
	if err := l.Available(); err != nil {
		t.Fatalf("available = %v", err)
	}
	if _, err := l.Get("dev@example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing item: got %v, want ErrNotFound", err)
	}
	if err := l.Set("dev@example.com", "s3cret"); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err := l.Get("dev@example.com")
	if err != nil || got != "s3cret" {
		t.Fatalf("get: %q %v", got, err)
	}
	if err := l.Delete("dev@example.com"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := l.Get("dev@example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: got %v, want ErrNotFound", err)
	}
}
