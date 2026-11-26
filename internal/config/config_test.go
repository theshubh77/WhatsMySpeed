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

	for _, mode := range modes {
		for _, unit := range units {
			for _, label := range labels {
				for _, layout := range layouts {
					for _, theme := range themes {
						want := Config{
							DisplayMode: mode,
							SpeedUnit:   unit,
							LabelStyle:  label,
							Layout:      layout,
							Theme:       theme,
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

func TestLoadFromLegacyConfigDefaultsNewFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"display_mode":"download"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DisplayMode != ModeDownload || cfg.SpeedUnit != UnitBytes || cfg.LabelStyle != LabelArrows || cfg.Layout != LayoutStackedUp || cfg.Theme != ThemeSystem {
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
	want := `"C:\Program Files\WhatsMySpeed\WhatsMySpeed.exe"`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
