package ui

import (
	"golang.org/x/sys/windows/registry"
)

const personalizeKeyPath = `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`

// ThemeColors defines ARGB colors matched to Windows 11 Light or Dark taskbar styling.
type ThemeColors struct {
	IsLight         bool
	TextARGB        uint32
	IconARGB        uint32
	TrayIconARGB    uint32
	HoverFillARGB   uint32
	HoverBorderARGB uint32
}

// DetectTaskbarLightTheme checks the Windows Personalize registry key to determine
// whether the taskbar uses the Light theme.
func DetectTaskbarLightTheme() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, personalizeKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()

	// SystemUsesLightTheme controls the Windows 11 taskbar and system tray directly.
	if val, _, err := key.GetIntegerValue("SystemUsesLightTheme"); err == nil {
		return val != 0
	}
	if val, _, err := key.GetIntegerValue("AppsUseLightTheme"); err == nil {
		return val != 0
	}
	return false
}

// ColorsForTheme returns native Windows 11 taskbar colors for light or dark mode.
func ColorsForTheme(isLight bool) ThemeColors {
	if isLight {
		return ThemeColors{
			IsLight:         true,
			TextARGB:        0xFF1A1A1A,
			IconARGB:        0xFF1A1A1A,
			TrayIconARGB:    0xFF1A1A1A,
			HoverFillARGB:   0x12000000,
			HoverBorderARGB: 0x1A000000,
		}
	}
	return ThemeColors{
		IsLight:         false,
		TextARGB:        0xFFFFFFFF,
		IconARGB:        0xFFFFFFFF,
		TrayIconARGB:    0xFFFFFFFF,
		HoverFillARGB:   0x1AFFFFFF,
		HoverBorderARGB: 0x24FFFFFF,
	}
}
