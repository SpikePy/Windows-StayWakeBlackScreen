//go:build windows

// Command staywakeblackscreenidle is a background idle guard - unlike
// staywakeblackscreen, it does NOT block input or show the black screen
// immediately. It just prevents the PC from sleeping/locking and keeps
// running in the background. Only after idle_minutes (default 3, set via
// config.yaml in %LOCALAPPDATA%\StayWakeBlackScreen, or overridden with
// -idle-minutes) of no real keyboard/mouse activity does it show the
// black screen and block all input, exactly like staywakeblackscreen.
// Pressing Escape then dismisses the black screen and restores input, but
// the program itself keeps running - the idle countdown simply restarts,
// and it will black out again after another idle_minutes of inactivity,
// repeating indefinitely.
//
// A tray icon (monitor glyph = enabled, same glyph greyed out with a
// diagonal red strike = disabled) lets the user pause/resume without
// stopping the process: left-click toggles it, right-click opens an
// Enable/Disable/Configure/Exit menu. Configure opens config.yaml in
// whatever application Windows has associated with .yaml files.
//
// This program does not exit on its own otherwise. To stop it: the tray
// menu's Exit, Task Manager, taskkill, or the installer (which does this
// automatically when updating).
//
// While blacked out: ALL keyboard and mouse input is blocked system-wide.
// Only Escape ends the black screen. Ctrl+Alt+Del always remains available
// as a hard escape hatch, since Windows never lets any hook suppress it.
//
// Logging is OFF by default. Pass -enable-logging to write diagnostics to
// StayWakeBlackScreenIdle.log next to the exe, for troubleshooting only.
//
// Built with -ldflags "-H=windowsgui" so it never shows a console window;
// everything below runs inside a top-level recover() that never lets a
// panic surface as a crash dialog - diagnostics go only to the log file.
package main

import (
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"windows-stay-wake-black-screen/internal/applog"
	"windows-stay-wake-black-screen/internal/autostart"
	"windows-stay-wake-black-screen/internal/blackout"
	"windows-stay-wake-black-screen/internal/config"
	"windows-stay-wake-black-screen/internal/singleinstance"
)

// version is stamped in at build time via -ldflags "-X main.version=...";
// left as "dev" for local/manual builds.
var version = "dev"

func main() {
	// Win32 hooks, timers and the message queue are bound to the OS thread
	// that creates them; the Go runtime must never migrate this goroutine
	// to a different one mid-run.
	runtime.LockOSThread()

	cfg, cfgErr := config.Load()

	idleMinutes := flag.Int("idle-minutes", cfg.IdleMinutes, "minutes of inactivity before blacking out (overrides config.yaml)")
	heartbeatSeconds := flag.Int("heartbeat-seconds", cfg.HeartbeatSeconds, "seconds between Caps Lock activity heartbeats while blacked out (overrides config.yaml)")
	startEnabled := flag.Bool("start-enabled", cfg.StartEnabled, "whether the idle guard is active on launch (overrides config.yaml)")
	autostartOn := flag.Bool("autostart", cfg.Autostart, "start at sign-in through a Startup-folder shortcut; the installed copy applies this every time it starts (overrides config.yaml)")
	enableLogging := flag.Bool("enable-logging", false, "write diagnostics to StayWakeBlackScreenIdle.log next to the exe")
	flag.Parse()

	logf, logPath := applog.New("StayWakeBlackScreenIdle.log", *enableLogging)
	if cfgErr != nil {
		logf("WARNING loading config.yaml (falling back to idle-minutes=%d): %v", config.DefaultIdleMinutes, cfgErr)
	}

	release, alreadyRunning, err := singleinstance.Acquire(`StayWakeBlackScreenIdle_SingleInstance`)
	if err != nil {
		logf("EXCEPTION acquiring single-instance mutex: %v", err)
		return
	}
	if alreadyRunning {
		logf("Another instance is already running - exiting.")
		return
	}
	defer release()

	syncAutostart(*autostartOn, logf)

	g := newGuard(logf, blackout.IdleThresholdMs(*idleMinutes), blackout.HeartbeatMs(*heartbeatSeconds), *startEnabled)
	defer func() {
		g.cleanup()
		logf("Cleanup done. Log at: %s", logPath)
	}()
	defer func() {
		if r := recover(); r != nil {
			logf("PANIC: %v", r)
		}
	}()

	blackout.EnableDPIAwareness()
	// Block sleep AND tell Windows the display must stay on for the
	// entire lifetime of this program while enabled (not just during
	// blackout), so it never sees a display-off/idle transition that
	// could trigger a session lock.
	blackout.BlockSleep()
	if !g.enabled {
		logf("Starting disabled (start_enabled=false)")
		blackout.RestoreExecutionState()
	}

	if err := g.start(); err != nil {
		logf("EXCEPTION %v", err)
		return
	}

	logf("Entering message loop (background idle guard, idleMinutes=%d)", *idleMinutes)
	if err := g.run(); err != nil {
		logf("EXCEPTION %v", err)
		return
	}
	logf("Message loop returned (Exit or unexpected shutdown).")
}

// syncAutostart keeps the Startup shortcut in line with the autostart
// setting, so an edited config.yaml takes effect on the next start without
// re-running Setup. Only the installed copy - the one next to config.yaml -
// does this, so running a build from anywhere else never repoints
// autostart at it.
func syncAutostart(enabled bool, logf func(format string, args ...any)) {
	exe, err := os.Executable()
	if err != nil {
		logf("WARNING locating own exe, leaving autostart alone: %v", err)
		return
	}
	cfgPath, err := config.Path()
	if err != nil {
		logf("WARNING locating config.yaml, leaving autostart alone: %v", err)
		return
	}
	if !strings.EqualFold(filepath.Dir(exe), filepath.Dir(cfgPath)) {
		logf("Not the installed copy (%s) - leaving autostart alone", exe)
		return
	}
	if err := autostart.Apply(enabled, exe); err != nil {
		logf("EXCEPTION updating autostart: %v", err)
		return
	}
	logf("Startup shortcut matches autostart=%t", enabled)
}
