// Package config manages persistent settings and Windows startup registration for WhatsMySpeed.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const AppName = "WhatsMySpeed"

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

// Config holds persisted user preferences.
type Config struct {
	DisplayMode DisplayMode `json:"display_mode"`
	SpeedUnit   SpeedUnit   `json:"speed_unit"`
}

// Default returns the default configuration.
func Default() Config {
	return Config{
		DisplayMode: ModeBoth,
		SpeedUnit:   UnitBytes,
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

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default(), fmt.Errorf("parse config %q: %w", path, err)
	}
	if !ValidDisplayMode(cfg.DisplayMode) {
		cfg.DisplayMode = ModeBoth
	}
	if !ValidSpeedUnit(cfg.SpeedUnit) {
		cfg.SpeedUnit = UnitBytes
	}
	return cfg, nil
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
	if !ValidDisplayMode(cfg.DisplayMode) {
		cfg.DisplayMode = ModeBoth
	}
	if !ValidSpeedUnit(cfg.SpeedUnit) {
		cfg.SpeedUnit = UnitBytes
	}

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

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace config %q: %w", path, err)
	}
	cleanup = false
	return nil
}
