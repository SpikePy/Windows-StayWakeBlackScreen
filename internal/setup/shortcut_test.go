//go:build windows

package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Windows-only, so Linux CI skips it; it covers the shortcut code for
// anyone building or testing on Windows.
func TestCreateShortcutRoundTrip(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "Target.exe")
	if err := os.WriteFile(target, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	lnk := filepath.Join(dir, "Target.lnk")

	if err := createShortcut(lnk, target, "round trip"); err != nil {
		t.Fatalf("createShortcut: %v", err)
	}
	if _, err := os.Stat(lnk); err != nil {
		t.Fatalf("shortcut was not written: %v", err)
	}

	got, err := shortcutTarget(lnk)
	if err != nil {
		t.Fatalf("shortcutTarget: %v", err)
	}
	if !strings.EqualFold(got, target) {
		t.Errorf("shortcut points at %q, want %q", got, target)
	}

	// Installing again must replace the shortcut, not fail or duplicate.
	if err := createShortcut(lnk, target, "round trip"); err != nil {
		t.Fatalf("createShortcut again: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var links int
	for _, e := range entries {
		if strings.EqualFold(filepath.Ext(e.Name()), ".lnk") {
			links++
		}
	}
	if links != 1 {
		t.Errorf("found %d shortcuts in the folder, want exactly 1", links)
	}
}
