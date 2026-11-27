// Package config manages persistent settings and Windows startup registration for WhatsMySpeed.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	AppName   = "WhatsMySpeed"
	AppAuthor = "Shubham Bhamare"
)

// DisplayMode controls how speeds are shown on the Windows 11 taskbar.
type DisplayMode string

const (
	ModeBoth     DisplayMode = "both"
	ModeDownload DisplayMode = "download"
	ModeUpload   DisplayMode = "upload"
	ModeHide     DisplayMode = "hide"
)

// SpeedUnit controls whether rates are displayed in Bytes/s or Bits/s.
type SpeedUnit string

const (
	UnitBytes SpeedUnit = "bytes"
	UnitBits  SpeedUnit = "bits"
)

// LabelStyle controls how upload and download indicators are rendered on the taskbar.
type LabelStyle string

const (
	LabelArrows LabelStyle = "arrows" // Phosphor up/down arrow icons
	LabelUD     LabelStyle = "ud"     // Compact U / D letters
	LabelWords  LabelStyle = "words"  // Full Up / Down labels
)

// OverlayLayout controls how speeds are arranged inside the taskbar overlay.
type OverlayLayout string

const (
	LayoutStackedUp   OverlayLayout = "stacked_up"   // 2 rows: Upload on top, Download on bottom
	LayoutStackedDown OverlayLayout = "stacked_down" // 2 rows: Download on top, Upload on bottom
	LayoutInline      OverlayLayout = "inline"       // 1 row: side-by-side
	LayoutCompact     OverlayLayout = "compact"      // 2 rows: tighter spacing & compact font
)

// ThemeMode controls the WhatsMySpeed window theme preference.
type ThemeMode string

const (
	ThemeSystem ThemeMode = "system"
	ThemeDark   ThemeMode = "dark"
	ThemeLight  ThemeMode = "light"
)

// Config holds persisted user preferences.
type Config struct {
	DisplayMode      DisplayMode   `json:"display_mode"`
	SpeedUnit        SpeedUnit     `json:"speed_unit"`
	LabelStyle       LabelStyle    `json:"label_style"`
	Layout           OverlayLayout `json:"layout"`
	Theme            ThemeMode     `json:"theme"`
	StartWithWindows bool          `json:"start_with_windows"`
}

// Default returns the default configuration.
func Default() Config {
	return Config{
		DisplayMode:      ModeBoth,
		SpeedUnit:        UnitBytes,
		LabelStyle:       LabelArrows,
		Layout:           LayoutStackedUp,
		Theme:            ThemeSystem,
		StartWithWindows: true,
	}
}

// ValidDisplayMode reports whether mode is one of the supported display modes.
func ValidDisplayMode(mode DisplayMode) bool {
	switch mode {
	case ModeBoth, ModeDownload, ModeUpload, ModeHide:
		return true
	default:
		return false
	}
}

// ValidSpeedUnit reports whether unit is one of the supported speed unit modes.
func ValidSpeedUnit(unit SpeedUnit) bool {
	switch unit {
	case UnitBytes, UnitBits:
		return true
	default:
		return false
	}
}

// ValidLabelStyle reports whether style is one of the supported direction label styles.
func ValidLabelStyle(style LabelStyle) bool {
	switch style {
	case LabelArrows, LabelUD, LabelWords:
		return true
	default:
		return false
	}
}

// ValidLayout reports whether layout is one of the supported taskbar overlay layouts.
func ValidLayout(layout OverlayLayout) bool {
	switch layout {
	case LayoutStackedUp, LayoutStackedDown, LayoutInline, LayoutCompact:
		return true
	default:
		return false
	}
}

// ValidThemeMode reports whether theme is one of the supported window themes.
func ValidThemeMode(theme ThemeMode) bool {
	switch theme {
	case ThemeSystem, ThemeDark, ThemeLight:
		return true
	default:
		return false
	}
}

func normalizeConfig(cfg Config) Config {
	if !ValidDisplayMode(cfg.DisplayMode) {
		cfg.DisplayMode = ModeBoth
	}
	if !ValidSpeedUnit(cfg.SpeedUnit) {
		cfg.SpeedUnit = UnitBytes
	}
	if !ValidLabelStyle(cfg.LabelStyle) {
		cfg.LabelStyle = LabelArrows
	}
	if !ValidLayout(cfg.Layout) {
		cfg.Layout = LayoutStackedUp
	}
	if !ValidThemeMode(cfg.Theme) {
		cfg.Theme = ThemeSystem
	}
	return cfg
}

