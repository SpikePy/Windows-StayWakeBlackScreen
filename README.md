# Windows StayWakeBlackScreen

Windows tools that keep a PC awake without letting the screen show
anything — instead of turning the monitor off, they cover every screen
with a real black window and keep telling Windows the display is
"required" (on). This avoids the session-lock-on-wake behavior that a real
monitor power-off can trigger (especially with "require sign-in on wake"
enabled).

Written in Go, calling the relevant Win32 APIs directly
(`golang.org/x/sys/windows`) — no .NET, no external GUI toolkit, no
runtime dependency beyond what Windows itself ships. Each program is a
single self-contained `.exe`.

## Get it

Download `Setup_StayWakeBlackScreenIdle.exe` from the
[Releases](../../releases) page and run it. Its menu offers **Install /
update** and **Uninstall**; left alone for 5 seconds it installs or
updates on its own. Installing puts `StayWakeBlackScreenIdle.exe` into
`%LOCALAPPDATA%\StayWakeBlackScreen\`, starts it, and adds a shortcut to
your Startup folder so it comes back at login. Re-run it any time to
update. Nothing here needs administrator rights.

## The programs

| Program | What it does |
| --- | --- |
| `StayWakeBlackScreenIdle.exe` | The one you normally want. Sits in the tray, keeps the PC awake, and blacks out the screen once you've been idle for `idle_minutes` (default 3). **Escape** ends the blackout; the program keeps running and starts the countdown again. |
| `StayWakeBlackScreen.exe` | Blacks out every screen and blocks all input **immediately**, for when you want it right now. **Escape** ends it and exits. |
| `Setup_StayWakeBlackScreenIdle.exe` | Installs, updates or uninstalls the idle program. |

While blacked out, **all** keyboard and mouse input is blocked
system-wide. **Escape** is the only key that ends it — and
**Ctrl+Alt+Del always works** as a hard fallback, because Windows never
lets any program suppress it.

## Tray icon

A monitor glyph while enabled; the same glyph greyed out with a diagonal
red strike while disabled. Hovering over it shows the version and the
current state.

- **Left-click** toggles it between enabled and disabled.
- **Right-click** opens a menu: Enable, Disable, Configure, Exit.

Disabling restores input right away (if blacked out) and lets Windows
sleep and lock normally again, without stopping the program — re-enable
it any time from the same menu. **Configure** opens the settings file in
whatever application Windows uses for `.yaml` files.

## Settings

On first run it creates `%LOCALAPPDATA%\StayWakeBlackScreen\config.yaml`
(the real file has an explanatory comment above each setting):

```yaml
idle_minutes: 3        # idle time before the screen blacks out
heartbeat_seconds: 5   # how often Caps Lock is pulsed while blacked out
start_enabled: true    # false = start paused, enable it from the tray
```

Edit a value and restart the program to apply it. Every setting also has
a matching command-line flag that overrides the file for that run — see
[DETAILS.md](DETAILS.md).

## Safety notes

- These tools intentionally block **all** keyboard and mouse input while
  blacked out. Escape is the only key that ends it; Ctrl+Alt+Del is the
  Windows-guaranteed hard fallback if anything goes wrong.
- Not intended to bypass any organizational policy — use only on machines
  and in contexts where preventing idle-lock/sleep is something you're
  authorized to do.

## More

[DETAILS.md](DETAILS.md) covers every flag, how it works internally,
logging, building from source, and the package layout.

## License

MIT - see [LICENSE](LICENSE).
