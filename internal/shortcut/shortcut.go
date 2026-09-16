//go:build windows

// Package shortcut manages the two shortcuts StayWakeBlackScreen can have,
// both per-user so nothing needs administrator rights: the one in the
// Startup folder that starts the idle guard in the background at sign-in,
// kept in line with the autostart setting, and the Start menu entry that
// opens the program for an instant black screen.
package shortcut

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	// BackgroundArg starts the program as the idle guard instead of
	// blacking out the screen at once. The Startup shortcut passes it.
	BackgroundArg = "-background"

	linkName = "StayWakeBlackScreen.lnk"

	// What older versions used for autostart, only ever removed now: the
	// Startup shortcut of the separate idle program, and before that a
	// registry value.
	legacyLinkName     = "StayWakeBlackScreenIdle.lnk"
	legacyRunKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	legacyRunValueName = "StayWakeBlackScreenIdle"
)

// SyncAutostart makes the user's Startup folder match the autostart
// setting: a shortcut that starts target in the background when enabled,
// none when not.
func SyncAutostart(enabled bool, target string) error {
	dir, err := knownFolder(windows.FOLDERID_Startup, "Startup")
	if err != nil {
		return err
	}
	if err := syncIn(dir, enabled, target, BackgroundArg, "StayWakeBlackScreen idle guard"); err != nil {
		return err
	}
	if err := removeFile(filepath.Join(dir, legacyLinkName)); err != nil {
		return err
	}
	return removeLegacyRunValue()
}

// SetStartMenu adds, or with enabled false removes, the Start menu entry
// that opens target for an instant black screen.
func SetStartMenu(enabled bool, target string) error {
	dir, err := knownFolder(windows.FOLDERID_Programs, "Start menu")
	if err != nil {
		return err
	}
	return syncIn(dir, enabled, target, "", "Black out the screen now - press Escape to end it")
}

// syncIn does the file work in dir. Creating replaces an existing shortcut
// rather than adding a second one, and removing one that isn't there is
// not an error.
func syncIn(dir string, enabled bool, target, args, description string) error {
	link := filepath.Join(dir, linkName)
	if !enabled {
		return removeFile(link)
	}
	if err := createShortcut(link, target, args, description); err != nil {
		return fmt.Errorf("creating %s: %w", link, err)
	}
	return nil
}

func knownFolder(id *windows.KNOWNFOLDERID, name string) (string, error) {
	dir, err := windows.KnownFolderPath(id, 0)
	if err != nil {
		return "", fmt.Errorf("locating the %s folder: %w", name, err)
	}
	return dir, nil
}

func removeFile(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing %s: %w", path, err)
	}
	return nil
}

func removeLegacyRunValue() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, legacyRunKeyPath, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer key.Close()
	if err := key.DeleteValue(legacyRunValueName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}
