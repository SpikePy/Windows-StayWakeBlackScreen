//go:build windows

// Package autostart keeps the Startup-folder shortcut that launches
// StayWakeBlackScreenIdle.exe at sign-in in line with the autostart
// setting. Setup applies the setting when installing, and the program
// applies it again every time it starts, so an edited config.yaml takes
// effect without re-running Setup.
package autostart

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	// linkName is the shortcut placed in the user's own Startup folder.
	linkName = "StayWakeBlackScreenIdle.lnk"

	// Versions before the Startup shortcut autostarted through this
	// registry value. It is only ever deleted now, never written.
	legacyRunKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	legacyRunValueName = "StayWakeBlackScreenIdle"
)

// Apply makes the user's Startup folder match the setting: a shortcut to
// target when enabled, no shortcut when not. It also removes the registry
// value older versions used. Everything it touches is per-user, so it
// needs no administrator rights.
func Apply(enabled bool, target string) error {
	dir, err := windows.KnownFolderPath(windows.FOLDERID_Startup, 0)
	if err != nil {
		return fmt.Errorf("locating the Startup folder: %w", err)
	}
	if err := applyIn(dir, enabled, target); err != nil {
		return err
	}
	return removeLegacyRunValue()
}

// Remove deletes the shortcut whatever the setting says, as uninstalling
// does.
func Remove() error { return Apply(false, "") }

// applyIn does Apply's file work in dir. Creating replaces an existing
// shortcut rather than adding a second one, and removing one that isn't
// there is not an error.
func applyIn(dir string, enabled bool, target string) error {
	link := filepath.Join(dir, linkName)
	if enabled {
		if err := createShortcut(link, target, "StayWakeBlackScreen idle guard"); err != nil {
			return fmt.Errorf("creating %s: %w", link, err)
		}
		return nil
	}
	if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing %s: %w", link, err)
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
