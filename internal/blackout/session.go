//go:build windows

package blackout

import (
	"errors"
	"fmt"

	"windows-stay-wake-black-screen/internal/win32"
)

// Session is one active blackout: a black overlay window on every
// monitor, the cursor hidden, all keyboard and mouse input blocked, and
// the Caps Lock heartbeat running. Escape presses arrive in the starting
// thread's message loop as WMEscapePressed.
//
// A Session and its message loop must stay on the OS thread that started
// it (see runtime.LockOSThread): its hooks and timers belong to that
// thread.
type Session struct {
	windows      []uintptr
	cursorHidden bool
	hooked       bool
	heartbeat    *heartbeat
	refit        uintptr // one-shot timer for a pending re-fit; 0 if none
	logf         func(format string, args ...any)
}

// current is the running Session, if any, for the overlay windows'
// procedure to reach. There is only ever one, on the thread that
// started it.
var current *Session

// refitDelayMs lets a display change settle - Windows sends one
// WM_DISPLAYCHANGE to each overlay, sometimes more while a change
// is still under way - before the overlays are re-fitted once.
const refitDelayMs = 250

// Start blacks out every screen and blocks all input. heartbeatMs is the
// interval between Caps Lock pulses (see HeartbeatMs), and logf receives
// diagnostics. If Start fails partway, the returned Session holds
// whatever it had already set up, so callers must still call End.
func Start(heartbeatMs uint32, logf func(format string, args ...any)) (*Session, error) {
	s := &Session{logf: logf}
	current = s

	mons, err := monitors()
	if err != nil {
		return s, fmt.Errorf("enumerating monitors: %w", err)
	}
	if len(mons) == 0 {
		return s, errors.New("enumerating monitors: none found")
	}
	for _, m := range mons {
		hwnd, err := createOverlayWindow(m)
		if err != nil {
			return s, fmt.Errorf("creating overlay window for %+v: %w", m, err)
		}
		s.windows = append(s.windows, hwnd)
		logf("Created overlay window for bounds=%+v", m)
	}
	for _, h := range s.windows {
		procShowWindow.Call(h, swShow)
	}
	win32.SetForegroundWindow(s.windows[0])
	procShowCursor.Call(0)
	s.cursorHidden = true

	if err := installInputBlockHooks(); err != nil {
		return s, fmt.Errorf("installing input hooks: %w", err)
	}
	s.hooked = true

	if s.heartbeat, err = startHeartbeat(heartbeatMs); err != nil {
		return s, fmt.Errorf("starting heartbeat timer: %w", err)
	}
	return s, nil
}

// HandleTimer processes a WM_TIMER message's id if it belongs to s. Safe
// to call on a nil Session and with any id.
func (s *Session) HandleTimer(id uintptr) {
	if s == nil {
		return
	}
	if id != 0 && id == s.refit {
		StopTimer(s.refit)
		s.refit = 0
		s.refitWindows()
		return
	}
	s.heartbeat.handleTimer(id)
}

// SetHeartbeat restarts the Caps Lock heartbeat at a new interval (see
// HeartbeatMs). Safe to call on nil.
func (s *Session) SetHeartbeat(heartbeatMs uint32) error {
	if s == nil {
		return nil
	}
	s.heartbeat.stop()
	var err error
	s.heartbeat, err = startHeartbeat(heartbeatMs)
	return err
}

// scheduleRefit (re)starts the re-fit timer, so a burst of display
// change messages leads to a single re-fit. Safe to call on nil.
func (s *Session) scheduleRefit() {
	if s == nil || len(s.windows) == 0 {
		return
	}
	StopTimer(s.refit)
	s.refit = 0
	var err error
	if s.refit, err = StartTimer(refitDelayMs); err != nil {
		s.logf("EXCEPTION starting re-fit timer: %v", err)
		s.refitWindows() // better now than never
	}
}

// refitWindows makes the overlays match the screens as they are now: one
// per monitor, each covering it exactly. Existing windows are moved
// rather than recreated, so there is no flash of what's underneath.
func (s *Session) refitWindows() {
	mons, err := monitors()
	if err != nil || len(mons) == 0 {
		s.logf("EXCEPTION re-enumerating monitors (keeping the overlays as they are): %v", err)
		return
	}
	for i, m := range mons {
		if i < len(s.windows) {
			fitOverlayWindow(s.windows[i], m)
			s.logf("Re-fitted overlay window to bounds=%+v", m)
			continue
		}
		hwnd, err := createOverlayWindow(m)
		if err != nil {
			s.logf("EXCEPTION creating overlay window for %+v: %v", m, err)
			continue
		}
		s.windows = append(s.windows, hwnd)
		procShowWindow.Call(hwnd, swShow)
		s.logf("Created overlay window for bounds=%+v", m)
	}
	if len(s.windows) > len(mons) {
		for _, h := range s.windows[len(mons):] {
			win32.DestroyWindow(h)
		}
		s.windows = s.windows[:len(mons)]
	}
	win32.SetForegroundWindow(s.windows[0])
}

// End undoes Start, restoring input first so the user regains control of
// their keyboard and mouse even if anything after that goes wrong. It
// also switches Caps Lock off if it's on. Safe to call on a nil or
// partially started Session, and more than once.
func (s *Session) End() {
	if s == nil {
		return
	}
	if s.hooked {
		removeInputBlockHooks()
		s.hooked = false
	}
	if s.cursorHidden {
		procShowCursor.Call(1)
		s.cursorHidden = false
	}
	s.heartbeat.stop()
	s.heartbeat = nil
	StopTimer(s.refit)
	s.refit = 0
	if current == s {
		current = nil
	}
	for _, h := range s.windows {
		win32.DestroyWindow(h)
	}
	s.windows = nil
	if IsCapsLockOn() {
		ToggleCapsLock()
	}
}

// pulseMs is how long Caps Lock stays flipped during one heartbeat pulse.
const pulseMs = 150

// heartbeat periodically pulses Caps Lock - flip it, then flip it back
// pulseMs later - as an activity signal while the screen is blacked out.
// The gap is timed with a second timer rather than a sleep so the thread
// keeps pumping messages: the input hooks run on this same thread, and
// Windows silently removes a low-level hook that doesn't respond in time.
type heartbeat struct {
	every   uintptr // periodic timer
	restore uintptr // one-shot timer ending the pulse in flight; 0 if none
}

func startHeartbeat(intervalMs uint32) (*heartbeat, error) {
	every, err := StartTimer(intervalMs)
	if err != nil {
		return nil, err
	}
	return &heartbeat{every: every}, nil
}

func (h *heartbeat) handleTimer(id uintptr) {
	switch {
	case h == nil || id == 0:
	case id == h.every && h.restore == 0:
		ToggleCapsLock()
		var err error
		if h.restore, err = StartTimer(pulseMs); err != nil {
			ToggleCapsLock() // can't time the pulse, so end it now rather than leave Caps Lock flipped
		}
	case id == h.restore:
		h.endPulse()
	}
}

// endPulse flips Caps Lock back, finishing the pulse in flight, if any.
func (h *heartbeat) endPulse() {
	if h.restore != 0 {
		StopTimer(h.restore)
		h.restore = 0
		ToggleCapsLock()
	}
}

// stop stops the heartbeat, finishing any pulse in flight so Caps Lock is
// left as it was found. Safe to call on nil.
func (h *heartbeat) stop() {
	if h == nil {
		return
	}
	StopTimer(h.every)
	h.every = 0
	h.endPulse()
}
