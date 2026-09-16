//go:build windows

package autostart

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Windows-only, like the shortcut test: it covers the setting-to-folder
// logic in a temp folder, never the real Startup folder.
func TestApplyInFollowsTheSetting(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "Target.exe")
	if err := os.WriteFile(target, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, linkName)
	exists := func() bool {
		_, err := os.Stat(link)
		return err == nil
	}

	if err := applyIn(dir, false, target); err != nil {
		t.Fatalf("disabling with no shortcut yet: %v", err)
	}
	if exists() {
		t.Fatal("disabled, yet a shortcut exists")
	}

	if err := applyIn(dir, true, target); err != nil {
		t.Fatalf("enabling: %v", err)
	}
	if !exists() {
		t.Fatal("enabled, yet no shortcut exists")
	}
	if got, err := shortcutTarget(link); err != nil || !strings.EqualFold(got, target) {
		t.Fatalf("shortcut points at %q (%v), want %q", got, err, target)
	}

	if err := applyIn(dir, true, target); err != nil {
		t.Fatalf("enabling again: %v", err)
	}
	links, _ := filepath.Glob(filepath.Join(dir, "*.lnk"))
	if len(links) != 1 {
		t.Fatalf("enabling twice left %d shortcuts, want 1", len(links))
	}

	if err := applyIn(dir, false, target); err != nil {
		t.Fatalf("disabling: %v", err)
	}
	if exists() {
		t.Fatal("disabled, yet the shortcut is still there")
	}
}
