//go:build windows

// Command staywakeblackscreen keeps a Windows PC awake behind a black
// screen, in one of two modes.
//
// Opened normally - double-clicked, or from its Start menu entry - it
// blacks out every screen at once and blocks ALL keyboard and mouse input
// system-wide through low-level WH_KEYBOARD_LL/WH_MOUSE_LL hooks, so
// nothing reaches any window. Escape ends the black screen and the program
// exits. If the background guard is already running, it asks the guard to
// black out instead, so there is only ever one black screen.
//
// Started with -background, as the Startup shortcut does, it is the idle
// guard: it runs quietly with a tray icon, prevents sleep and lock, and
// blacks out the same way only after idle_minutes without real input.
// Escape then ends the black screen, but the guard keeps running and the
// countdown restarts. The tray icon (monitor glyph = enabled, the same
// glyph greyed out with a red strike = disabled) blacks out on left-click;
// right-click opens Blackout, Enable, Disable, Configure and Exit;
// Configure opens config.yaml in its default editor, and the guard applies
// the file whenever it is saved.
//
// Windows never lets any hook suppress Ctrl+Alt+Del, so that always
// remains a hard way out while the screen is black.
//
// This does NOT power off the monitor. Turning a display off via
// SC_MONITORPOWER makes Windows treat that as an idle/wake transition and
// can lock the session (especially with "require sign-in on wake"
// enabled) even though SetThreadExecutionState is blocking sleep. Instead,
// every screen is covered with a real black window while the display is
// kept explicitly "required" (on), so Windows sees no display-off event
// and has no reason to lock.
//
// Settings come from config.yaml in %LOCALAPPDATA%\StayWakeBlackScreen,
// each overridable by a flag. Logging is off by default; -enable-logging
// writes diagnostics to StayWakeBlackScreen.log next to the exe.
//
// Built with -ldflags "-H=windowsgui" so it never shows a console window;
// each mode runs inside a recover() that never lets a panic surface as a
// crash dialog - diagnostics go only to the log file.
package main

import (
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"windows-stay-wake-black-screen/internal/applog"
	"windows-stay-wake-black-screen/internal/blackout"
	"windows-stay-wake-black-screen/internal/config"
	"windows-stay-wake-black-screen/internal/shortcut"
	"windows-stay-wake-black-screen/internal/singleinstance"
	"windows-stay-wake-black-screen/internal/tray"
)

// version is stamped in at build time via -ldflags "-X main.version=...";
// left as "dev" for local/manual builds.
var version = "dev"

type logFunc = func(format string, args ...any)

// settings are what the background guard applies while it runs, as
// config.yaml changes. start_enabled only matters at start, so it isn't
// one of them.
type settings struct {
	idleMinutes      int
	heartbeatSeconds int
	autostart        bool
}

func main() {
	// Win32 hooks, timers and the message queue are bound to the OS thread
	// that creates them; the Go runtime must never migrate this goroutine
	// to a different one mid-run.
	runtime.LockOSThread()

	cfg, cfgErr := config.Load()

	background := flag.Bool(strings.TrimPrefix(shortcut.BackgroundArg, "-"), false, "run as the background idle guard instead of blacking out the screen at once")
	idleMinutes := flag.Int("idle-minutes", cfg.IdleMinutes, "background: minutes of inactivity before blacking out (overrides config.yaml)")
	heartbeatSeconds := flag.Int("heartbeat-seconds", cfg.HeartbeatSeconds, "seconds between Caps Lock activity heartbeats while blacked out (overrides config.yaml)")
	startEnabled := flag.Bool("start-enabled", cfg.StartEnabled, "background: whether the idle guard is active on launch (overrides config.yaml)")
	autostartOn := flag.Bool("autostart", cfg.Autostart, "start the idle guard at sign-in through a Startup-folder shortcut; the installed copy applies this every time it starts (overrides config.yaml)")
	enableLogging := flag.Bool("enable-logging", false, "write diagnostics to StayWakeBlackScreen.log next to the exe")
	flag.Parse()

	logf, logPath := applog.New("StayWakeBlackScreen.log", *enableLogging)
	if cfgErr != nil {
		logf("WARNING loading config.yaml, using defaults: %v", cfgErr)
	}
	syncAutostart(*autostartOn, logf)

	if !*background {
		runInstant(blackout.HeartbeatMs(*heartbeatSeconds), logf, logPath)
		return
	}

	// A flag given on the command line keeps winning over config.yaml for
	// the whole run, also when the file changes.
	given := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { given[f.Name] = true })
	flagged := settings{*idleMinutes, *heartbeatSeconds, *autostartOn}
	reload := func() (settings, error) {
		cfg, err := config.Load()
		if err != nil {
			return settings{}, err
		}
		s := settings{cfg.IdleMinutes, cfg.HeartbeatSeconds, cfg.Autostart}
		if given["idle-minutes"] {
			s.idleMinutes = flagged.idleMinutes
		}
		if given["heartbeat-seconds"] {
			s.heartbeatSeconds = flagged.heartbeatSeconds
		}
		if given["autostart"] {
			s.autostart = flagged.autostart
		}
		return s, nil
	}
	runBackground(flagged, reload, *startEnabled, logf, logPath)
}

