---
name: windows-go-tool
description: Build, extend or release a Windows desktop tool written in Go, following this project's blueprint - one self-contained exe, tray icon, autostart, YAML config in the user's profile, an install/uninstall setup program, automatic tests and GitHub Actions. Use when the user wants to start such a tool, add one of those pieces to an existing one, or says "cut a release", "release a patch", "ship it", or asks to publish a version and update their local install.
---

# Windows Go tool

Covers two jobs for Windows desktop tools written in Go: **building** one
(or adding a missing piece to one) and **releasing** one. Read the
blueprint, then follow the section that matches the request.

This repo, Windows-StayWakeBlackScreen, is the reference implementation:
when building a new tool, port its `internal/` packages rather than
writing Win32 plumbing from scratch.

## Blueprint

What every tool built this way has. Confirm the tool's name and purpose
with the user first, then keep all of this true.

- **Go only, Windows only.** Win32 called directly through
  `golang.org/x/sys/windows`; no CGO, no GUI toolkit, no .NET. Each
  program ships as one self-contained `.exe`, built for
  `GOOS=windows GOARCH=amd64` with `CGO_ENABLED=0`.
- **Build flags.** `-trimpath -ldflags "-H=windowsgui -s -w"` for GUI
  programs (no console window); drop `-H=windowsgui` for console ones
  like Setup. Stamp the version with `-X main.version=<tag>`, defaulting
  to `dev` in the source.
- **Tray icon** (for a background tool): drawn in code at runtime, no
  image files. Distinct enabled/disabled looks, tooltip carrying the
  version, left-click toggles, right-click menu with at least Configure
  and Exit. It **must re-add itself on the `TaskbarCreated` broadcast**,
  or the icon vanishes for good whenever Explorer restarts. The same
  glyph is embedded as each exe's file icon via a committed
  `rsrc_windows_amd64.syso` (generate with `tools/genicon` + `akavel/rsrc`).
- **Config file** at `%LOCALAPPDATA%\<Tool>\config.yaml`: created with
  defaults and an explanatory comment per setting on first run, unknown
  keys ignored (so older files keep working), an invalid value falls back
  to that field's default instead of failing, and every setting has a
  matching command-line flag that wins for that run. A tray "Configure"
  entry opens the file with its default editor.
