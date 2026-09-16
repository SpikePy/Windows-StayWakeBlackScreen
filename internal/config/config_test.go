package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// useTempDir points Load and Path at a fresh directory and returns the
// config.yaml path they will use.
func useTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("LOCALAPPDATA", dir)
	path := filepath.Join(dir, "StayWakeBlackScreen", fileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadCreatesDefaultFile(t *testing.T) {
	path := useTempDir(t)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg != defaults() {
		t.Errorf("first Load = %+v, want defaults %+v", cfg, defaults())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("default config.yaml was not created: %v", err)
	}
	for _, line := range []string{"idle_minutes: 3", "heartbeat_seconds: 5", "start_enabled: true", "autostart: true"} {
		if !strings.Contains(string(data), line) {
			t.Errorf("created config.yaml is missing %q", line)
		}
	}

	again, err := Load()
	if err != nil || again != defaults() {
		t.Errorf("reloading the generated file = %+v, %v; want defaults, nil", again, err)
	}
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want Config
	}{
		{
			name: "file predating newer fields keeps their defaults",
			yaml: "idle_minutes: 7\n",
			want: Config{Autostart: true, IdleMinutes: 7, HeartbeatSeconds: 5, StartEnabled: true},
		},
		{
			name: "every field overridden",
			yaml: "idle_minutes: 10\nheartbeat_seconds: 2\nstart_enabled: false\n",
			want: Config{Autostart: true, IdleMinutes: 10, HeartbeatSeconds: 2, StartEnabled: false},
		},
		{
			name: "invalid values fall back per field",
			yaml: "idle_minutes: -1\nheartbeat_seconds: 0\nstart_enabled: false\n",
			want: Config{Autostart: true, IdleMinutes: 3, HeartbeatSeconds: 5, StartEnabled: false},
		},
		{
			name: "poll_ms from older versions is ignored",
			yaml: "idle_minutes: 4\npoll_ms: 100\n",
			want: Config{Autostart: true, IdleMinutes: 4, HeartbeatSeconds: 5, StartEnabled: true},
		},
		{
			name: "autostart can be switched off",
			yaml: "autostart: false\n",
			want: Config{IdleMinutes: 3, HeartbeatSeconds: 5, StartEnabled: true, Autostart: false},
		},
		{
			name: "comments, blank lines and odd spacing",
			yaml: "# header\n\n  idle_minutes:   7   # was 3\n\n# trailing note\nstart_enabled: false\n",
			want: Config{Autostart: true, IdleMinutes: 7, HeartbeatSeconds: 5, StartEnabled: false},
		},
		{
			name: "windows line endings and a Notepad byte order mark",
			yaml: "\ufeff# edited in Notepad\r\nidle_minutes: 9\r\nheartbeat_seconds: 2\r\n",
			want: Config{Autostart: true, IdleMinutes: 9, HeartbeatSeconds: 2, StartEnabled: true},
		},
		{
			name: "quoted values and yes/no booleans",
			yaml: "idle_minutes: \"8\"\nstart_enabled: no\n",
			want: Config{Autostart: true, IdleMinutes: 8, HeartbeatSeconds: 5, StartEnabled: false},
		},
		{
			name: "a key with no value keeps its default",
			yaml: "idle_minutes:\nheartbeat_seconds: 4\n",
			want: Config{Autostart: true, IdleMinutes: 3, HeartbeatSeconds: 4, StartEnabled: true},
		},
		{
			name: "the generated file itself round-trips",
			yaml: "# StayWakeBlackScreen configuration\n#\n# idle_minutes: minutes of inactivity\nidle_minutes: 3\n\nheartbeat_seconds: 5\n\nstart_enabled: true\n",
			want: Config{Autostart: true, IdleMinutes: 3, HeartbeatSeconds: 5, StartEnabled: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := useTempDir(t)
			if err := os.WriteFile(path, []byte(tt.yaml), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg != tt.want {
				t.Errorf("Load = %+v, want %+v", cfg, tt.want)
			}
		})
	}
}

func TestLoadRejectsBrokenFiles(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{"value that isn't a number", "idle_minutes: [not a number\n"},
		{"letters where a number belongs", "heartbeat_seconds: often\n"},
		{"value that isn't true or false", "start_enabled: maybe\n"},
		{"autostart that isn't true or false", "autostart: sometimes\n"},
		{"a line that isn't key: value", "idle_minutes 3\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := useTempDir(t)
			if err := os.WriteFile(path, []byte(tt.yaml), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load()
			if err == nil {
				t.Error("Load returned no error")
			}
			if cfg != defaults() {
				t.Errorf("Load = %+v, want defaults %+v", cfg, defaults())
			}
		})
	}
}

func TestPath(t *testing.T) {
	want := useTempDir(t)
	got, err := Path()
	if err != nil || got != want {
		t.Errorf("Path() = %q, %v; want %q, nil", got, err, want)
	}
}

func TestSetAutostart(t *testing.T) {
	tests := []struct {
		name, before, want string
		enabled            bool
	}{
		{
			name:    "flips an existing value and nothing else",
			before:  "# note\nidle_minutes: 4\nautostart: true\nstart_enabled: false\n",
			enabled: false,
			want:    "# note\nidle_minutes: 4\nautostart: false\nstart_enabled: false\n",
		},
		{
			name:    "keeps windows line endings",
			before:  "idle_minutes: 4\r\nautostart: false\r\n",
			enabled: true,
			want:    "idle_minutes: 4\r\nautostart: true\r\n",
		},
		{
			name:    "appends the setting to a file from before it existed",
			before:  "idle_minutes: 4\n",
			enabled: false,
			want:    "idle_minutes: 4\n\n" + fmt.Sprintf(autostartBlock, false),
		},
		{
			name:    "a comment mentioning autostart is not the setting",
			before:  "# autostart: an old note\nidle_minutes: 4\n",
			enabled: true,
			want:    "# autostart: an old note\nidle_minutes: 4\n\n" + fmt.Sprintf(autostartBlock, true),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := useTempDir(t)
			if err := os.WriteFile(path, []byte(tt.before), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := SetAutostart(tt.enabled); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != tt.want {
				t.Errorf("file is now\n%q\nwant\n%q", data, tt.want)
			}
			if cfg, err := Load(); err != nil || cfg.Autostart != tt.enabled {
				t.Errorf("Load after SetAutostart(%t) = %+v, %v", tt.enabled, cfg, err)
			}
		})
	}
}

func TestSetAutostartCreatesTheFile(t *testing.T) {
	useTempDir(t)
	if err := SetAutostart(false); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	want := defaults()
	want.Autostart = false
	if err != nil || cfg != want {
		t.Errorf("Load = %+v, %v; want %+v", cfg, err, want)
	}
}
