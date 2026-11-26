package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFromMissingReturnsDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "config.json")
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom missing returned error: %v", err)
	}
	if cfg.DisplayMode != ModeBoth {
		t.Fatalf("expected ModeBoth, got %q", cfg.DisplayMode)
	}
}

func TestSaveToAndLoadFromRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "WhatsMySpeed", "config.json")
	modes := []DisplayMode{ModeBoth, ModeDownload, ModeUpload, ModeHide}
	units := []SpeedUnit{UnitBytes, UnitBits}
	labels := []LabelStyle{LabelArrows, LabelUD, LabelWords}
	layouts := []OverlayLayout{LayoutStackedUp, LayoutStackedDown, LayoutInline, LayoutCompact}
	themes := []ThemeMode{ThemeSystem, ThemeDark, ThemeLight}
	autoStarts := []bool{false, true}

	for _, mode := range modes {
		for _, unit := range units {
			for _, label := range labels {
				for _, layout := range layouts {
					for _, theme := range themes {
						for _, autoStart := range autoStarts {
							want := Config{
								DisplayMode:      mode,
								SpeedUnit:        unit,
								LabelStyle:       label,
								Layout:           layout,
								Theme:            theme,
								StartWithWindows: autoStart,
							}
							if err := SaveTo(path, want); err != nil {
								t.Fatalf("SaveTo(%+v) failed: %v", want, err)
							}
							got, err := LoadFrom(path)
							if err != nil {
								t.Fatalf("LoadFrom(%+v) failed: %v", want, err)
							}
							if got != want {
								t.Fatalf("config mismatch: got %+v, want %+v", got, want)
							}
						}
					}
				}
			}
		}
	}
}

func TestLoadFromLegacyConfigDefaultsNewFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"display_mode":"download"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DisplayMode != ModeDownload || cfg.SpeedUnit != UnitBytes || cfg.LabelStyle != LabelArrows || cfg.Layout != LayoutStackedUp || cfg.Theme != ThemeSystem || !cfg.StartWithWindows {
		t.Fatalf("unexpected config %+v", cfg)
	}
}

func TestLoadFromInvalidModeFallsBackToBoth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"display_mode":"bogus"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DisplayMode != ModeBoth {
		t.Fatalf("expected fallback ModeBoth, got %q", cfg.DisplayMode)
	}
}

func TestLoadFromCorruptJSONReturnsDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{not-json`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err == nil {
		t.Fatal("expected error for corrupt JSON, got nil")
	}
	if cfg.DisplayMode != ModeBoth {
		t.Fatalf("expected fallback ModeBoth, got %q", cfg.DisplayMode)
	}
}

func TestConfigPathUsesAppName(t *testing.T) {
	p, err := ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath failed: %v", err)
	}
	if !strings.HasSuffix(p, filepath.Join(AppName, "config.json")) {
		t.Fatalf("unexpected config path %q", p)
	}
}

func TestFormatAutoStartCommand(t *testing.T) {
	got := FormatAutoStartCommand(`C:\Program Files\WhatsMySpeed\WhatsMySpeed.exe`)
	want := `"C:\Program Files\WhatsMySpeed\WhatsMySpeed.exe" --tray`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAppAuthorMetadata(t *testing.T) {
	if AppAuthor != "Shubham Bhamare" {
		t.Fatalf("AppAuthor = %q, want %q", AppAuthor, "Shubham Bhamare")
	}
}

func TestLoadFromStripsUTF8BOM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bom_config.json")
	bomJSON := append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{"theme":"dark","speed_unit":"bits","start_with_windows":true}`)...)
	if err := os.WriteFile(path, bomJSON, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom(BOM) failed: %v", err)
	}
	if cfg.Theme != ThemeDark || cfg.SpeedUnit != UnitBits || !cfg.StartWithWindows {
		t.Fatalf("unexpected BOM config %+v", cfg)
	}
}

func TestLoadAndSyncStartupInitializesAndUpgradesConfig(t *testing.T) {
	origQuery, origApply := queryAutoStart, applyAutoStart
	defer func() {
		queryAutoStart, applyAutoStart = origQuery, origApply
	}()

	regState := false
	appliedCalls := 0
	queryAutoStart = func() (bool, error) { return regState, nil }
	applyAutoStart = func(enabled bool) error {
		appliedCalls++
		regState = enabled
		return nil
	}

	dir := t.TempDir()
	// 1. Missing config creates full JSON with default StartWithWindows=true and enables autostart.
	missingPath := filepath.Join(dir, "fresh", "config.json")
	cfg, err := LoadAndSyncStartup(missingPath)
	if err != nil {
		t.Fatalf("LoadAndSyncStartup(missing) failed: %v", err)
	}
	if !cfg.StartWithWindows || !regState {
		t.Fatalf("expected StartWithWindows=true and regState=true by default (got cfg=%v reg=%v)", cfg.StartWithWindows, regState)
	}
	if _, statErr := os.Stat(missingPath); statErr != nil {
		t.Fatalf("expected initial config file to be written: %v", statErr)
	}

	// 2. Legacy 5-field config upgrades with default StartWithWindows=true.
	legacyPath := filepath.Join(dir, "legacy.json")
	if err := os.WriteFile(legacyPath, []byte(`{"display_mode":"download","theme":"dark"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	regState = false
	upCfg, err := LoadAndSyncStartup(legacyPath)
	if err != nil {
		t.Fatalf("LoadAndSyncStartup(legacy) failed: %v", err)
	}
	if upCfg.DisplayMode != ModeDownload || upCfg.Theme != ThemeDark || !upCfg.StartWithWindows || !regState {
		t.Fatalf("unexpected upgraded config %+v regState=%v", upCfg, regState)
	}

	// 3. Explicit start_with_windows=false in config applies false to registry.
	regState = true
	explicitPath := filepath.Join(dir, "explicit.json")
	wantExplicit := Config{
		DisplayMode:      ModeUpload,
		SpeedUnit:        UnitBits,
		LabelStyle:       LabelWords,
		Layout:           LayoutInline,
		Theme:            ThemeLight,
		StartWithWindows: false,
	}
	if err := SaveTo(explicitPath, wantExplicit); err != nil {
		t.Fatal(err)
	}
	gotExplicit, err := LoadAndSyncStartup(explicitPath)
	if err != nil {
		t.Fatalf("LoadAndSyncStartup(explicit) failed: %v", err)
	}
	if gotExplicit != wantExplicit || regState || appliedCalls == 0 {
		t.Fatalf("got=%+v regState=%v appliedCalls=%d, want=%+v false >0", gotExplicit, regState, appliedCalls, wantExplicit)
	}
}
