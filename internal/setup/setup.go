//go:build windows

// Package setup implements what Setup_StayWakeBlackScreen.exe does:
// installing StayWakeBlackScreen.exe for either of its two uses - the idle
// guard that starts in the background at sign-in, or the instant black
// screen opened from the Start menu - and uninstalling it again.
package setup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"windows-stay-wake-black-screen/internal/config"
	"windows-stay-wake-black-screen/internal/shortcut"
)

const (
	// appName is both the release asset and the installed exe.
	appName = "StayWakeBlackScreen.exe"

	// legacyIdleName is the separate idle program that versions before 2.0
	// installed alongside it; Setup stops and deletes it.
	legacyIdleName = "StayWakeBlackScreenIdle.exe"

	userAgent = "stay-wake-setup"
)

// Use is what the program gets installed for.
type Use int

const (
	// Background is the idle guard: started right after installing, and
	// at every sign-in through the Startup shortcut.
	Background Use = iota
	// Instant is a Start menu entry that blacks out the screen when opened.
	Instant
)

// resolveInstallDir returns dir, or %LOCALAPPDATA%\StayWakeBlackScreen if
// dir is empty.
func resolveInstallDir(dir string) (string, error) {
	if dir != "" {
		return dir, nil
	}
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return "", fmt.Errorf("%%LOCALAPPDATA%% is not set")
	}
	return filepath.Join(base, "StayWakeBlackScreen"), nil
}

// InstallOptions configures Install.
type InstallOptions struct {
	Use         Use
	InstallDir  string            // defaults to %LOCALAPPDATA%\StayWakeBlackScreen if empty
	NoLaunch    bool              // Background: don't start the guard now
	NoAutostart bool              // leave the autostart setting and Startup shortcut as they are
	Progress    func(step string) // told about each step as it starts; may be nil
}

// Install downloads the latest released StayWakeBlackScreen.exe, installs
// it under the current user's %LOCALAPPDATA%, and sets it up for the chosen
// use. For Background it turns the autostart setting on, keeps the Startup
// shortcut in line with it and starts the guard; for Instant it turns
// autostart off and adds the Start menu entry instead. Any running copy is
// stopped first so the file can be replaced, and re-running replaces
// shortcuts rather than adding second ones, so it doubles as the update.
func Install(opts InstallOptions) error {
	progress := reporter(opts.Progress)
	installDir, err := resolveInstallDir(opts.InstallDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return fmt.Errorf("creating install dir: %w", err)
	}
	target := filepath.Join(installDir, appName)

	// The version is only shown, so failing to learn it isn't fatal; the
	// download below reports any real network problem.
	progress("Looking up the latest release...")
	if tag, err := latestTag(); err == nil {
		progress(fmt.Sprintf("Downloading StayWakeBlackScreen %s...", tag))
	} else {
		progress("Downloading the latest StayWakeBlackScreen...")
	}
	tmpPath := target + ".download"
	if err := downloadFile(latestAssetURL(appName), tmpPath); err != nil {
		return fmt.Errorf("downloading: %w", err)
	}

	progress("Stopping StayWakeBlackScreen if it's running...")
	for _, exe := range []string{appName, legacyIdleName} {
		if err := terminateRunning(exe); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("stopping %s: %w", exe, err)
		}
	}

	progress("Installing to " + installDir + "...")
	if err := replaceFile(tmpPath, target); err != nil {
		return fmt.Errorf("installing: %w", err)
	}
	if err := os.Remove(filepath.Join(installDir, legacyIdleName)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing the old idle program: %w", err)
	}

	background := opts.Use == Background
	if !opts.NoAutostart {
		if background {
			progress("Turning on autostart...")
		} else {
			progress("Turning off autostart...")
		}
		if err := config.SetAutostart(background); err != nil {
			return fmt.Errorf("updating config.yaml: %w", err)
		}
		if err := shortcut.SyncAutostart(background, target); err != nil {
			return fmt.Errorf("updating autostart: %w", err)
		}
	}

	progress("Updating the Start menu...")
	if err := shortcut.SetStartMenu(!background, target); err != nil {
		return fmt.Errorf("updating the Start menu: %w", err)
	}

	if background && !opts.NoLaunch {
		progress("Starting the idle guard...")
		if err := exec.Command(target, shortcut.BackgroundArg).Start(); err != nil {
			return fmt.Errorf("starting %s: %w", target, err)
		}
	}

	progress("Done.")
	return nil
}

