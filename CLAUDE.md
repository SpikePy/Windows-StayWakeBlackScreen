# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Windows-only Go tool (one self-contained exe plus a Setup exe) that keeps the PC awake behind black overlay windows instead of powering the monitor off. User-facing docs: `README.md` (overview) and `DETAILS.md` (flags, Setup, internals, build). Keep both in sync when behaviour, flags or settings change.

## Commands

Development happens in WSL; everything cross-compiles for Windows.

```sh
go test ./...                                   # OS-independent tests (config, icon, clamp/idle math, release parsing)
go test ./internal/config -run TestName         # single test
GOOS=windows GOARCH=amd64 go vet -unsafeptr=false ./...   # -unsafeptr=false is intentional (raw Win32 pointers), as in CI
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...    # compile check, same as ci.yml

GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-H=windowsgui -s -w -X main.version=dev" -o StayWakeBlackScreen.exe ./cmd/staywakeblackscreen
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-H=windowsgui -s -w -X main.version=dev" -o Setup_StayWakeBlackScreen.exe ./cmd/stay-wake-setup
```

Windows-only packages carry `//go:build windows`; their tests (e.g. `internal/shortcut/shelllink_test.go`) only run on Windows. If the monitor glyph (`internal/monitoricon`) or `setup.manifest` changes, regenerate the committed `rsrc_windows_amd64.syso` files with the commands in DETAILS.md → "Building from source"; a test fails if they drift.

## Release workflow (do this after every working change)

The user wants every working change shipped: commit, push to `main`, cut a new release, then install and start it on this PC. The `windows-go-tool` skill covers this flow.

1. Tests/vet/build pass, then commit and `git push`.
2. Tag the next semver (`git tag --sort=-v:refname | head -1` for the current one; patch for fixes, minor for features) and push the tag. `.github/workflows/build.yml` builds both exes from the tag (version stamped via `-X main.version=<tag>`) and publishes the GitHub Release; watch it with `gh run watch`.
3. Install locally once the release exists: download `Setup_StayWakeBlackScreen.exe` from the release (`gh release download <tag> -p Setup_StayWakeBlackScreen.exe`) into a Windows-side directory and run it from WSL with `-mode install`. Setup always installs the *latest* release, stops the running guard, replaces the exe in `%LOCALAPPDATA%\StayWakeBlackScreen\`, and starts the idle guard again (unless `-no-launch`). Never run the instant mode (no `-background`) as a test — it blacks out the screen and blocks all input until Escape.

## Architecture

- **One exe, two modes** (`cmd/staywakeblackscreen`): `main.go` parses `config.yaml` then flags (flags override the file for the whole run, including later reloads), syncs the Startup shortcut, and runs either `runInstant` (blackout now, exit on Escape) or `runBackground` (the idle guard in `guard.go`). An instant launch while the guard runs finds the guard's hidden tray window and posts it a "black out now" message instead of stacking a second blackout (`internal/singleinstance` + `tray`).
- **Single-threaded Win32 message loop.** `main` calls `runtime.LockOSThread()`; hooks, timers and windows all belong to that thread. `guard.run` pumps messages and handles thread messages (`WMTimer`, `WMEscapePressed`) itself; tray clicks and config-change notifications arrive as window messages dispatched by the same loop. Never introduce goroutines that touch Win32 state.
- **Nothing polls.** The guard arms one one-shot timer for when the idle threshold would be hit (`blackout.IdleTracker`, tick-wraparound safe) and re-arms after real input; Escape comes from the low-level keyboard hook; `config.Watch` (directory change notifications) triggers a reload via the tray window.
- **`internal/blackout`**: `Session` = one blackout (overlay window per monitor, re-fitted on display change, cursor hidden, `WH_KEYBOARD_LL`/`WH_MOUSE_LL` swallowing all input, Caps Lock heartbeat whose own synthetic key the hook lets through). Plus `SetThreadExecutionState` and the clamping of user values.
- **`internal/config`**: hand-rolled parser for a few `key: value` lines (no YAML library — keep it that way); creates the file with comments on first load, and can edit single values in place (Setup uses this for `autostart`).
- **Setup** (`cmd/stay-wake-setup` + `internal/setup`): `TaskDialogIndirect` dialog or `-mode install|uninstall` for scripts. Downloads via WinINet from `releases/latest/download/<asset>` and reads the version from the `releases/latest` redirect — deliberately never the GitHub API. Also cleans up pre-2.0 leftovers.
- **Autostart** is a Startup-folder shortcut (`internal/shortcut`, IShellLink via COM), never a registry entry; only the installed copy adjusts it on start.
- Only module dependency is `golang.org/x/sys`; the tray icon and exe icon are drawn from code (`internal/monitoricon`), no image assets.
