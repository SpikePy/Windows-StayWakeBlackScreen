//go:build windows

package blackout

import (
	"fmt"
	"syscall"
	"unsafe"

	"windows-stay-wake-black-screen/internal/hotkey"
	"windows-stay-wake-black-screen/internal/win32"
)

// WMHotkeyPressed arrives (with Hwnd 0) in the message loop of the thread
// that called WatchHotkey whenever the shortcut is pressed.
const WMHotkeyPressed = win32.WMHotkeyPressed

var (
	hotkeyHookHandle uintptr
	hotkeyWanted     hotkey.Hotkey
	// hotkeyUpPending: the shortcut's key-down was swallowed, so its
	// key-up must be too, or the focused window would get a stray one.
	hotkeyUpPending bool

	hotkeyHookCB = syscall.NewCallback(hotkeyHookProc)
)

// WatchHotkey installs a low-level keyboard hook that recognizes h
// system-wide and swallows it. A hook rather than RegisterHotKey,
// because Explorer already registers most Win+key shortcuts (Win+B
// included) and Windows refuses a second registration. Calling it again
// replaces the shortcut; a None shortcut removes the hook.
//
// The hook only looks at key-downs of h's key; every other key passes
// straight through, so the cost while typing is one comparison.
func WatchHotkey(h hotkey.Hotkey) error {
	hotkeyWanted = h
	if h.None() {
		StopHotkey()
		return nil
	}
	if hotkeyHookHandle != 0 {
		return nil
	}
	hh, _, err := procSetWindowsHookExW.Call(uintptr(whKeyboardLL), hotkeyHookCB, uintptr(win32.ModuleHandle()), 0)
	if hh == 0 {
		return fmt.Errorf("SetWindowsHookExW(WH_KEYBOARD_LL): %w", err)
	}
	hotkeyHookHandle = hh
	return nil
}

// StopHotkey removes the hook WatchHotkey installed, if any.
func StopHotkey() {
	if hotkeyHookHandle != 0 {
		procUnhookWindowsHookEx.Call(hotkeyHookHandle)
		hotkeyHookHandle = 0
	}
	hotkeyUpPending = false
}

func hotkeyHookProc(nCode int32, wParam, lParam uintptr) uintptr {
	if nCode >= 0 {
		data := (*kbdllhookstruct)(unsafe.Pointer(lParam))
		if data.VkCode == hotkeyWanted.VK && data.DwExtraInfo != ownInjectedMarker {
			switch wParam {
			case wmKeydown, wmSyskeydown:
				if heldModifiers() == hotkeyWanted.Mods {
					if hotkeyWanted.Mods&(hotkey.Win|hotkey.Alt) != 0 {
						maskModifierRelease()
					}
					hotkeyUpPending = true
					win32.PostMessage(0, WMHotkeyPressed, 0, 0)
					return 1
				}
				// The key went down on its own, so its key-up belongs to
				// this press, even if a blackout swallowed the last one.
				hotkeyUpPending = false
			case wmKeyup, wmSyskeyup:
				if hotkeyUpPending {
					hotkeyUpPending = false
					return 1
				}
			}
		}
	}
	r, _, _ := procCallNextHookEx.Call(hotkeyHookHandle, uintptr(nCode), wParam, lParam)
	return r
}

// heldModifiers returns which modifiers are down right now. Inside a
// low-level hook the async key state already includes every earlier
// event, so this is the state the shortcut's key was pressed in.
func heldModifiers() uint32 {
	var m uint32
	if keyDown(vkLWin) || keyDown(vkRWin) {
		m |= hotkey.Win
	}
	if keyDown(vkControl) {
		m |= hotkey.Ctrl
	}
	if keyDown(vkMenu) {
		m |= hotkey.Alt
	}
	if keyDown(vkShift) {
		m |= hotkey.Shift
	}
	return m
}

func keyDown(vk uint32) bool {
	r, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
	return r&0x8000 != 0
}

// maskModifierRelease taps an unassigned key while Win or Alt is still
// held. With the shortcut's own key swallowed, Windows would otherwise
// see Win (or Alt) pressed and released alone, and open the Start menu
// (or focus a menu bar) when it's let go. The tap is marked as ours, so
// a blackout's input block lets it through.
func maskModifierRelease() {
	marker := uintptr(ownInjectedMarker)
	procKeybdEvent.Call(uintptr(vkUnassigned), 0, 0, marker)
	procKeybdEvent.Call(uintptr(vkUnassigned), 0, uintptr(keyeventfKeyup), marker)
}
