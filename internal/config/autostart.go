package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

const (
	runRegistryPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	// AutoStartArg marks launches initiated from the Windows Run registry entry so they start quietly in the taskbar/tray.
	AutoStartArg = "--tray"
)

// FormatAutoStartCommand returns the quoted command line written to the Windows Run key.
func FormatAutoStartCommand(exePath string) string {
	return fmt.Sprintf(`"%s" %s`, exePath, AutoStartArg)
}

// IsAutoStartEnabled reports whether WhatsMySpeed is registered to launch on Windows sign-in.
func IsAutoStartEnabled() (bool, error) {
	if IsPackaged() {
		return isMSIXStartupEnabled()
	}

	key, err := registry.OpenKey(registry.CURRENT_USER, runRegistryPath, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("open Run registry key: %w", err)
	}
	defer key.Close()

	val, _, err := key.GetStringValue(AppName)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("query Run registry value: %w", err)
	}
	return val != "", nil
}

// SetAutoStart updates startup registration via MSIX StartupTask when packaged
// or HKCU\...\Run when running unpackaged.
func SetAutoStart(enabled bool) error {
	if IsPackaged() {
		return setMSIXStartupEnabled(enabled)
	}

	if !enabled {
		key, err := registry.OpenKey(registry.CURRENT_USER, runRegistryPath, registry.SET_VALUE)
		if err != nil {
			if errors.Is(err, registry.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("open Run registry key for removal: %w", err)
		}
		defer key.Close()

		if err := key.DeleteValue(AppName); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return fmt.Errorf("delete Run registry value: %w", err)
		}
		return nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	if abs, err := filepath.Abs(exePath); err == nil {
		exePath = abs
	}

	key, _, err := registry.CreateKey(registry.CURRENT_USER, runRegistryPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open Run registry key for write: %w", err)
	}
	defer key.Close()

	if err := key.SetStringValue(AppName, FormatAutoStartCommand(exePath)); err != nil {
		return fmt.Errorf("write Run registry value: %w", err)
	}
	return nil
}