// runInstant blacks out the screen now and returns once Escape ends it.
func runInstant(heartbeatMs uint32, logf logFunc, logPath string) {
	// A background guard that is already running does the blackout
	// itself, so there's only ever one black screen and one Escape to press.
	if tray.RequestBlackout() {
		logf("Asked the running background guard to black out")
		return
	}

	release, alreadyRunning, err := singleinstance.Acquire(`StayWakeBlackScreen_Instant`)
	if err != nil {
		logf("EXCEPTION acquiring single-instance mutex: %v", err)
		return
	}
	if alreadyRunning {
		logf("A black screen is already showing - exiting.")
		return
	}
	defer release()

	var session *blackout.Session
	exitReason := "message loop returned without any handler logging a reason (unexpected)"
	defer func() {
		// Session.End restores input first, so the user regains control of
		// their keyboard and mouse as soon as possible no matter what else
		// might fail.
		session.End()
		blackout.RestoreExecutionState()
		logf("Cleanup done. Log at: %s", logPath)
	}()
	defer func() {
		if r := recover(); r != nil {
			logf("PANIC: %v", r)
		}
	}()

	blackout.EnableDPIAwareness()
	blackout.BlockSleep()

	// Blacks out every screen and blocks ALL keyboard/mouse input
	// system-wide - nothing reaches any window, including this one's.
	// Escape presses arrive below as WMEscapePressed.
	if session, err = blackout.Start(heartbeatMs, logf); err != nil {
		logf("EXCEPTION %v", err)
		return
	}

	logf("Entering message loop (instant black screen)")
	for {
		m, ok := blackout.GetMessage()
		if !ok {
			break
		}
		switch {
		case m.Hwnd == 0 && m.Message == blackout.WMTimer:
			session.HandleTimer(m.WParam)
		case m.Hwnd == 0 && m.Message == blackout.WMEscapePressed:
			exitReason = "Escape pressed"
			blackout.PostQuitMessage()
		default:
			blackout.Dispatch(m)
		}
	}
	logf("Message loop returned. Reason: %s", exitReason)
}

// runBackground runs the idle guard until Exit is chosen from its tray menu.
func runBackground(s settings, reload func() (settings, error), startEnabled bool, logf logFunc, logPath string) {
	release, alreadyRunning, err := singleinstance.Acquire(`StayWakeBlackScreen_Background`)
	if err != nil {
		logf("EXCEPTION acquiring single-instance mutex: %v", err)
		return
	}
	if alreadyRunning {
		logf("The background guard is already running - exiting.")
		return
	}
	defer release()

	g := newGuard(logf, s, reload, startEnabled)
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
	// Block sleep AND tell Windows the display must stay on for the whole
	// time the guard is enabled (not just during a blackout), so it never
	// sees a display-off/idle transition that could trigger a session lock.
	blackout.BlockSleep()
	if !g.enabled {
		logf("Starting disabled (start_enabled=false)")
		blackout.RestoreExecutionState()
	}

	if err := g.start(); err != nil {
		logf("EXCEPTION %v", err)
		return
	}

	logf("Entering message loop (background idle guard, idleMinutes=%d)", s.idleMinutes)
	if err := g.run(); err != nil {
		logf("EXCEPTION %v", err)
		return
	}
	logf("Message loop returned (Exit or unexpected shutdown).")
}

// syncAutostart keeps the Startup shortcut in line with the autostart
// setting, at every start and whenever config.yaml is saved, so an edit
// takes effect without re-running Setup. Only the installed copy - the one next to config.yaml -
// does this, so running a build from anywhere else never repoints
// autostart at it.
func syncAutostart(enabled bool, logf logFunc) {
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
	if err := shortcut.SyncAutostart(enabled, exe); err != nil {
		logf("EXCEPTION updating autostart: %v", err)
		return
	}
	logf("Startup shortcut matches autostart=%t", enabled)
}
