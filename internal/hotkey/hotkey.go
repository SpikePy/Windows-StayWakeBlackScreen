// Package hotkey parses the keyboard shortcut written in config.yaml,
// such as "win+b" or "ctrl+alt+f12", into the virtual-key code and
// modifiers the idle guard's keyboard hook matches against. It has no
// Windows dependency, so it is tested on any platform.
package hotkey

import (
	"fmt"
	"strconv"
	"strings"
)

// Modifier keys, combined as bit flags.
const (
	Win = 1 << iota
	Ctrl
	Alt
	Shift
)

// Hotkey is a parsed shortcut: one key plus the exact set of modifiers
// that must be held with it. The zero value means "no shortcut".
type Hotkey struct {
	Mods uint32
	VK   uint32 // Windows virtual-key code
}

// None reports whether h is the disabled shortcut.
func (h Hotkey) None() bool { return h.VK == 0 }

var modifierNames = map[string]uint32{
	"win": Win, "windows": Win,
	"ctrl": Ctrl, "control": Ctrl,
	"alt":   Alt,
	"shift": Shift,
}

// namedKeys are the non-letter, non-digit, non-F-key names accepted.
var namedKeys = map[string]uint32{
	"space": 0x20, "enter": 0x0D, "tab": 0x09, "backspace": 0x08,
	"insert": 0x2D, "delete": 0x2E, "home": 0x24, "end": 0x23,
	"pageup": 0x21, "pagedown": 0x22,
	"left": 0x25, "up": 0x26, "right": 0x27, "down": 0x28,
	"pause": 0x13, "scrolllock": 0x91, "printscreen": 0x2C,
}

// Parse reads a shortcut such as "win+b": any of win, ctrl, alt and
// shift, then exactly one key - a letter, a digit, f1 to f24, or one of
// a few named keys (space, enter, home, pause, ...) - joined by "+",
// in any letter case. "none", "off" and "false" mean no shortcut.
// Escape can't be the key: it already ends the black screen.
func Parse(s string) (Hotkey, error) {
	s = strings.ToLower(strings.ReplaceAll(s, " ", ""))
	switch s {
	case "none", "off", "false":
		return Hotkey{}, nil
	case "":
		return Hotkey{}, fmt.Errorf("empty shortcut")
	}
	var h Hotkey
	parts := strings.Split(s, "+")
	for _, p := range parts[:len(parts)-1] {
		m, ok := modifierNames[p]
		if !ok {
			return Hotkey{}, fmt.Errorf("%q in %q is not win, ctrl, alt or shift", p, s)
		}
		h.Mods |= m
	}
	vk, err := keyCode(parts[len(parts)-1])
	if err != nil {
		return Hotkey{}, fmt.Errorf("%q: %w", s, err)
	}
	h.VK = vk
	return h, nil
}

func keyCode(k string) (uint32, error) {
	switch {
	case len(k) == 1 && k[0] >= 'a' && k[0] <= 'z':
		return uint32(k[0]-'a') + 'A', nil
	case len(k) == 1 && k[0] >= '0' && k[0] <= '9':
		return uint32(k[0]), nil
	case len(k) > 1 && k[0] == 'f':
		if n, err := strconv.Atoi(k[1:]); err == nil && n >= 1 && n <= 24 {
			return 0x70 + uint32(n-1), nil // VK_F1..VK_F24
		}
	case k == "":
		return 0, fmt.Errorf("no key after the last +")
	case k == "esc" || k == "escape":
		return 0, fmt.Errorf("escape is reserved for ending the black screen")
	}
	if vk, ok := namedKeys[k]; ok {
		return vk, nil
	}
	if _, ok := modifierNames[k]; ok {
		return 0, fmt.Errorf("ends with a modifier, not a key")
	}
	return 0, fmt.Errorf("%q is not a key this setting knows", k)
}
