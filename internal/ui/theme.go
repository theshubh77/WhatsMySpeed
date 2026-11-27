package ui

import (
	"unsafe"

	"github.com/WhatsMySpeed/WhatsMySpeed/internal/config"
	"golang.org/x/sys/windows/registry"
)

const (
	personalizeKeyPath = `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`
	dwmKeyPath         = `Software\Microsoft\Windows\DWM`
)

// ThemeColors defines ARGB colors matched to Windows 11 Light, Dark, Accent, or High Contrast styling.
type ThemeColors struct {
	IsLight         bool
	TextARGB        uint32
	IconARGB        uint32
	TrayIconARGB    uint32
	HoverFillARGB   uint32
	HoverBorderARGB uint32
}

func colorrefToARGB(cr uint32) uint32 {
	r := cr & 0xFF
	g := (cr >> 8) & 0xFF
	b := (cr >> 16) & 0xFF
	return 0xFF000000 | (r << 16) | (g << 8) | b
}

func perceivedLuminance(argb uint32) uint32 {
	r := (argb >> 16) & 0xFF
	g := (argb >> 8) & 0xFF
	b := argb & 0xFF
	return (r*299 + g*587 + b*114) / 1000
}

func resolveTaskbarIsLightFromSettings(sysLight *uint64, appLight *uint64, colorPrevalence *uint64, accentABGR *uint64) bool {
	if colorPrevalence != nil && *colorPrevalence != 0 && accentABGR != nil {
		return perceivedLuminance(colorrefToARGB(uint32(*accentABGR))) >= 145
	}
	if sysLight != nil {
		return *sysLight != 0
	}
	if appLight != nil {
		return *appLight != 0
	}
	return false
}

// DetectHighContrast reports whether Windows High Contrast mode is active and returns
// its window background and foreground text colors in ARGB format.
func DetectHighContrast() (active bool, bgARGB, textARGB uint32) {
	if procSystemParametersInfoW.Find() != nil || procGetSysColor.Find() != nil {
		return false, 0, 0
	}
	var hc highContrastW
	hc.CbSize = uint32(unsafe.Sizeof(hc))
	ok, _, _ := procSystemParametersInfoW.Call(spiGetHighContrast, uintptr(hc.CbSize), uintptr(unsafe.Pointer(&hc)), 0)
	if ok == 0 || (hc.DwFlags&hcfHighContrastOn) == 0 {
		return false, 0, 0
	}
	bgCR, _, _ := procGetSysColor.Call(colorWindow)
	txtCR, _, _ := procGetSysColor.Call(colorWindowText)
	return true, colorrefToARGB(uint32(bgCR)), colorrefToARGB(uint32(txtCR))
}

// DetectTaskbarColors resolves the best foreground/hover palette for the Windows taskbar,
// accounting for Dark/Light themes, "Show accent color on taskbar", and High Contrast themes.
func DetectTaskbarColors() ThemeColors {
	if active, bgARGB, txtARGB := DetectHighContrast(); active {
		isLight := perceivedLuminance(bgARGB) >= 145
		base := ColorsForTheme(isLight)
		base.TextARGB = txtARGB
		base.IconARGB = txtARGB
		base.TrayIconARGB = txtARGB
		return base
	}

	var sysLightPtr, appLightPtr, prevPtr, accentPtr *uint64
	if key, err := registry.OpenKey(registry.CURRENT_USER, personalizeKeyPath, registry.QUERY_VALUE); err == nil {
		if v, _, err := key.GetIntegerValue("SystemUsesLightTheme"); err == nil {
			sysLightPtr = &v
		}
		if v, _, err := key.GetIntegerValue("AppsUseLightTheme"); err == nil {
			appLightPtr = &v
		}
		if v, _, err := key.GetIntegerValue("ColorPrevalence"); err == nil {
			prevPtr = &v
		}
		key.Close()
	}

	if prevPtr != nil && *prevPtr != 0 {
		if dwmKey, err := registry.OpenKey(registry.CURRENT_USER, dwmKeyPath, registry.QUERY_VALUE); err == nil {
			if v, _, err := dwmKey.GetIntegerValue("AccentColor"); err == nil {
				accentPtr = &v
			} else if v, _, err := dwmKey.GetIntegerValue("ColorizationColor"); err == nil {
				// ColorizationColor is 0xAARRGGBB in registry; convert to ABGR only when AccentColor is absent.
				abgr := (v & 0xFF00FF00) | ((v & 0x00FF0000) >> 16) | ((v & 0x000000FF) << 16)
				accentPtr = &abgr
			}
			dwmKey.Close()
		}
	}

	return ColorsForTheme(resolveTaskbarIsLightFromSettings(sysLightPtr, appLightPtr, prevPtr, accentPtr))
}

// DetectTaskbarLightTheme checks Windows taskbar appearance (including dark/light mode,
// taskbar accent color brightness, and High Contrast mode).
func DetectTaskbarLightTheme() bool {
	return DetectTaskbarColors().IsLight
}

// DetectAppsLightTheme checks Windows app theme preference (AppsUseLightTheme / High Contrast),
// matching Windows Custom mode where taskbar is Dark while app windows are Light.
func DetectAppsLightTheme() bool {
	if active, bgARGB, _ := DetectHighContrast(); active {
		return perceivedLuminance(bgARGB) >= 145
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, personalizeKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()

	if val, _, err := key.GetIntegerValue("AppsUseLightTheme"); err == nil {
		return val != 0
	}
	if val, _, err := key.GetIntegerValue("SystemUsesLightTheme"); err == nil {
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

func resolveThemeIsLight(mode config.ThemeMode) bool {
	switch mode {
	case config.ThemeLight:
		return true
	case config.ThemeDark:
		return false
	default:
		return DetectAppsLightTheme()
	}
}

type tooltipPalette struct {
	bg         uint32
	border     uint32
	titleColor uint32
	textColor  uint32
	downArrow  uint32
	upArrow    uint32
}

func resolveTooltipPalette(isLight bool) tooltipPalette {
	if active, _, _ := DetectHighContrast(); active && procGetSysColor.Find() == nil {
		winCR, _, _ := procGetSysColor.Call(colorWindow)
		txtCR, _, _ := procGetSysColor.Call(colorWindowText)
		hiCR, _, _ := procGetSysColor.Call(colorHighlight)
		bg := colorrefToARGB(uint32(winCR))
		txt := colorrefToARGB(uint32(txtCR))
		accent := colorrefToARGB(uint32(hiCR))
		return tooltipPalette{
			bg:         bg,
			border:     txt,
			titleColor: txt,
			textColor:  txt,
			downArrow:  accent,
			upArrow:    txt,
		}
	}
	if isLight {
		return tooltipPalette{
			bg:         0xFAFFFFFF,
			border:     0xFFCBD5E1,
			titleColor: 0xFF475569,
			textColor:  0xFF0F172A,
			downArrow:  0xFF0E7490,
			upArrow:    0xFF4D7C0F,
		}
	}
	return tooltipPalette{
		bg:         0xF7161D2C,
		border:     0xFF334155,
		titleColor: 0xFF94A3B8,
		textColor:  0xFFF8FAFC,
		downArrow:  0xFF22D3EE,
		upArrow:    0xFFA3E635,
	}
}

func resolveTooltipPaletteForMode(mode config.ThemeMode) tooltipPalette {
	return resolveTooltipPalette(resolveThemeIsLight(mode))
}