// UninstallOptions configures Uninstall.
type UninstallOptions struct {
	InstallDir string            // defaults to %LOCALAPPDATA%\StayWakeBlackScreen if empty
	KeepFiles  bool              // remove the shortcuts and stop the program, but leave the installed files in place
	Progress   func(step string) // told about each step as it starts; may be nil
}

// Uninstall reverses Install: removes both shortcuts (and what older
// versions used for autostart), stops any running copy - including the
// pre-2.0 idle program - and, unless KeepFiles, deletes the installed
// files together with config.yaml.
func Uninstall(opts UninstallOptions) error {
	progress := reporter(opts.Progress)
	installDir, err := resolveInstallDir(opts.InstallDir)
	if err != nil {
		return err
	}

	progress("Removing the shortcuts...")
	if err := shortcut.SyncAutostart(false, ""); err != nil {
		return fmt.Errorf("removing autostart: %w", err)
	}
	if err := shortcut.SetStartMenu(false, ""); err != nil {
		return fmt.Errorf("removing the Start menu entry: %w", err)
	}

	progress("Stopping StayWakeBlackScreen if it's running...")
	for _, exe := range []string{appName, legacyIdleName} {
		if err := terminateRunning(exe); err != nil {
			return fmt.Errorf("stopping %s: %w", exe, err)
		}
	}

	if !opts.KeepFiles {
		progress("Removing " + installDir + "...")
		if err := os.RemoveAll(installDir); err != nil {
			return fmt.Errorf("removing %s: %w", installDir, err)
		}
	}

	progress("Done.")
	return nil
}

func reporter(progress func(string)) func(string) {
	if progress == nil {
		return func(string) {}
	}
	return progress
}

// latestTag returns the newest release's tag, read from where GitHub's
// "latest release" page redirects to - no GitHub API involved.
func latestTag() (string, error) {
	location, err := redirectTarget(latestPageURL())
	if err != nil {
		return "", err
	}
	return tagFromLocation(location)
}

// downloadFile saves url's content to destPath, removing the file again
// if the download fails partway.
func downloadFile(url, destPath string) error {
	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if err := httpGet(url, nil, out); err != nil {
		out.Close()
		os.Remove(destPath)
		return err
	}
	return out.Close()
}

// replaceFile moves tmpPath onto targetPath, retrying briefly: the target
// may still be momentarily locked right after terminateRunning killed the
// process that had it open/mapped.
func replaceFile(tmpPath, targetPath string) error {
	var err error
	for i := 0; i < 10; i++ {
		if err = os.Rename(tmpPath, targetPath); err == nil {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	os.Remove(tmpPath)
	return err
}

// terminateRunning finds every running process whose image file name
// matches exeName (case-insensitively) and terminates it, waiting briefly
// for each to actually exit.
func terminateRunning(exeName string) error {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return fmt.Errorf("CreateToolhelp32Snapshot: %w", err)
	}
	defer windows.CloseHandle(snap)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))

	var pids []uint32
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		if strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), exeName) {
			pids = append(pids, entry.ProcessID)
		}
	}

	for _, pid := range pids {
		h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, pid)
		if err != nil {
			continue // already gone, or no permission - nothing more we can do
		}
		windows.TerminateProcess(h, 0)
		windows.WaitForSingleObject(h, 5000)
		windows.CloseHandle(h)
	}
	return nil
}
