//go:build windows

package shortcut

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Windows-only, like the IShellLink test: these work in a temp folder,
// never in the real Startup folder or Start menu.

func newTarget(t *testing.T) (dir, target string) {
	t.Helper()
	dir = t.TempDir()
	target = filepath.Join(dir, "Target.exe")
	if err := os.WriteFile(target, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir, target
}

func TestSyncInFollowsTheSetting(t *testing.T) {
	dir, target := newTarget(t)
	link := filepath.Join(dir, linkName)
	exists := func() bool {
		_, err := os.Stat(link)
		return err == nil
	}

	if err := syncIn(dir, false, target, BackgroundArg, "test"); err != nil {
		t.Fatalf("disabling with no shortcut yet: %v", err)
	}
	if exists() {
		t.Fatal("disabled, yet a shortcut exists")
	}

	if err := syncIn(dir, true, target, BackgroundArg, "test"); err != nil {
		t.Fatalf("enabling: %v", err)
	}
	got, args, err := shortcutTarget(link)
	if err != nil || !strings.EqualFold(got, target) || args != BackgroundArg {
		t.Fatalf("shortcut runs %q %q (%v), want %q %q", got, args, err, target, BackgroundArg)
	}

	if err := syncIn(dir, true, target, BackgroundArg, "test"); err != nil {
		t.Fatalf("enabling again: %v", err)
	}
	if links, _ := filepath.Glob(filepath.Join(dir, "*.lnk")); len(links) != 1 {
		t.Fatalf("enabling twice left %d shortcuts, want 1", len(links))
	}

	if err := syncIn(dir, false, target, BackgroundArg, "test"); err != nil {
		t.Fatalf("disabling: %v", err)
	}
	if exists() {
		t.Fatal("disabled, yet the shortcut is still there")
	}
}

// The Start menu entry opens the program with no arguments, which is what
// makes it black out the screen at once.
func TestSyncInWithoutArguments(t *testing.T) {
	dir, target := newTarget(t)
	if err := syncIn(dir, true, target, "", "test"); err != nil {
		t.Fatal(err)
	}
	got, args, err := shortcutTarget(filepath.Join(dir, linkName))
	if err != nil || !strings.EqualFold(got, target) || args != "" {
		t.Fatalf("shortcut runs %q %q (%v), want %q with no arguments", got, args, err, target)
	}
}
