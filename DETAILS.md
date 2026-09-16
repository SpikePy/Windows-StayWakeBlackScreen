# Details

Full reference for Windows StayWakeBlackScreen. See [README.md](README.md)
for the overview and quick start.

## The program

`StayWakeBlackScreen.exe` has two modes, chosen by how it is started.

### Instant black screen

```
StayWakeBlackScreen.exe
StayWakeBlackScreen.exe -heartbeat-seconds 5 -enable-logging
```

Blacks out every screen and blocks **all** keyboard and mouse input
system-wide the moment it runs. **Escape** ends the black screen and the
program exits. This is what its Start menu entry does.

If the idle guard is already running, the program doesn't start a second
blackout: it asks the guard to black out now and exits, so there is only
ever one black screen and one Escape to press. The guard does this even
while it's disabled from its tray menu, and stays disabled afterwards.

### Idle guard

```
StayWakeBlackScreen.exe -background
StayWakeBlackScreen.exe -background -idle-minutes 3 -heartbeat-seconds 5 -enable-logging
```

Runs quietly in the background — no black screen, no input blocking, and
a tray icon — preventing sleep/lock, until the PC has been genuinely idle
(no real keyboard/mouse activity) for `idle_minutes` (default 3). At that
point it blacks out and blocks input exactly like the instant mode.
Pressing **Escape** dismisses the blackout and restores input, but the
guard keeps running and the idle countdown restarts — it will black out
again after another idle period, indefinitely. The Startup shortcut
starts it this way at sign-in.

The tray icon is a monitor glyph while enabled and the same glyph greyed
out with a red strike while disabled. Left-click toggles it; right-click
opens Blackout (the same as opening the program), Enable, Disable,
Configure (opens `config.yaml` in its default editor) and Exit. The guard doesn't exit on its own otherwise: use
*Exit*, Task Manager/`taskkill`, or Setup, which stops it when updating
or uninstalling.

### Flags

| Flag | Mode | Meaning |
| --- | --- | --- |
| `-background` | — | Run as the idle guard instead of blacking out at once. |
| `-idle-minutes N` | idle guard | Overrides `idle_minutes`. |
| `-heartbeat-seconds N` | both | Overrides `heartbeat_seconds`. |
| `-start-enabled=false` | idle guard | Overrides `start_enabled`. |
| `-autostart=false` | both | Overrides `autostart` for this run. |
| `-enable-logging` | both | Writes `StayWakeBlackScreen.log` next to the exe. |

## Setup

`Setup_StayWakeBlackScreen.exe` opens a small Windows dialog. Two radio
buttons choose how the program is used - preselected from your current
`config.yaml`, or the idle guard on a PC where it was never installed -
and three buttons act on that:

- **Install/Update** installs or updates the program for the chosen use:
  - *Idle guard*: turns the `autostart` setting on, adds the Startup
    shortcut (which passes `-background`), removes the Start menu entry
    and starts the guard.
  - *Instant black screen*: turns `autostart` off, removes the Startup
    shortcut and adds a Start menu entry that opens the program without
    arguments. It doesn't start the program - that would black out the
    screen in the middle of setup.
