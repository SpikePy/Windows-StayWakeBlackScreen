//go:build windows

// Command stay-wake-setup installs, updates and uninstalls
// StayWakeBlackScreen. Opened normally it shows a small Windows dialog
// with the three buttons (see dialog.go); -mode runs one of them directly
// for scripts, printing its steps to the console it was started from.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows"

	"windows-stay-wake-black-screen/internal/setup"
)

// version is stamped in at build time via -ldflags "-X main.version=...";
// left as "dev" for local/manual builds.
var version = "dev"

// options are the flags that shape what an action does.
type options struct {
	installDir                                  string
	autostart, launch, keepAutostart, keepFiles bool
}

func main() {
	mode := flag.String("mode", "", "run without the dialog: install (or background), instant, or uninstall")
	installDir := flag.String("install-dir", "", "directory to install into/remove from (default: %LOCALAPPDATA%\\StayWakeBlackScreen)")
	autostart := flag.Bool("autostart", true, "install: start the idle guard at every sign-in, through a Startup-folder shortcut")
	noLaunch := flag.Bool("no-launch", false, "install: don't start the idle guard now")
	noAutostart := flag.Bool("no-autostart", false, "install: leave the autostart setting and Startup shortcut as they are, ignoring -autostart")
	keepFiles := flag.Bool("keep-files", false, "remove the shortcuts and stop the program, but don't delete the installed files (uninstall only)")
	flag.Parse()

	o := options{*installDir, *autostart, !*noLaunch, *noAutostart, *keepFiles}
	if *mode == "" {
		os.Exit(runDialog(o))
	}

	attachConsole()
	if err := run(strings.ToLower(*mode), o, func(step string) { fmt.Println(step) }); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// run performs one Setup action, reporting each step to progress.
func run(action string, o options, progress func(string)) error {
	install := func() error {
		return setup.Install(setup.InstallOptions{
			InstallDir:    o.installDir,
			Autostart:     o.autostart,
			KeepAutostart: o.keepAutostart,
			Launch:        o.launch,
			Progress:      progress,
		})
	}
	switch action {
	case "install", "background":
		return install()
	case "instant":
		// Only the Start menu entry: no idle guard, now or at sign-in.
		o.autostart, o.launch = false, false
		return install()
	case "uninstall":
		return setup.Uninstall(setup.UninstallOptions{
			InstallDir: o.installDir,
			KeepFiles:  o.keepFiles,
			Progress:   progress,
		})
	}
	return fmt.Errorf("unknown -mode %q (want install, instant or uninstall)", action)
}

var (
	modKernel32       = windows.NewLazySystemDLL("kernel32.dll")
	procAttachConsole = modKernel32.NewProc("AttachConsole")
)

// attachConsole makes -mode's output visible. Setup is a GUI program, so
// Windows gives it no console of its own; unless its output is already
// going to a pipe or file, it borrows the console of whatever started it.
func attachConsole() {
	if h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE); err == nil && h != 0 && h != windows.InvalidHandle {
		return
	}
	const attachParentProcess = uintptr(^uint32(0))
	if r, _, _ := procAttachConsole.Call(attachParentProcess); r == 0 {
		return
	}
	if out, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout, os.Stderr = out, out
	}
}