- **Never needs administrator rights.** Everything is per-user: installed
  under `%LOCALAPPDATA%\<Tool>\`, autostart as a shortcut in the user's
  own Startup folder (`FOLDERID_Startup`, written with `IShellLink`),
  and a named mutex so a second copy refuses to start. Re-installing
  replaces that shortcut instead of adding another. Never use HKLM, the
  all-users Startup folder, a service or a scheduled task. One
  consequence to accept: while an elevated window has focus, Windows
  won't let a non-elevated program's hooks see or block its input.
- **Stay out of the registry.** Anything that has to persist belongs in
  a plain file in the user's profile - settings in
  `%LOCALAPPDATA%\<Tool>\config.yaml`, autostart as a Startup shortcut,
  any other state next to them. The user can see, edit and delete those,
  and uninstalling leaves nothing behind. Write to the registry only
  where Windows offers no file-based alternative, and then only under
  HKCU - or to remove a value an older version left there.
- **Setup program**, one exe that both installs and uninstalls:
  interactive menu when double-clicked, defaulting to Install/update if
  nothing is chosen within 5s, closing itself 3s after an unattended
  success but staying open on failure so the error is readable. `-mode
  install|uninstall` skips the menu for scripting. It downloads the
  latest GitHub release asset through **WinINet** (Windows' own HTTP
  stack: system proxy, system certificates, no Go TLS bloat) and stops
  any running copy before replacing the file.
- **Tests that run anywhere.** Keep decision logic (config loading,
  timing/limits, menu flow, geometry) in OS-independent packages with
  table tests, so `go test ./...` passes on Linux CI; Windows-only code
  is covered by vet and a cross-compile. Add guard tests for things that
  silently rot, e.g. that committed icon resources still match the
  generator.
- **Two workflows**: `ci.yml` on every push/PR to `main` (test, vet with
  `-unsafeptr=false` under `GOOS=windows`, build - publishing nothing)
  and `build.yml` on `v*` tags (same checks, then build every exe and
  publish a GitHub Release).
- **Docs always split in two.** `README.md` stays short and basic: what
  the tool is, how to install it, the programs, the handful of settings,
  safety notes - and a link to the details. Everything long lives in
  `DETAILS.md`: the full flag reference, how it works internally,
  logging, building from source, the package layout and CI, with a link
  back to the README. Never let the README grow into the reference; when
  it starts to, move that part across and link it. Plus a `LICENSE` (MIT
  unless the user says otherwise).
- **Logging off by default**, enabled with `-enable-logging`, written
  next to the exe.

### Layout

```
cmd/<tool>/            the GUI program
cmd/<tool>-setup/      install/uninstall program
internal/config/       config.yaml loading, defaults, per-field fallback
internal/tray/         tray icon, menu, runtime-drawn icon
internal/win32/        Win32 declarations shared between packages
internal/setup/        install/uninstall + WinINet download
internal/setupmenu/    setup's console menu (OS-independent, tested)
internal/singleinstance/ named-mutex guard
internal/applog/       opt-in log file next to the exe
tools/genicon/         renders the icon glyph to .ico
```

## Building a new tool

1. **Ask** for the tool's name and what it does, unless the user already
   said. Derive exe names (`<Tool>.exe`, `Setup_<Tool>.exe`), the module
   path and `%LOCALAPPDATA%\<Tool>\`.
2. **Port the packages** listed above from this repo, renaming identifiers
   and registry/mutex/class names. Don't re-derive Win32 bindings.
3. **Write the tool's own logic** in `cmd/<tool>`, keeping anything
   testable in an OS-independent package.
4. **Wire the pieces**: config + flags, tray icon with Configure/Exit,
   autostart through Setup, single instance, opt-in logging.
5. **Tests** for every OS-independent package, then `go test ./...`,
   `gofmt -l .`, and a `GOOS=windows` vet and build.
6. **Add both workflows and the docs**, then cut the first release
   (`v0.0.1`) with the Releasing section below.

## Releasing

**Argument:** `patch` (default), `minor` or `major`. "and install" also
runs steps 6-7.

1. **Check the working tree.** `git status --short`. If there's nothing to
   commit and `HEAD` is already tagged, say so and stop.
2. **Verify.** Go lives at `/usr/local/go/bin` on this machine:
   ```sh
   export PATH=$PATH:/usr/local/go/bin
   gofmt -l .                                        # must print nothing
   go test ./...
   GOOS=windows GOARCH=amd64 go vet -unsafeptr=false ./...
   GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...
   ```
   Any failure stops the release - report it instead of pushing.
3. **Commit** what's uncommitted, describing the change and why (not a
   file list). Never commit secrets.
4. **Tag and push.** Next version = highest existing tag
   (`git tag --sort=-v:refname | head -1`) with the requested part bumped:
   ```sh
   git push origin main
   git tag -a vX.Y.Z -m "vX.Y.Z - <summary>

   - <user-visible change>"
   git push origin vX.Y.Z
   ```
5. **Watch CI** and report a failure with its failing step:
   ```sh
   id=$(gh run list --workflow build.yml --branch vX.Y.Z --limit 1 --json databaseId -q '.[0].databaseId')
   gh run watch "$id" --exit-status
   gh release view vX.Y.Z --json assets -q '.assets[].name'
   ```
6. **Install locally** (only if asked). This machine is WSL, so Windows
   programs run through `/mnt/c`:
   ```sh
   T=/mnt/c/Users/$USER/AppData/Local/Temp
   gh release download vX.Y.Z -p Setup_<Tool>.exe -D "$T" --clobber
   (cd "$T" && ./Setup_<Tool>.exe -mode install)
   rm -f "$T/Setup_<Tool>.exe"
   ```
7. **Verify the install** and report each result: the version string
   inside the installed exe, exactly one running copy
   (`tasklist.exe /FI "IMAGENAME eq <Tool>.exe"`), the autostart shortcut
   (`ls "/mnt/c/Users/$USER/AppData/Roaming/Microsoft/Windows/Start Menu/Programs/Startup/<Tool>.lnk"`),
   and the tray icon via UI Automation (its tooltip carries the version):
   ```sh
   powershell.exe -NoProfile -Command 'Add-Type -AssemblyName UIAutomationClient, UIAutomationTypes; $A=[System.Windows.Automation.AutomationElement]; @($A::RootElement.FindAll([System.Windows.Automation.TreeScope]::Descendants, (New-Object System.Windows.Automation.PropertyCondition($A::NameProperty, "<tooltip incl. version>")))).Count'
   ```
8. **Report** the release URL and, if installed, the version now running.

## Rules

- Never force-push, never skip hooks, never retag an existing version.
- Stop at the first failing step and say what broke; don't bypass checks
  to make it pass.
- Anything that blocks the user's input or blacks out their screen needs
  their go-ahead before you run it live, and always keep Escape plus
  Ctrl+Alt+Del working as a way out.
- Pipe Windows output through `tr -d '\r'` to keep it readable from WSL.
- Don't launch a Windows GUI program with `cmd.exe /c start` from a
  command whose output you read - it inherits the pipe and the command
  hangs. Use `powershell.exe -NoProfile -Command "Start-Process ..."`.
