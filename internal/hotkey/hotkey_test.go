package hotkey

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		in   string
		want Hotkey
	}{
		{"win+b", Hotkey{Win, 'B'}},
		{"Win + B", Hotkey{Win, 'B'}},
		{"ctrl+alt+shift+f12", Hotkey{Ctrl | Alt | Shift, 0x7B}},
		{"control+windows+1", Hotkey{Ctrl | Win, '1'}},
		{"f24", Hotkey{0, 0x87}},
		{"alt+pause", Hotkey{Alt, 0x13}},
		{"none", Hotkey{}},
		{"OFF", Hotkey{}},
	}
	for _, tt := range tests {
		got, err := Parse(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("Parse(%q) = %+v, %v; want %+v, nil", tt.in, got, err, tt.want)
		}
	}
}

func TestParseRejects(t *testing.T) {
	for _, in := range []string{"", "win+", "win", "hyper+b", "win+bb", "f0", "f25", "ctrl+escape", "win+b+c"} {
		if h, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) = %+v, want an error", in, h)
		}
	}
}

func TestNone(t *testing.T) {
	if !(Hotkey{}).None() || (Hotkey{Win, 'B'}).None() {
		t.Error("None() should be true only for the zero Hotkey")
	}
}