// ConfigPath returns the JSON config file path inside %APPDATA%\WhatsMySpeed\config.json.
func ConfigPath() (string, error) {
	baseDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(baseDir, AppName, "config.json"), nil
}

// Load reads the persisted configuration from the default path, falling back to Default()
// if the file does not exist or contains invalid data.
func Load() (Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return Default(), err
	}
	return LoadFrom(path)
}

// LoadFrom reads the configuration from path, returning Default() when the file is missing.
func LoadFrom(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Default(), nil
		}
		return Default(), fmt.Errorf("read config %q: %w", path, err)
	}

	cfg := Default()
	cleanData := bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if err := json.Unmarshal(cleanData, &cfg); err != nil {
		return Default(), fmt.Errorf("parse config %q: %w", path, err)
	}
	return normalizeConfig(cfg), nil
}

// Save writes cfg to the default %APPDATA%\WhatsMySpeed\config.json path.
func Save(cfg Config) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	return SaveTo(path, cfg)
}

// SaveTo atomically writes cfg as indented JSON to path.
func SaveTo(path string, cfg Config) error {
	cfg = normalizeConfig(cfg)

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir %q: %w", dir, err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	data = append(data, '\n')

	tmpFile, err := os.CreateTemp(dir, "config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpPath := tmpFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}

	var renameErr error
	for attempt := 0; attempt < 8; attempt++ {
		renameErr = os.Rename(tmpPath, path)
		if renameErr == nil {
			cleanup = false
			return nil
		}
		time.Sleep(time.Duration(attempt+1) * 15 * time.Millisecond)
	}
	return fmt.Errorf("replace config %q: %w", path, renameErr)
}

var (
	queryAutoStart = IsAutoStartEnabled
	applyAutoStart = SetAutoStart
)

func hasAllConfigKeys(raw map[string]json.RawMessage) bool {
	if raw == nil {
		return false
	}
	for _, k := range []string{"display_mode", "speed_unit", "label_style", "layout", "theme", "start_with_windows"} {
		if _, ok := raw[k]; !ok {
			return false
		}
	}
	return true
}

// LoadAndSyncStartup loads preferences from path (or the default %APPDATA%\WhatsMySpeed\config.json
// when empty), creates/upgrades missing config fields while preserving any pre-existing Windows Run
// entry on upgrade, and keeps HKCU\...\Run synchronized with start_with_windows.
func LoadAndSyncStartup(path string) (Config, error) {
	if path == "" {
		resolved, err := ConfigPath()
		if err != nil {
			cfg := Default()
			if enabled, rErr := queryAutoStart(); rErr == nil {
				cfg.StartWithWindows = enabled
			}
			return cfg, err
		}
		path = resolved
	}

	data, readErr := os.ReadFile(path)
	if readErr != nil {
		cfg := Default()
		if errors.Is(readErr, os.ErrNotExist) {
			if sErr := applyAutoStart(cfg.StartWithWindows); sErr != nil {
				if enabled, rErr := queryAutoStart(); rErr == nil {
					cfg.StartWithWindows = enabled
				}
			}
			_ = SaveTo(path, cfg)
			return cfg, nil
		}
		if enabled, rErr := queryAutoStart(); rErr == nil {
			cfg.StartWithWindows = enabled
		}
		return cfg, fmt.Errorf("read config %q: %w", path, readErr)
	}

	parsed := Default()
	cleanData := bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if err := json.Unmarshal(cleanData, &parsed); err != nil {
		fallback := Default()
		if enabled, rErr := queryAutoStart(); rErr == nil {
			fallback.StartWithWindows = enabled
		}
		return fallback, fmt.Errorf("parse config %q: %w", path, err)
	}

	cfg := normalizeConfig(parsed)

	var raw map[string]json.RawMessage
	_ = json.Unmarshal(cleanData, &raw)
	_, hasAutoStartKey := raw["start_with_windows"]

	if !hasAutoStartKey {
		if sErr := applyAutoStart(cfg.StartWithWindows); sErr != nil {
			if enabled, rErr := queryAutoStart(); rErr == nil {
				cfg.StartWithWindows = enabled
			}
		}
		_ = SaveTo(path, cfg)
		return cfg, nil
	}

	regEnabled, rErr := queryAutoStart()
	if rErr != nil || regEnabled != cfg.StartWithWindows || (!IsPackaged() && cfg.StartWithWindows) {
		if sErr := applyAutoStart(cfg.StartWithWindows); sErr != nil && rErr == nil {
			cfg.StartWithWindows = regEnabled
		}
	}

	if !hasAllConfigKeys(raw) || cfg != parsed {
		_ = SaveTo(path, cfg)
	}
	return cfg, nil
}
