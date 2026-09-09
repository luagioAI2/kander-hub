//go:build linux

package fs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// TestPublishViaLink covers the RENAME_NOREPLACE fallback primitive used when a
// filesystem (notably WSL v9fs) rejects RENAME_NOREPLACE with EINVAL. It is a
// direct-link semantics test, independent of the higher-level protected write API.
func TestPublishViaLink(t *testing.T) {
	dir := t.TempDir()
	dirfd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		t.Fatalf("open test dir fd: %v", err)
	}
	t.Cleanup(func() { unix.Close(dirfd) })

	tempName := "temp-payload"
	destName := "dest-payload"
	tempPath := filepath.Join(dir, tempName)
	destPath := filepath.Join(dir, destName)

	symlinkUnsupported := func(err error) bool {
		// On an exotic Linux host where hard links are refused (e.g. overlay
		// special mounts), report the primitive as unsupported rather than fail.
		return err == unix.EOPNOTSUPP || err == unix.EMLINK || err == unix.EPERM
	}

	t.Run("publishes and removes temp", func(t *testing.T) {
		os.WriteFile(tempPath, []byte("data"), 0o600)
		if err := publishViaLink(dirfd, tempName, destName, filepath.Join(dir, destName)); err != nil {
			if symlinkUnsupported(err) {
				t.Skipf("hard links not supported here: %v", err)
			}
			t.Fatalf("publishViaLink: %v", err)
		}
		b, err := os.ReadFile(destPath)
		if err != nil {
			t.Fatalf("dest not published: %v", err)
		}
		if string(b) != "data" {
			t.Fatalf("dest content = %q, want %q", b, "data")
		}
		if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
			t.Fatalf("temp should be removed, stat err = %v", err)
		}
	})

	t.Run("rejects existing destination and preserves it", func(t *testing.T) {
		os.WriteFile(tempPath, []byte("new"), 0o600)
		os.WriteFile(destPath, []byte("original"), 0o600)
		err := publishViaLink(dirfd, tempName, destName, filepath.Join(dir, destName))
		if err == nil {
			t.Fatalf("expected existError for existing destination, got nil")
		}
		if !strings.Contains(err.Error(), "already exists") && !strings.Contains(err.Error(), "exists") {
			t.Fatalf("unexpected error text: %v", err)
		}
		b, err := os.ReadFile(destPath)
		if err != nil {
			t.Fatalf("re-read dest: %v", err)
		}
		if string(b) != "original" {
			t.Fatalf("existing dest overwritten: content = %q, want %q", b, "original")
		}
		if _, err := os.Stat(tempPath); err != nil {
			t.Fatalf("temp should be retained on failure, stat err = %v", err)
		}
	})
}