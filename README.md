# Windows StayWakeBlackScreen

A Windows tool that keeps a PC awake without letting the screen show
anything — instead of turning the monitor off, it covers every screen
with a real black window and keeps telling Windows the display is
"required" (on). This avoids the session-lock-on-wake behavior that a real
monitor power-off can trigger (especially with "require sign-in on wake"
enabled).

Written in Go, calling the relevant Win32 APIs directly
(`golang.org/x/sys/windows`) — no .NET, no external GUI toolkit, no
runtime dependency beyond what Windows itself ships. The program and its
Setup are each a single self-contained `.exe`.

## Get it

Download `Setup_StayWakeBlackScreen.exe` from the
[Releases](../../releases) page and run it. Pick how you want to use
StayWakeBlackScreen:

- **Idle guard** — runs in the background with a tray icon and blacks out
  the screen after a few minutes without input. It starts again whenever
  you sign in.
- **Instant black screen** — adds StayWakeBlackScreen to the Start menu;
  opening it blacks out the screen right away.

Then click **Install/Update**. Run Setup again to update, to switch
between the two, or to **Uninstall**; **Close** changes nothing.
Everything is installed for your account only, in
`%LOCALAPPDATA%\StayWakeBlackScreen\`, and nothing needs administrator
rights.

## Using it

`StayWakeBlackScreen.exe` is one program with two modes:

| Started as | What it does |
| --- | --- |
| `StayWakeBlackScreen.exe` | Blacks out every screen and blocks all input **at once**. **Escape** ends it. If the idle guard is running, it asks the guard to black out instead. |
| `StayWakeBlackScreen.exe -background` | The **idle guard**: sits in the tray, keeps the PC awake, and blacks out the screen once you've been idle for `idle_minutes`. **Escape** ends the black screen; the guard keeps running. |

While the screen is black, **all** keyboard and mouse input is blocked
system-wide. **Escape** is the only key that ends it — and
**Ctrl+Alt+Del always works** as a hard fallback, because Windows never
lets any program suppress it.

## Tray icon

The idle guard shows a monitor glyph while enabled, and the same glyph
greyed out with a diagonal red strike while disabled. Hovering over it
shows the version and the current state.

- **Left-click** blacks out the screen at once (the same as Blackout).
- **Right-click** opens a menu: Blackout, Enable, Disable, Configure,
  Exit.

**Blackout** blacks out the screen at once, even while the guard is
disabled. Disabling restores input right away (if blacked out) and lets
Windows sleep and lock normally again, without stopping the program —
re-enable it any time from the same menu. **Configure** opens the
settings file in whatever application Windows uses for `.yaml` files.

## Settings

On first run it creates `%LOCALAPPDATA%\StayWakeBlackScreen\config.yaml`
(the real file has an explanatory comment above each setting):

```yaml
idle_minutes: 3        # idle time before the screen blacks out
heartbeat_seconds: 5   # how often Caps Lock is pulsed while blacked out
start_enabled: true    # false = start paused, enable it from the tray
autostart: true        # start the idle guard when you sign in
```

Edit a value and restart the program to apply it. Setup sets `autostart`
to match your choice. Every setting also has a matching command-line flag
that overrides the file for that run — see [DETAILS.md](DETAILS.md).

## Safety notes

- This tool intentionally blocks **all** keyboard and mouse input while
  the screen is black. Escape is the only key that ends it; Ctrl+Alt+Del
  is the Windows-guaranteed hard fallback if anything goes wrong.
- Not intended to bypass any organizational policy — use only on machines
  and in contexts where preventing idle-lock/sleep is something you're
  authorized to do.

## More

[DETAILS.md](DETAILS.md) covers every flag, Setup's options, how it works
internally, logging, building from source, and the package layout.

## License

MIT - see [LICENSE](LICENSE).