- **Uninstall** removes both shortcuts, stops the program and deletes
  `%LOCALAPPDATA%\StayWakeBlackScreen\`, including `config.yaml`.
- **Close** leaves everything as it is, and so do Escape and the title
  bar's X. Just opening Setup doesn't create or change anything.

If nothing is clicked within 5 seconds, Install/Update runs by itself
with the preselected use, so double-clicking Setup and walking away still
installs or updates; clicking anything, a radio button included, stops
that countdown for good. Setup then shows its progress and the result.
After a success it closes itself 5 seconds later (Close works at once);
after an error it stays open, so you can read what went wrong. Running
Setup again later updates the program or switches between the two uses.

Setup always installs the latest release - found through GitHub's plain
release links, not the GitHub API, so there's no API rate limit to run
into - stops any running copy before replacing the file, and clears out
what versions before 2.0 left behind: the separate
`StayWakeBlackScreenIdle.exe`, its Startup shortcut, and the registry
value even older versions used for autostart.

Everything is per-user, so nothing — install, autostart or uninstall —
needs administrator rights. Setup's manifest says so explicitly, which
also stops Windows from asking for elevation just because the file is
called "Setup".

For scripts, `-mode` runs one action without the dialog (and without its
countdowns) and prints its steps to the console it was started from (exit
code 1 on failure):

```
Setup_StayWakeBlackScreen.exe -mode background    (or -mode install)
Setup_StayWakeBlackScreen.exe -mode instant
Setup_StayWakeBlackScreen.exe -mode uninstall
```

Other flags: `-install-dir <path>` (override the install location),
`-no-launch` (don't start the idle guard after installing it),
`-no-autostart` (leave the `autostart` setting and Startup shortcut as
they are), `-keep-files` (uninstall: remove the shortcuts and stop the
program, but keep the files).

## Settings reference

The program creates `%LOCALAPPDATA%\StayWakeBlackScreen\config.yaml` on
first run:

```yaml
idle_minutes: 3
heartbeat_seconds: 5
start_enabled: true
autostart: true
```

| Option | Default | Description |
| --- | --- | --- |
| `idle_minutes` | `3` | Minutes of inactivity (no real keyboard/mouse input) before the idle guard blacks out the screen. |
| `heartbeat_seconds` | `5` | While blacked out, how often (seconds) the program toggles Caps Lock as a harmless "still alive" signal that keeps Windows from treating the session as idle. |
| `start_enabled` | `true` | Whether the idle guard is active as soon as it starts. Set to `false` to start paused — no sleep blocking, no blackout — until enabled from the tray menu. |
| `autostart` | `true` | Whether the idle guard starts in the background when you sign in, through a shortcut in your Startup folder. Setup sets it to match your choice, and the installed program adds or removes the shortcut to match every time it starts, so a change applies on the next start. |

Edit a value and restart the program to apply it. Each option also has a
matching command-line flag (see [Flags](#flags)) that overrides the file
for that run only. Config files from older versions may still contain a
`poll_ms` line; it's no longer used (nothing polls any more) and is
ignored.

## How it works

- `SetThreadExecutionState` with `ES_SYSTEM_REQUIRED | ES_DISPLAY_REQUIRED`
  tells Windows sleep and display-off must not happen.
- A borderless, topmost, black window is created per monitor instead of
  powering the display off, so Windows never sees a display-off/idle
  transition and has no reason to lock the session.
- Low-level `WH_KEYBOARD_LL` / `WH_MOUSE_LL` hooks swallow all keyboard and
  mouse input system-wide while blacked out — nothing reaches any window,
  including the tool's own. Only Escape is detected (via the hook) to end
  the blackout.
- A periodic synthetic Caps Lock toggle acts as an activity heartbeat some
  environments use to avoid idle/lock detection; the hook lets only this
  specific synthetic keystroke through so the Caps Lock LED actually
  updates, and the final state is cleaned up on exit.
- Nothing polls. The idle guard sets a single timer for the moment the
  idle threshold would be reached (re-arming it for the rest if there was
  input in the meantime), and the input hook wakes the program directly
  when Escape is pressed.
- Opening the program while the idle guard runs doesn't stack a second
  blackout: the new copy finds the guard's hidden tray window, posts it a
  request, and exits; the guard blacks out itself.
- Autostart is a shortcut in your own Startup folder that starts the
  program with `-background`, kept in line with the `autostart` setting:
  the installed program adds or removes it every time it starts (a copy
  run from anywhere else leaves it alone), and Setup does the same on
  install. No registry entry is involved.
- The tray icon is re-added by the program itself whenever Explorer
  restarts (the `TaskbarCreated` broadcast), which otherwise wipes every
  tray icon for good.
- Setup's window is a Windows task dialog (`TaskDialogIndirect`). It
  downloads through WinINet, Windows' own HTTP stack, so it uses your
  system proxy settings and Windows' certificate store, from GitHub's
  `releases/latest/download/<asset>` link; the version it shows comes from
  where `releases/latest` redirects. It never calls the GitHub API.
- **Ctrl+Alt+Del always remains available** — Windows never lets any hook
  suppress it — so it's a hard escape hatch no matter what else goes wrong.
- The tray icon is drawn at runtime (no image assets) as a 32×32
  alpha-blended `HICON` via `CreateDIBSection`/`CreateIconIndirect`.

## Logging

Off by default (nothing is written, no console output, no popups). Pass
`-enable-logging` to write diagnostics to `StayWakeBlackScreen.log` next
to the exe, for troubleshooting only.

## Building from source

Requires Go 1.26+ (matching the `go` directive in `go.mod`). The only
module it requires is `golang.org/x/sys`; config.yaml is parsed by a few
dozen lines in `internal/config` rather than a YAML library.

```
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-H=windowsgui -s -w" -o StayWakeBlackScreen.exe ./cmd/staywakeblackscreen
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-H=windowsgui -s -w" -o Setup_StayWakeBlackScreen.exe ./cmd/stay-wake-setup
```

Run the tests with `go test ./...`. They cover the OS-independent parts
(config loading and editing, the icon glyph and generator, and the timing
limits) and run on any platform; the shortcut code has additional
Windows-only tests.

Both programs show a version string — the tray tooltip and Setup's
footer — stamped in via `-X main.version=v1.2.3` appended to `-ldflags`
(the release build does this from the pushed tag); a build without it
shows `dev`.

`-H=windowsgui` means neither program opens a console window. Setup's
`-mode` output still appears when it's run from a terminal, because it
attaches to the console that started it.

Both `.exe` files carry the monitor glyph as their Explorer/taskbar icon
(the same one the idle guard draws at runtime for its tray icon),
embedded via a `rsrc_windows_amd64.syso` resource file committed in each
`cmd/` directory — `go build` picks these up automatically. Setup's also
embeds `cmd/stay-wake-setup/setup.manifest`, which its dialog needs. If
the glyph or the manifest changes, regenerate them with:

```
go run ./tools/genicon monitor.ico
go run github.com/akavel/rsrc@latest -ico monitor.ico -arch amd64 -o cmd/staywakeblackscreen/rsrc_windows_amd64.syso
go run github.com/akavel/rsrc@latest -manifest cmd/stay-wake-setup/setup.manifest -ico monitor.ico -arch amd64 -o cmd/stay-wake-setup/rsrc_windows_amd64.syso
```

A test fails if those committed icon resources no longer match what the
generator produces, so a changed glyph can't ship half-applied.

Package layout:

```
internal/blackout/        Win32 bindings: sleep/display block, input
                          hooks, overlay windows, DPI, idle detection
