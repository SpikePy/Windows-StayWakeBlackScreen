// Package config reads the user-editable config.yaml that lives next to
// the installed app's data, in the same per-user directory the installer
// uses (%LOCALAPPDATA%\StayWakeBlackScreen). It is created with default
// values the first time it's loaded, so the user always has a real file
// to edit rather than having to know the option names up front.
//
// The file is YAML-shaped so editors highlight it and it reads the way
// people expect, but only the handful of "key: value" lines this tool
// writes are understood - see parse. Three settings don't justify a YAML
// library.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Defaults written to a freshly created config.yaml, and also the
// fallback used if the file is missing, unreadable, or has an invalid
// value for the corresponding field.
const (
	DefaultIdleMinutes      = 3
	DefaultHeartbeatSeconds = 5
	DefaultStartEnabled     = true
	DefaultAutostart        = true
)

const fileName = "config.yaml"

const template = `# StayWakeBlackScreen configuration
#
# idle_minutes: minutes of inactivity (no keyboard/mouse input) before the
# screen blacks out. Can still be overridden per-run with -idle-minutes.
idle_minutes: %d

# heartbeat_seconds: how often (in seconds), while blacked out, the
# program toggles Caps Lock as a harmless "still alive" signal that keeps
# Windows from treating the session as idle. Can still be overridden
# per-run with -heartbeat-seconds.
heartbeat_seconds: %d

# start_enabled: whether the idle guard is active as soon as the program
# starts (true), or starts paused - no blackout, no sleep blocking - until
# enabled from the tray menu (false).
start_enabled: %t

` + autostartBlock

// autostartBlock ends the template, and SetAutostart appends it to a
// config.yaml written before the setting existed.
const autostartBlock = `# autostart: whether the idle guard starts in the background when you
# sign in to Windows (true), through a shortcut in your Startup folder, or
# not (false). Setup sets it to match what you chose to install. Applied
# the next time the program starts.
autostart: %t
`

// Config holds the settings read from config.yaml. Keys it doesn't know,
// such as the poll_ms that older versions wrote, are ignored.
type Config struct {
	IdleMinutes      int
	HeartbeatSeconds int
	StartEnabled     bool
	Autostart        bool
}

func defaults() Config {
	return Config{
		IdleMinutes:      DefaultIdleMinutes,
		HeartbeatSeconds: DefaultHeartbeatSeconds,
		StartEnabled:     DefaultStartEnabled,
		Autostart:        DefaultAutostart,
	}
}

// Load reads config.yaml, creating it with default values on first run.
// Any error, or an invalid value for a given field, falls back to that
// field's default rather than failing - a bad or missing config file
// should never stop the program from starting. A config.yaml written
// before a field existed (e.g. an old file with only idle_minutes) is
// treated the same as that field being absent: the field keeps its
// default rather than being reset to zero.
func Load() (Config, error) {
	def := defaults()

	dir, err := userDir()
	if err != nil {
		return def, err
	}
	path := filepath.Join(dir, fileName)

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		text := fmt.Sprintf(template, DefaultIdleMinutes, DefaultHeartbeatSeconds, DefaultStartEnabled, DefaultAutostart)
		if werr := os.WriteFile(path, []byte(text), 0o644); werr != nil {
			return def, fmt.Errorf("writing default config.yaml: %w", werr)
		}
		return def, nil
	}
	if err != nil {
		return def, fmt.Errorf("reading config.yaml: %w", err)
	}

	cfg := def
	if err := parse(data, &cfg); err != nil {
		return def, fmt.Errorf("parsing config.yaml: %w", err)
	}
	if cfg.IdleMinutes < 1 {
		cfg.IdleMinutes = DefaultIdleMinutes
	}
	if cfg.HeartbeatSeconds < 1 {
		cfg.HeartbeatSeconds = DefaultHeartbeatSeconds
	}
	return cfg, nil
}

// parse fills cfg from the file's "key: value" lines, leaving fields the
// file doesn't mention untouched. It understands blank lines, whole-line
// and trailing comments, optional quotes, and CRLF - everything a user
// editing this file in Notepad can produce. An unknown key is skipped so
// files from other versions still load; a value that isn't a number or a
// true/false is an error.
func parse(data []byte, cfg *Config) error {
	text := strings.TrimPrefix(string(data), "\ufeff") // Notepad writes a BOM
	for n, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return fmt.Errorf(`line %d: expected "key: value", got %q`, n+1, line)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if i := strings.Index(value, " #"); i >= 0 { // a trailing comment
			value = strings.TrimSpace(value[:i])
		}
		value = strings.Trim(value, `"'`)
		if value == "" { // "key:" with nothing after it: keep the default
			continue
		}

		var err error
		switch key {
		case "idle_minutes":
			cfg.IdleMinutes, err = strconv.Atoi(value)
		case "heartbeat_seconds":
			cfg.HeartbeatSeconds, err = strconv.Atoi(value)
		case "start_enabled":
			cfg.StartEnabled, err = parseBool(value)
		case "autostart":
			cfg.Autostart, err = parseBool(value)
		default:
			continue // a setting this version doesn't know
		}
		if err != nil {
			return fmt.Errorf("line %d: %s: %w", n+1, key, err)
		}
	}
	return nil
}

// parseBool accepts the spellings a hand-edited YAML-ish file may carry.
func parseBool(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "true", "yes", "on", "1":
		return true, nil
	case "false", "no", "off", "0":
		return false, nil
	}
	return false, fmt.Errorf("%q is not true or false", s)
}

// SetAutostart writes the autostart setting into config.yaml, creating
// the file with defaults first if there is none. Only the setting's own
// line changes; a file from before the setting existed gets it appended,
// with its comment.
func SetAutostart(enabled bool) error {
	path, err := Path()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		text := fmt.Sprintf(template, DefaultIdleMinutes, DefaultHeartbeatSeconds, DefaultStartEnabled, enabled)
		return os.WriteFile(path, []byte(text), 0o644)
	}
	if err != nil {
		return fmt.Errorf("reading config.yaml: %w", err)
	}
	return os.WriteFile(path, setAutostartIn(data, enabled), 0o644)
}

// autostartLine matches the setting itself, not a comment mentioning it.
var autostartLine = regexp.MustCompile(`(?m)^[ \t]*autostart[ \t]*:[^\r\n]*`)

// setAutostartIn returns data with its autostart line set to enabled,
// keeping the file's line endings, and appends the setting if data has
// none.
func setAutostartIn(data []byte, enabled bool) []byte {
	if autostartLine.Match(data) {
		return autostartLine.ReplaceAllLiteral(data, []byte(fmt.Sprintf("autostart: %t", enabled)))
	}
	nl := "\n"
	if strings.Contains(string(data), "\r\n") {
		nl = "\r\n"
	}
	block := strings.ReplaceAll(fmt.Sprintf(autostartBlock, enabled), "\n", nl)
	text := strings.TrimRight(string(data), "\r\n")
	if text == "" {
		return []byte(block)
	}
	return []byte(text + nl + nl + block)
}

// Path returns the config.yaml path, creating its containing directory
// if necessary. It does not create the file itself - see Load.
func Path() (string, error) {
	dir, err := userDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

func userDir() (string, error) {
	dir := os.Getenv("LOCALAPPDATA")
	if dir == "" {
		var err error
		dir, err = os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("resolving user config directory: %w", err)
		}
	}
	dir = filepath.Join(dir, "StayWakeBlackScreen")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating config directory: %w", err)
	}
	return dir, nil
}
