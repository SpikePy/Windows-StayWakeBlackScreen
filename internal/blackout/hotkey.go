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
	// modsDown tracks which modifier keys are held, one bit per key
	// (see modifierKeys), from the key events the hook sees.
	modsDown uint8

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
	modsDown = 0
	for i, k := range modifierKeys {
		if keyDown(k.vk) {
			modsDown |= 1 << i
		}
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
	modsDown = 0
}

func hotkeyHookProc(nCode int32, wParam, lParam uintptr) uintptr {
	if nCode >= 0 {
		data := (*kbdllhookstruct)(unsafe.Pointer(lParam))
		trackModifier(data.VkCode, wParam)
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

// modifierKeys are the left/right-specific keys a low-level hook reports
// for the modifiers.
var modifierKeys = [...]struct{ vk, mod uint32 }{
	{vkLWin, hotkey.Win}, {vkRWin, hotkey.Win},
	{vkLControl, hotkey.Ctrl}, {vkRControl, hotkey.Ctrl},
	{vkLMenu, hotkey.Alt}, {vkRMenu, hotkey.Alt},
	{vkLShift, hotkey.Shift}, {vkRShift, hotkey.Shift},
}

// trackModifier updates modsDown for one key event.
func trackModifier(vk uint32, wParam uintptr) {
	for i, k := range modifierKeys {
		if k.vk != vk {
			continue
		}
		switch wParam {
		case wmKeydown, wmSyskeydown:
			modsDown |= 1 << i
		case wmKeyup, wmSyskeyup:
			modsDown &^= 1 << i
		}
		return
	}
}

// heldModifiers returns which modifiers are down, as tracked from the
// events the hook saw. Not GetAsyncKeyState: inside the hook it reports
// a held Win key only while Explorer has the foreground - in a browser or
// terminal it claims none is, and Windows' own Win+B ran instead.
func heldModifiers() uint32 {
	var m uint32
	for i, k := range modifierKeys {
		if modsDown&(1<<i) != 0 {
			m |= k.mod
		}
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
