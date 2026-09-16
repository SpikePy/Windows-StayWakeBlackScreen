//go:build windows

package shortcut

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Windows-only, so Linux CI skips it; it covers the IShellLink code for
// anyone building or testing on Windows.
func TestCreateShortcutRoundTrip(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "Target.exe")
	if err := os.WriteFile(target, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	lnk := filepath.Join(dir, "Target.lnk")

	if err := createShortcut(lnk, target, "-flag value", "round trip"); err != nil {
		t.Fatalf("createShortcut: %v", err)
	}
	got, args, err := shortcutTarget(lnk)
	if err != nil {
		t.Fatalf("shortcutTarget: %v", err)
	}
	if !strings.EqualFold(got, target) || args != "-flag value" {
		t.Errorf("shortcut runs %q %q, want %q %q", got, args, target, "-flag value")
	}

	// Writing it again must replace the shortcut, not fail or duplicate it.
	if err := createShortcut(lnk, target, "", "round trip"); err != nil {
		t.Fatalf("createShortcut again: %v", err)
	}
	if _, args, _ := shortcutTarget(lnk); args != "" {
		t.Errorf("rewritten shortcut still has arguments %q", args)
	}
	if links, _ := filepath.Glob(filepath.Join(dir, "*.lnk")); len(links) != 1 {
		t.Errorf("found %d shortcuts in the folder, want exactly 1", len(links))
	}
}