internal/tray/            Notification-area icon, menu, drawn icon
internal/win32/           Win32 declarations shared between packages
internal/monitoricon/     The monitor glyph's geometry, shared by the
                          tray icon and the generated .exe file icon
internal/singleinstance/  Named-mutex single-instance guard
internal/setup/           Install/uninstall, downloads through WinINet
internal/shortcut/        Startup and Start menu shortcuts
internal/config/          Loads, creates and edits config.yaml
internal/applog/          Opt-in diagnostics log next to the exe
tools/genicon/            Renders internal/monitoricon as a .ico file
cmd/staywakeblackscreen/  StayWakeBlackScreen.exe (both modes)
cmd/stay-wake-setup/      Setup_StayWakeBlackScreen.exe (dialog, -mode)
```

## Prebuilt releases and CI

The GitHub Actions workflow (`.github/workflows/build.yml`) tests and
cross-compiles both `.exe` files and publishes them to a
[GitHub Release](../../releases) whenever a `v*` tag is pushed (or the
workflow is triggered manually). Grab the latest from the
[Releases](../../releases) page, or just run
`Setup_StayWakeBlackScreen.exe`, which always installs the latest release.

Every push and pull request to `main` also runs `.github/workflows/ci.yml`:
the tests, vet, and a compile of both `.exe` files, without publishing
anything.

## Requirements

- Windows 10 or later (the input hooks and task dialogs are
  Windows-only, and Go itself needs Windows 10)
